package agent

import (
	"context"
	"sync"
	"time"
)

type taskHandle struct {
	mu             sync.RWMutex
	id             string
	baseDefinition Definition
	definition     Definition
	input          any
	context        TaskContext
	overlay        RuntimeOverlay

	machine TaskLifecycleStateMachine
	clock   func() time.Time
	eventCh chan Event
	cancel  context.CancelFunc

	status     TaskStatus
	progress   float64
	summary    string
	err        error
	createdAt  time.Time
	startedAt  time.Time
	updatedAt  time.Time
	finishedAt time.Time
	sealed     bool
}

func newTaskHandle(id string, baseDefinition Definition, effectiveDefinition Definition, input any, context TaskContext, overlay RuntimeOverlay, clock func() time.Time, eventBuffer int) *taskHandle {
	if clock == nil {
		clock = time.Now
	}
	if eventBuffer <= 0 {
		eventBuffer = 1
	}

	now := clock()
	return &taskHandle{
		id:             id,
		baseDefinition: cloneDefinition(baseDefinition),
		definition:     cloneDefinition(effectiveDefinition),
		input:          input,
		context:        cloneTaskContext(context),
		overlay:        cloneRuntimeOverlay(overlay),
		machine:        TaskLifecycleStateMachine{},
		clock:          clock,
		eventCh:        make(chan Event, eventBuffer),
		status:         TaskStatusPending,
		createdAt:      now,
		updatedAt:      now,
	}
}

func (t *taskHandle) ID() string {
	if t == nil {
		return ""
	}
	return t.id
}

func (t *taskHandle) Definition() Definition {
	if t == nil {
		return Definition{}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return cloneDefinition(t.definition)
}

func (t *taskHandle) Status() TaskStatus {
	if t == nil {
		return TaskStatusUnknown
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

func (t *taskHandle) Snapshot() TaskSnapshot {
	if t == nil {
		return TaskSnapshot{}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	return TaskSnapshot{
		ID:             t.id,
		BaseDefinition: cloneDefinition(t.baseDefinition),
		Definition:     cloneDefinition(t.definition),
		Input:          t.input,
		Context:        cloneTaskContext(t.context),
		Overlay:        cloneRuntimeOverlay(t.overlay),
		Status:         t.status,
		Progress:       t.progress,
		Summary:        t.summary,
		CreatedAt:      t.createdAt,
		StartedAt:      t.startedAt,
		UpdatedAt:      t.updatedAt,
		FinishedAt:     t.finishedAt,
		Err:            t.err,
	}
}

func (t *taskHandle) attachCancel(cancel context.CancelFunc) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.cancel = cancel
	t.mu.Unlock()
}

func (t *taskHandle) transition(to TaskStatus, err error, reason string) (StatusChange, bool, error) {
	if t == nil {
		return StatusChange{}, false, ErrNilRuntime
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	from := t.status
	if from == to && from.IsTerminal() {
		return StatusChange{From: from, To: to, Reason: reason}, false, nil
	}
	if err := t.machine.Transition(from, to); err != nil {
		return StatusChange{}, false, err
	}

	now := t.clock()
	t.status = to
	t.updatedAt = now
	if to == TaskStatusRunning && t.startedAt.IsZero() {
		t.startedAt = now
	}
	if to.IsTerminal() && t.finishedAt.IsZero() {
		t.finishedAt = now
	}
	if err != nil {
		t.err = err
	}

	return StatusChange{From: from, To: to, Reason: reason}, true, nil
}

func (t *taskHandle) seal() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.sealed = true
	t.mu.Unlock()
}

func (t *taskHandle) emit(event Event) bool {
	return t.emitWithMode(event, false)
}

func (t *taskHandle) emitForced(event Event) bool {
	return t.emitWithMode(event, true)
}

func (t *taskHandle) emitWithMode(event Event, force bool) bool {
	if t == nil {
		return false
	}

	t.mu.Lock()
	if t.sealed && !force {
		t.mu.Unlock()
		return false
	}
	if event.TaskID == "" {
		event.TaskID = t.id
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = t.clock()
	}
	if event.Metadata == nil {
		event.Metadata = map[string]string{}
	}
	if event.Type == EventTypeProgress {
		switch payload := event.Payload.(type) {
		case ProgressEvent:
			if payload.Progress >= 0 {
				t.progress = payload.Progress
			}
			if payload.Summary != "" {
				t.summary = payload.Summary
			}
		case *ProgressEvent:
			if payload != nil {
				if payload.Progress >= 0 {
					t.progress = payload.Progress
				}
				if payload.Summary != "" {
					t.summary = payload.Summary
				}
			}
		}
	}
	t.updatedAt = event.Timestamp
	ch := t.eventCh
	t.mu.Unlock()

	ch <- event
	return true
}
