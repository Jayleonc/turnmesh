package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/eventctx"
	"github.com/Jayleonc/turnmesh/internal/executor"
	"github.com/Jayleonc/turnmesh/internal/model"
)

type stubSession struct {
	streams [][]core.TurnEvent
	inputs  []core.TurnInput
	err     error
}

func (s *stubSession) ID() string       { return "session-1" }
func (s *stubSession) Provider() string { return "stub" }
func (s *stubSession) Model() string    { return "stub-model" }
func (s *stubSession) Capabilities() model.Capabilities {
	return model.Capabilities{CanStream: true, CanToolCall: true}
}
func (s *stubSession) Close() error { return nil }

func (s *stubSession) StreamTurn(_ context.Context, input core.TurnInput) (<-chan core.TurnEvent, error) {
	if s.err != nil {
		return nil, s.err
	}

	s.inputs = append(s.inputs, input)
	index := len(s.inputs) - 1

	var events []core.TurnEvent
	if index < len(s.streams) {
		events = s.streams[index]
	}

	ch := make(chan core.TurnEvent, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

type stubDispatcher struct {
	calls  []core.ToolInvocation
	result core.ToolResult
	err    error
}

func (s *stubDispatcher) ExecuteTool(_ context.Context, call core.ToolInvocation) (core.ToolResult, error) {
	s.calls = append(s.calls, call)
	return s.result, s.err
}

type emittingDispatcher struct {
	calls  []core.ToolInvocation
	result core.ToolResult
	err    error
}

func (s *emittingDispatcher) ExecuteTool(ctx context.Context, call core.ToolInvocation) (core.ToolResult, error) {
	s.calls = append(s.calls, call)
	eventctx.Emit(ctx, core.TurnEvent{
		Kind:    core.TurnEventCitation,
		Status:  core.TurnStatusRunning,
		Payload: json.RawMessage(`{"source":"doc-1","text":"alpha"}`),
	})
	eventctx.Emit(ctx, core.TurnEvent{
		Kind:    core.TurnEventClarification,
		Status:  core.TurnStatusWaiting,
		Payload: json.RawMessage(`{"question":"need more context"}`),
	})
	return s.result, s.err
}

type stubBatchRuntime struct {
	calls  [][]core.ToolInvocation
	report executor.BatchReport
	err    error
}

func (s *stubBatchRuntime) Plan(context.Context, []core.ToolInvocation) ([]executor.ToolBatch, error) {
	return nil, nil
}

func (s *stubBatchRuntime) Run(_ context.Context, calls []core.ToolInvocation) (executor.BatchReport, error) {
	cloned := make([]core.ToolInvocation, 0, len(calls))
	for _, call := range calls {
		cloned = append(cloned, call)
	}
	s.calls = append(s.calls, cloned)
	return s.report, s.err
}

func (s *stubBatchRuntime) Stream(context.Context, []core.ToolInvocation) (executor.BatchStream, error) {
	return executor.BatchStream{}, errors.New("not implemented")
}

type recordingFinalizer struct {
	reports []TurnReport
}

func (f *recordingFinalizer) FinalizeTurn(_ context.Context, report TurnReport) error {
	f.reports = append(f.reports, report)
	return nil
}

type failingPreparer struct {
	err error
}

func (p failingPreparer) PrepareTurn(context.Context, core.TurnInput) (core.TurnInput, error) {
	return core.TurnInput{}, p.err
}

func collectEvents(ch <-chan core.TurnEvent) []core.TurnEvent {
	var events []core.TurnEvent
	for event := range ch {
		events = append(events, event)
	}
	return events
}

func TestBootWithoutSessionIsSafe(t *testing.T) {
	engine := New(Config{})

	if err := engine.Boot(context.Background()); err != nil {
		t.Fatalf("Boot() error = %v", err)
	}
	if !engine.Booted() {
		t.Fatal("Booted() = false, want true")
	}
}

func TestStreamTurnWithoutSessionEmitsCompletion(t *testing.T) {
	engine := New(Config{})
	if err := engine.Boot(context.Background()); err != nil {
		t.Fatalf("Boot() error = %v", err)
	}

	events, err := engine.StreamTurn(context.Background(), core.TurnInput{})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}

	got := collectEvents(events)
	if len(got) < 2 {
		t.Fatalf("len(events) = %d, want >= 2", len(got))
	}
	if got[0].Kind != core.TurnEventStarted {
		t.Fatalf("first event = %q, want %q", got[0].Kind, core.TurnEventStarted)
	}
	if got[len(got)-1].Kind != core.TurnEventCompleted {
		t.Fatalf("last event = %q, want %q", got[len(got)-1].Kind, core.TurnEventCompleted)
	}
}

func TestStreamTurnRoutesToolCallToDispatcher(t *testing.T) {
	dispatcher := &stubDispatcher{result: core.ToolResult{
		InvocationID: "tool-1",
		Tool:         "bash",
		Status:       core.ToolStatusSucceeded,
		Output:       "done",
	}}
	session := &stubSession{
		streams: [][]core.TurnEvent{
			{
				{
					Kind:    core.TurnEventMessage,
					Message: &core.Message{Role: core.MessageRoleAssistant, Content: "checking"},
				},
				{
					Kind:     core.TurnEventToolCall,
					ToolCall: &core.ToolInvocation{ID: "tool-1", Tool: "bash", Arguments: json.RawMessage(`{"command":"pwd"}`)},
				},
				{Kind: core.TurnEventCompleted},
			},
			{
				{
					Kind:    core.TurnEventMessage,
					Message: &core.Message{Role: core.MessageRoleAssistant, Content: "done"},
				},
				{Kind: core.TurnEventCompleted},
			},
		},
	}

	engine := New(Config{
		Session: session,
		Tools:   dispatcher,
	})
	if err := engine.Boot(context.Background()); err != nil {
		t.Fatalf("Boot() error = %v", err)
	}

	events, err := engine.StreamTurn(context.Background(), core.TurnInput{})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}

	got := collectEvents(events)
	if len(dispatcher.calls) != 1 {
		t.Fatalf("dispatcher calls = %d, want 1", len(dispatcher.calls))
	}
	if len(session.inputs) != 2 {
		t.Fatalf("session calls = %d, want 2", len(session.inputs))
	}
	if got[1].Kind != core.TurnEventMessage {
		t.Fatalf("event[1] = %q, want %q", got[1].Kind, core.TurnEventMessage)
	}
	if got[2].Kind != core.TurnEventToolCall {
		t.Fatalf("event[2] = %q, want %q", got[2].Kind, core.TurnEventToolCall)
	}
	if got[3].Kind != core.TurnEventToolResult {
		t.Fatalf("event[3] = %q, want %q", got[3].Kind, core.TurnEventToolResult)
	}
	if got[4].Kind != core.TurnEventMessage {
		t.Fatalf("event[4] = %q, want %q", got[4].Kind, core.TurnEventMessage)
	}
	if got[len(got)-1].Kind != core.TurnEventCompleted {
		t.Fatalf("last event = %q, want %q", got[len(got)-1].Kind, core.TurnEventCompleted)
	}

	secondInput := session.inputs[1]
	if len(secondInput.Messages) != 2 {
		t.Fatalf("second input messages = %d, want 2", len(secondInput.Messages))
	}
	if secondInput.Messages[0].Role != core.MessageRoleAssistant {
		t.Fatalf("second input first role = %q, want %q", secondInput.Messages[0].Role, core.MessageRoleAssistant)
	}
	if len(secondInput.Messages[0].Parts) != 2 {
		t.Fatalf("assistant continuation parts = %d, want 2", len(secondInput.Messages[0].Parts))
	}
	if secondInput.Messages[1].Role != core.MessageRoleTool {
		t.Fatalf("second input second role = %q, want %q", secondInput.Messages[1].Role, core.MessageRoleTool)
	}
	if len(secondInput.Messages[1].Parts) != 1 || secondInput.Messages[1].Parts[0].ToolResult == nil {
		t.Fatalf("tool result message = %#v, want one tool result part", secondInput.Messages[1])
	}
}

