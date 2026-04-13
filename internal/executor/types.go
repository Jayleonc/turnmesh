package executor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
)

// ToolSpec describes a tool that can be registered in the runtime.
type ToolSpec struct {
	Name            string
	Description     string
	InputSchema     json.RawMessage
	OutputSchema    json.RawMessage
	ReadOnly        bool
	ConcurrencySafe bool
	Timeout         time.Duration
	Metadata        map[string]string
}

// CommandRequest is the normalized input for local command execution.
type CommandRequest struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
	Stdin   []byte
	Timeout time.Duration
}

// CommandResult is the normalized output from command execution.
type CommandResult struct {
	Command    string
	Args       []string
	Dir        string
	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration
	ExitCode   int
	Stdout     string
	Stderr     string
	TimedOut   bool
	Canceled   bool
}

// ToolRequest is the normalized input for generic tool execution.
type ToolRequest struct {
	Tool       string
	Input      json.RawMessage
	Arguments  json.RawMessage
	Caller     string
	ApprovalID string
	Metadata   map[string]string
}

// ToolOutcome is the normalized output from a generic tool execution.
type ToolOutcome struct {
	Output     string
	Structured json.RawMessage
	Metadata   map[string]string
	Duration   time.Duration
	Status     core.ToolStatus
	Error      *core.Error
}

// CommandExecutor executes one normalized command request.
type CommandExecutor interface {
	Execute(ctx context.Context, request CommandRequest) (CommandResult, error)
}

// ToolRuntime is a named executable tool that can be registered in a registry.
type ToolRuntime interface {
	Spec() ToolSpec
	Execute(ctx context.Context, request ToolRequest) (ToolOutcome, error)
}

// Registry provides lookup and registration for tools.
type Registry interface {
	Register(tool ToolRuntime) error
	Lookup(name string) (ToolRuntime, bool)
	List() []ToolSpec
}

// Runtime is the executable facade for registered tools.
type Runtime interface {
	Registry
	Execute(ctx context.Context, name string, request ToolRequest) (ToolOutcome, error)
}

// Dispatcher executes normalized tool invocations for the orchestrator.
type Dispatcher interface {
	ExecuteTool(ctx context.Context, call core.ToolInvocation) (core.ToolResult, error)
}
