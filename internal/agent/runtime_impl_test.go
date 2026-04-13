package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTaskLifecycleStateMachine(t *testing.T) {
	machine := TaskLifecycleStateMachine{}

	if !machine.CanTransition(TaskStatusUnknown, TaskStatusPending) {
		t.Fatalf("unknown -> pending should be allowed")
	}
	if !machine.CanTransition(TaskStatusPending, TaskStatusRunning) {
		t.Fatalf("pending -> running should be allowed")
	}
	if !machine.CanTransition(TaskStatusRunning, TaskStatusCompleted) {
		t.Fatalf("running -> completed should be allowed")
	}
	if machine.CanTransition(TaskStatusCompleted, TaskStatusStopped) {
		t.Fatalf("completed -> stopped should be rejected")
	}
	if err := machine.Transition(TaskStatusRunning, TaskStatusCompleted); err != nil {
		t.Fatalf("transition should be allowed: %v", err)
	}
	if err := machine.Transition(TaskStatusCompleted, TaskStatusRunning); !errors.Is(err, ErrInvalidTaskTransition) {
		t.Fatalf("expected invalid transition error, got %v", err)
	}
}

func TestAgentRuntimeStartEmitsEventsAndInjectsRunner(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []RunRequest
	)

	runnerStarted := make(chan struct{})
	runnerDone := make(chan struct{})
	runner := RunnerFunc(func(ctx context.Context, req RunRequest, emit func(Event)) error {
		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()

		select {
		case <-runnerStarted:
		default:
			close(runnerStarted)
		}

		emit(Event{
			Type:    EventTypeProgress,
			Payload: ProgressEvent{Progress: 0.5, Summary: "halfway"},
		})
		close(runnerDone)
		return nil
	})

	clockValue := time.Date(2026, time.April, 13, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return clockValue }

	runtime := NewAgentRuntime(runner, WithClock(clock), WithTaskIDGenerator(func() string { return "task-1" }), WithEventBuffer(8))

	overlayPrompt := "overlay prompt"
	overlayBackground := false
	started, events, err := runtime.Start(context.Background(), StartRequest{
		TaskID: "task-1",
		Definition: Definition{
			ID:           "agent-1",
			Name:         "tester",
			SystemPrompt: "base prompt",
			AllowedTools: []string{"base"},
			MCPServers:   []string{"mcp-a"},
			Background:   true,
			Isolated:     true,
			Metadata:     map[string]string{"base": "1"},
		},
		Input: "payload",
		Context: TaskContext{
			ParentTaskID: "parent",
			SessionID:    "session",
			Background:   true,
			Isolated:     true,
			Metadata:     map[string]string{"ctx": "1"},
		},
		Overlay: RuntimeOverlay{
			SystemPrompt: &overlayPrompt,
			AllowedTools: []string{"overlay"},
			MCPServers:   []string{"mcp-b"},
			Background:   &overlayBackground,
			Metadata:     map[string]string{"overlay": "2"},
		},
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	if started.ID() != "task-1" {
		t.Fatalf("unexpected task id: %s", started.ID())
	}

	gotEvents := collectEvents(t, events, 5, time.Second)
	if len(gotEvents) != 5 {
		t.Fatalf("expected 5 events, got %d", len(gotEvents))
	}
	wantTypes := []string{
		EventTypeStarted,
		EventTypeStatusChanged,
		EventTypeProgress,
		EventTypeCompleted,
		EventTypeStatusChanged,
	}
	for i, want := range wantTypes {
		if gotEvents[i].Type != want {
			t.Fatalf("event %d: want type %q, got %q", i, want, gotEvents[i].Type)
		}
	}

	if !waitForTaskStatus(t, runtime, "task-1", TaskStatusCompleted, time.Second) {
		t.Fatalf("task did not reach completed status")
	}

	snapshot, err := runtime.GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if snapshot.Status != TaskStatusCompleted {
		t.Fatalf("expected completed status, got %s", snapshot.Status)
	}
	if snapshot.Progress != 0.5 {
		t.Fatalf("expected progress 0.5, got %v", snapshot.Progress)
	}
	if snapshot.Definition.SystemPrompt != overlayPrompt {
		t.Fatalf("overlay system prompt not applied: %q", snapshot.Definition.SystemPrompt)
	}
	if snapshot.Definition.Background != overlayBackground {
		t.Fatalf("overlay background not applied")
	}
	if got := snapshot.Context.Metadata["base"]; got != "1" {
		t.Fatalf("base metadata not preserved: %q", got)
	}
	if got := snapshot.Context.Metadata["ctx"]; got != "1" {
		t.Fatalf("context metadata not preserved: %q", got)
	}
	if got := snapshot.Context.Metadata["overlay"]; got != "2" {
		t.Fatalf("overlay metadata not merged: %q", got)
	}

	mu.Lock()
	if len(requests) != 1 {
		mu.Unlock()
		t.Fatalf("expected one run request, got %d", len(requests))
	}
	req := requests[0]
	mu.Unlock()

	if req.BaseDefinition.SystemPrompt != "base prompt" {
		t.Fatalf("base definition changed: %q", req.BaseDefinition.SystemPrompt)
	}
	if req.Definition.SystemPrompt != overlayPrompt {
		t.Fatalf("effective definition not overlayed: %q", req.Definition.SystemPrompt)
	}
	if req.Context.ParentTaskID != "parent" || req.Context.SessionID != "session" {
		t.Fatalf("task context not forwarded: %+v", req.Context)
	}
	if req.Context.Background != overlayBackground {
		t.Fatalf("context background not overlayed")
	}
	if got := req.Context.Metadata["overlay"]; got != "2" {
		t.Fatalf("runner context missing overlay metadata: %q", got)
	}

	select {
	case <-runnerDone:
	case <-time.After(time.Second):
		t.Fatalf("runner did not finish")
	}
}

func TestAgentRuntimeStopTransitionsTaskToStopped(t *testing.T) {
	runnerStarted := make(chan struct{})
	runner := RunnerFunc(func(ctx context.Context, req RunRequest, emit func(Event)) error {
		close(runnerStarted)
		<-ctx.Done()
		return ctx.Err()
	})

	runtime := NewAgentRuntime(runner, WithClock(func() time.Time {
		return time.Date(2026, time.April, 13, 11, 0, 0, 0, time.UTC)
	}), WithTaskIDGenerator(func() string { return "task-stop" }))

	_, _, err := runtime.Start(context.Background(), StartRequest{
		TaskID: "task-stop",
		Definition: Definition{
			ID:   "agent-stop",
			Name: "stopper",
		},
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}

	select {
	case <-runnerStarted:
	case <-time.After(time.Second):
		t.Fatalf("runner did not start")
	}

	if err := runtime.Stop(context.Background(), "task-stop"); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	if !waitForTaskStatus(t, runtime, "task-stop", TaskStatusStopped, time.Second) {
		t.Fatalf("task did not reach stopped status")
	}

	snapshot, err := runtime.GetTask(context.Background(), "task-stop")
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if snapshot.Status != TaskStatusStopped {
		t.Fatalf("expected stopped status, got %s", snapshot.Status)
	}
}

func TestAgentRuntimeListTasksReturnsSnapshots(t *testing.T) {
	runner := RunnerFunc(func(ctx context.Context, req RunRequest, emit func(Event)) error {
		return nil
	})

	runtime := NewAgentRuntime(runner, WithClock(func() time.Time {
		return time.Date(2026, time.April, 13, 12, 0, 0, 0, time.UTC)
	}))

	if _, _, err := runtime.Start(context.Background(), StartRequest{
		TaskID:     "task-a",
		Definition: Definition{ID: "agent-a", Name: "a"},
	}); err != nil {
		t.Fatalf("start a failed: %v", err)
	}
	if _, _, err := runtime.Start(context.Background(), StartRequest{
		TaskID:     "task-b",
		Definition: Definition{ID: "agent-b", Name: "b"},
	}); err != nil {
		t.Fatalf("start b failed: %v", err)
	}

	snapshots, err := runtime.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(snapshots))
	}
	if snapshots[0].ID != "task-a" || snapshots[1].ID != "task-b" {
		t.Fatalf("tasks not sorted deterministically: %#v", snapshots)
	}
}

func collectEvents(t *testing.T, events <-chan Event, count int, timeout time.Duration) []Event {
	t.Helper()

	out := make([]Event, 0, count)
	deadline := time.After(timeout)
	for len(out) < count {
		select {
		case event := <-events:
			out = append(out, event)
		case <-deadline:
			t.Fatalf("timed out waiting for event %d", len(out)+1)
		}
	}
	return out
}

func waitForTaskStatus(t *testing.T, runtime *AgentRuntime, taskID string, want TaskStatus, timeout time.Duration) bool {
	t.Helper()

	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		snapshot, err := runtime.GetTask(context.Background(), taskID)
		if err == nil && snapshot.Status == want {
			return true
		}

		select {
		case <-deadline:
			return false
		case <-ticker.C:
		}
	}
}