func TestStreamTurnPropagatesContextEventsFromToolExecution(t *testing.T) {
	dispatcher := &emittingDispatcher{result: core.ToolResult{
		InvocationID: "tool-1",
		Tool:         "bash",
		Status:       core.ToolStatusSucceeded,
		Output:       "done",
	}}
	session := &stubSession{
		streams: [][]core.TurnEvent{
			{
				{
					Kind:    core.TurnEventMessage,
					Message: &core.Message{Role: core.MessageRoleAssistant, Content: "checking"},
				},
				{
					Kind:     core.TurnEventToolCall,
					ToolCall: &core.ToolInvocation{ID: "tool-1", Tool: "bash", Arguments: json.RawMessage(`{"command":"pwd"}`)},
				},
				{Kind: core.TurnEventCompleted},
			},
			{
				{
					Kind:    core.TurnEventMessage,
					Message: &core.Message{Role: core.MessageRoleAssistant, Content: "done"},
				},
				{Kind: core.TurnEventCompleted},
			},
		},
	}

	engine := New(Config{
		Session: session,
		Tools:   dispatcher,
	})
	if err := engine.Boot(context.Background()); err != nil {
		t.Fatalf("Boot() error = %v", err)
	}

	events, err := engine.StreamTurn(context.Background(), core.TurnInput{})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}

	got := collectEvents(events)
	if len(dispatcher.calls) != 1 {
		t.Fatalf("dispatcher calls = %d, want 1", len(dispatcher.calls))
	}
	if got[3].Kind != core.TurnEventCitation {
		t.Fatalf("event[3] = %q, want %q", got[3].Kind, core.TurnEventCitation)
	}
	if got[4].Kind != core.TurnEventClarification {
		t.Fatalf("event[4] = %q, want %q", got[4].Kind, core.TurnEventClarification)
	}
	if got[3].Payload == nil || string(got[3].Payload) != `{"source":"doc-1","text":"alpha"}` {
		t.Fatalf("citation payload = %s, want doc-1 alpha", string(got[3].Payload))
	}
	if got[4].Payload == nil || string(got[4].Payload) != `{"question":"need more context"}` {
		t.Fatalf("clarification payload = %s, want question", string(got[4].Payload))
	}
}

