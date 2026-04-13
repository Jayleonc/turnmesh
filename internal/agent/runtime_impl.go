package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

var agentTaskCounter uint64

// RuntimeOption customizes the agent runtime.
type RuntimeOption func(*runtimeConfig)

type runtimeConfig struct {
	clock       func() time.Time
	taskIDGen   func() string
	eventBuffer int
}

func defaultRuntimeConfig() runtimeConfig {
	return runtimeConfig{
		clock:       time.Now,
		eventBuffer: 16,
		taskIDGen: func() string {
			n := atomic.AddUint64(&agentTaskCounter, 1)
			return fmt.Sprintf("agent-task-%d-%d", time.Now().UnixNano(), n)
		},
	}
}

// WithClock injects a clock for deterministic tests.
func WithClock(clock func() time.Time) RuntimeOption {
	return func(cfg *runtimeConfig) {
		if clock != nil {
			cfg.clock = clock
		}
	}
}

// WithTaskIDGenerator injects a task ID generator.
func WithTaskIDGenerator(fn func() string) RuntimeOption {
	return func(cfg *runtimeConfig) {
		if fn != nil {
			cfg.taskIDGen = fn
		}
	}
}

// WithEventBuffer sets the per-task event channel buffer.
func WithEventBuffer(size int) RuntimeOption {
	return func(cfg *runtimeConfig) {
		if size > 0 {
			cfg.eventBuffer = size
		}
	}
}

// AgentRuntime is the minimal agent runtime implementation.
type AgentRuntime struct {
	runner      Runner
	tasks       *taskRegistry
	clock       func() time.Time
	taskIDGen   func() string
	eventBuffer int
}

// NewAgentRuntime constructs a runtime from an injected runner.
func NewAgentRuntime(runner Runner, opts ...RuntimeOption) *AgentRuntime {
	cfg := defaultRuntimeConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	return &AgentRuntime{
		runner:      runner,
		tasks:       newTaskRegistry(),
		clock:       cfg.clock,
		taskIDGen:   cfg.taskIDGen,
		eventBuffer: cfg.eventBuffer,
	}
}

// Start creates a new task, registers it, and runs it through the injected runner.
func (r *AgentRuntime) Start(ctx context.Context, req StartRequest) (Task, <-chan Event, error) {
	if r == nil {
		return nil, nil, ErrNilRuntime
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if r.runner == nil {
		return nil, nil, ErrNilRunner
	}

	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		taskID = r.nextTaskID()
	}

	effectiveDef := applyRuntimeOverlay(req.Definition, req.Overlay)
	effectiveCtx := mergeTaskContext(req.Context, req.Definition, req.Overlay)

	task := newTaskHandle(taskID, req.Definition, effectiveDef, req.Input, effectiveCtx, req.Overlay, r.clock, r.eventBuffer)
	if err := r.tasks.register(task); err != nil {
		return nil, nil, err
	}

	task.emit(Event{
		Type:    EventTypeStarted,
		Payload: task.Snapshot(),
	})

	change, changed, err := task.transition(TaskStatusRunning, nil, "task started")
	if err != nil {
		return nil, nil, err
	}
	if changed {
		task.emit(Event{
			Type:    EventTypeStatusChanged,
			Payload: change,
		})
	}

	runCtx, cancel := context.WithCancel(ctx)
	task.attachCancel(cancel)

	runReq := RunRequest{
		TaskID:         taskID,
		Definition:     effectiveDef,
		BaseDefinition: req.Definition,
		Input:          req.Input,
		Context:        effectiveCtx,
		Overlay:        req.Overlay,
		StartedAt:      r.clock(),
	}

	go r.runTask(runCtx, task, runReq)

	return task, task.eventCh, nil
}

