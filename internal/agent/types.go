package agent

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNilRunner             = errors.New("agent: nil runner")
	ErrNilRuntime            = errors.New("agent: nil runtime")
	ErrTaskNotFound          = errors.New("agent: task not found")
	ErrTaskAlreadyExists     = errors.New("agent: task already exists")
	ErrInvalidTaskTransition = errors.New("agent: invalid task transition")
)

// Definition describes an agent or subagent without binding the kernel to a provider.
type Definition struct {
	ID           string
	Name         string
	Description  string
	SystemPrompt string
	ModelHint    string
	AllowedTools []string
	MCPServers   []string
	Background   bool
	Isolated     bool
	Metadata     map[string]string
}

// TaskStatus captures the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusUnknown   TaskStatus = ""
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusStopped   TaskStatus = "stopped"
)

// IsTerminal reports whether the status is a terminal state.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusStopped:
		return true
	default:
		return false
	}
}

// Event is the normalized runtime feedback emitted by an agent task.
type Event struct {
	TaskID    string
	Type      string
	Payload   any
	Timestamp time.Time
	Metadata  map[string]string
}

const (
	EventTypeStarted       = "started"
	EventTypeStatusChanged = "status_changed"
	EventTypeProgress      = "progress"
	EventTypeCompleted     = "completed"
	EventTypeFailed        = "failed"
	EventTypeStopped       = "stopped"
	EventTypeMessage       = "message"
	EventTypeError         = "error"
)

// StatusChange records a lifecycle transition.
type StatusChange struct {
	From   TaskStatus
	To     TaskStatus
	Reason string
}

// ProgressEvent records a normalized progress update.
type ProgressEvent struct {
	Progress float64
	Summary  string
	Metadata map[string]string
}

// TaskContext carries the task-local execution context.
type TaskContext struct {
	ParentTaskID string
	SessionID    string
	Background   bool
	Isolated     bool
	Metadata     map[string]string
}

// RuntimeOverlay describes local overrides applied to the parent runtime.
type RuntimeOverlay struct {
	SystemPrompt *string
	AllowedTools []string
	MCPServers   []string
	Background   *bool
	Isolated     *bool
	Metadata     map[string]string
}

// StartRequest is the input required to start an agent task.
type StartRequest struct {
	TaskID     string
	Definition Definition
	Input      any
	Context    TaskContext
	Overlay    RuntimeOverlay
}

// RunRequest is what the injected runner receives once the runtime has resolved the effective task context.
type RunRequest struct {
	TaskID         string
	Definition     Definition
	BaseDefinition Definition
	Input          any
	Context        TaskContext
	Overlay        RuntimeOverlay
	StartedAt      time.Time
}

// TaskSnapshot captures the current or terminal state of a task.
type TaskSnapshot struct {
	ID             string
	BaseDefinition Definition
	Definition     Definition
	Input          any
	Context        TaskContext
	Overlay        RuntimeOverlay
	Status         TaskStatus
	Progress       float64
	Summary        string
	CreatedAt      time.Time
	StartedAt      time.Time
	UpdatedAt      time.Time
	FinishedAt     time.Time
	Err            error
}

// Task abstracts a live agent invocation.
type Task interface {
	ID() string
	Definition() Definition
	Status() TaskStatus
	Snapshot() TaskSnapshot
}

// Runner executes an agent task while the runtime owns lifecycle and task registration.
type Runner interface {
	Run(ctx context.Context, req RunRequest, emit func(Event)) error
}

// RunnerFunc adapts a function into a Runner.
type RunnerFunc func(ctx context.Context, req RunRequest, emit func(Event)) error

// Run executes the function as a Runner.
func (f RunnerFunc) Run(ctx context.Context, req RunRequest, emit func(Event)) error {
	if f == nil {
		return ErrNilRunner
	}
	return f(ctx, req, emit)
}

// Runtime is the minimal agent/subagent execution boundary.
type Runtime interface {
	Start(ctx context.Context, req StartRequest) (Task, <-chan Event, error)
	Stop(ctx context.Context, taskID string) error
	GetTask(ctx context.Context, taskID string) (TaskSnapshot, error)
	ListTasks(ctx context.Context) ([]TaskSnapshot, error)
}