func TestStreamTurnFailsWhenSessionCannotStart(t *testing.T) {
	engine := New(Config{
		Session: &stubSession{err: errors.New("boom")},
	})
	if err := engine.Boot(context.Background()); err != nil {
		t.Fatalf("Boot() error = %v", err)
	}

	events, err := engine.StreamTurn(context.Background(), core.TurnInput{})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}

	got := collectEvents(events)
	if got[len(got)-1].Kind != core.TurnEventError {
		t.Fatalf("last event = %q, want %q", got[len(got)-1].Kind, core.TurnEventError)
	}
}

func TestStreamTurnUsesBatchRuntimeAndFinalizer(t *testing.T) {
	dispatcher := &stubDispatcher{result: core.ToolResult{
		InvocationID: "tool-1",
		Tool:         "bash",
		Status:       core.ToolStatusSucceeded,
		Output:       "dispatcher should stay idle",
	}}
	batch := &stubBatchRuntime{
		report: executor.BatchReport{
			Results: []core.ToolResult{{
				InvocationID: "tool-1",
				Tool:         "bash",
				Status:       core.ToolStatusSucceeded,
				Output:       "batched",
			}},
		},
	}
	finalizer := &recordingFinalizer{}
	session := &stubSession{
		streams: [][]core.TurnEvent{
			{
				{
					Kind:     core.TurnEventToolCall,
					ToolCall: &core.ToolInvocation{ID: "tool-1", Tool: "bash", Arguments: json.RawMessage(`{"command":"pwd"}`)},
				},
				{Kind: core.TurnEventCompleted},
			},
			{
				{
					Kind:    core.TurnEventMessage,
					Message: &core.Message{Role: core.MessageRoleAssistant, Content: "done"},
				},
				{Kind: core.TurnEventCompleted},
			},
		},
	}

	engine := New(Config{
		Session:   session,
		Tools:     dispatcher,
		ToolBatch: batch,
		Finalizer: finalizer,
	})
	if err := engine.Boot(context.Background()); err != nil {
		t.Fatalf("Boot() error = %v", err)
	}

	events, err := engine.StreamTurn(context.Background(), core.TurnInput{SessionID: "session-1", TurnID: "turn-1"})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}

	got := collectEvents(events)
	if len(batch.calls) != 1 {
		t.Fatalf("batch calls = %d, want 1", len(batch.calls))
	}
	if len(dispatcher.calls) != 0 {
		t.Fatalf("dispatcher calls = %d, want 0 when batch runtime is configured", len(dispatcher.calls))
	}
	if got[2].Kind != core.TurnEventToolResult || got[2].ToolResult == nil || got[2].ToolResult.Output != "batched" {
		t.Fatalf("event[2] = %#v, want batched tool result", got[2])
	}
	if len(finalizer.reports) != 1 {
		t.Fatalf("finalizer reports = %d, want 1", len(finalizer.reports))
	}
	if finalizer.reports[0].Prepared.SessionID != "session-1" {
		t.Fatalf("prepared session id = %q, want session-1", finalizer.reports[0].Prepared.SessionID)
	}
	if len(finalizer.reports[0].Events) == 0 {
		t.Fatal("finalizer report events are empty")
	}
}

func TestStreamTurnSkipsFinalizerWhenPrepareFails(t *testing.T) {
	finalizer := &recordingFinalizer{}
	engine := New(Config{
		Preparer:  failingPreparer{err: errors.New("bad input")},
		Finalizer: finalizer,
		Session:   &stubSession{},
	})
	if err := engine.Boot(context.Background()); err != nil {
		t.Fatalf("Boot() error = %v", err)
	}

	events, err := engine.StreamTurn(context.Background(), core.TurnInput{})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}
	got := collectEvents(events)

	if len(finalizer.reports) != 0 {
		t.Fatalf("finalizer reports = %d, want 0", len(finalizer.reports))
	}
	if got[len(got)-1].Kind != core.TurnEventError {
		t.Fatalf("last event = %q, want error", got[len(got)-1].Kind)
	}
}