// Stop requests a task stop and seals the task state.
func (r *AgentRuntime) Stop(ctx context.Context, taskID string) error {
	if r == nil {
		return ErrNilRuntime
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	task, ok := r.tasks.lookup(taskID)
	if !ok {
		return ErrTaskNotFound
	}

	change, changed, err := task.transition(TaskStatusStopped, nil, "stop requested")
	if err != nil {
		if errors.Is(err, ErrInvalidTaskTransition) && task.Status().IsTerminal() {
			return nil
		}
		return err
	}
	if !changed {
		return nil
	}

	task.seal()
	task.mu.RLock()
	cancel := task.cancel
	task.mu.RUnlock()
	if cancel != nil {
		cancel()
	}

	task.emitForced(Event{
		Type:    EventTypeStopped,
		Payload: change,
	})
	task.emitForced(Event{
		Type:    EventTypeStatusChanged,
		Payload: change,
	})
	return nil
}

// GetTask returns a snapshot of a task.
func (r *AgentRuntime) GetTask(ctx context.Context, taskID string) (TaskSnapshot, error) {
	if r == nil {
		return TaskSnapshot{}, ErrNilRuntime
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TaskSnapshot{}, err
	}

	snapshot, ok := r.tasks.snapshot(taskID)
	if !ok {
		return TaskSnapshot{}, ErrTaskNotFound
	}
	return snapshot, nil
}

// ListTasks returns all task snapshots in a deterministic order.
func (r *AgentRuntime) ListTasks(ctx context.Context) ([]TaskSnapshot, error) {
	if r == nil {
		return nil, ErrNilRuntime
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return r.tasks.list(), nil
}

func (r *AgentRuntime) nextTaskID() string {
	if r == nil {
		return ""
	}
	if r.taskIDGen == nil {
		return defaultRuntimeConfig().taskIDGen()
	}
	id := strings.TrimSpace(r.taskIDGen())
	if id == "" {
		return defaultRuntimeConfig().taskIDGen()
	}
	return id
}

func (r *AgentRuntime) runTask(ctx context.Context, task *taskHandle, req RunRequest) {
	err := r.runner.Run(ctx, req, func(event Event) {
		task.emit(event)
	})

	if task == nil || task.Status().IsTerminal() {
		return
	}

	switch {
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		change, changed, transitionErr := task.transition(TaskStatusStopped, nil, "runner canceled")
		if transitionErr != nil || !changed {
			return
		}
		task.seal()
		task.emitForced(Event{Type: EventTypeStopped, Payload: change})
		task.emitForced(Event{Type: EventTypeStatusChanged, Payload: change})
	case err != nil:
		change, changed, transitionErr := task.transition(TaskStatusFailed, err, "runner error")
		if transitionErr != nil || !changed {
			return
		}
		task.seal()
		task.emitForced(Event{Type: EventTypeFailed, Payload: change, Metadata: map[string]string{"error": err.Error()}})
		task.emitForced(Event{Type: EventTypeStatusChanged, Payload: change})
	default:
		change, changed, transitionErr := task.transition(TaskStatusCompleted, nil, "runner completed")
		if transitionErr != nil || !changed {
			return
		}
		task.seal()
		task.emitForced(Event{Type: EventTypeCompleted, Payload: change})
		task.emitForced(Event{Type: EventTypeStatusChanged, Payload: change})
	}
}

func applyRuntimeOverlay(base Definition, overlay RuntimeOverlay) Definition {
	out := cloneDefinition(base)
	if overlay.SystemPrompt != nil {
		out.SystemPrompt = *overlay.SystemPrompt
	}
	if overlay.AllowedTools != nil {
		out.AllowedTools = cloneStrings(overlay.AllowedTools)
	}
	if overlay.MCPServers != nil {
		out.MCPServers = cloneStrings(overlay.MCPServers)
	}
	if overlay.Background != nil {
		out.Background = *overlay.Background
	}
	if overlay.Isolated != nil {
		out.Isolated = *overlay.Isolated
	}
	if overlay.Metadata != nil {
		out.Metadata = mergeStringMaps(out.Metadata, overlay.Metadata)
	}
	return out
}

func mergeTaskContext(base TaskContext, definition Definition, overlay RuntimeOverlay) TaskContext {
	out := cloneTaskContext(base)
	out.Background = definition.Background
	out.Isolated = definition.Isolated

	if overlay.Background != nil {
		out.Background = *overlay.Background
	}
	if overlay.Isolated != nil {
		out.Isolated = *overlay.Isolated
	}

	out.Metadata = mergeStringMaps(definition.Metadata, out.Metadata)
	if overlay.Metadata != nil {
		out.Metadata = mergeStringMaps(out.Metadata, overlay.Metadata)
	}
	return out
}

func cloneDefinition(def Definition) Definition {
	out := def
	out.AllowedTools = cloneStrings(def.AllowedTools)
	out.MCPServers = cloneStrings(def.MCPServers)
	out.Metadata = cloneStringMap(def.Metadata)
	return out
}

func cloneTaskContext(ctx TaskContext) TaskContext {
	out := ctx
	out.Metadata = cloneStringMap(ctx.Metadata)
	return out
}

func cloneRuntimeOverlay(overlay RuntimeOverlay) RuntimeOverlay {
	out := overlay
	out.AllowedTools = cloneStrings(overlay.AllowedTools)
	out.MCPServers = cloneStrings(overlay.MCPServers)
	out.Metadata = cloneStringMap(overlay.Metadata)
	return out
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mergeStringMaps(base map[string]string, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}

	out := cloneStringMap(base)
	if out == nil {
		out = make(map[string]string, len(overlay))
	}
	for key, value := range overlay {
		out[key] = value
	}
	return out
}
