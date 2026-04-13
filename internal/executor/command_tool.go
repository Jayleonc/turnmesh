package executor

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
)

// CommandTool adapts a CommandExecutor into a ToolRuntime.
type CommandTool struct {
	spec     ToolSpec
	executor CommandExecutor
}

// NewCommandTool creates a named tool backed by a command executor.
func NewCommandTool(spec ToolSpec, executor CommandExecutor) *CommandTool {
	if spec.Name == "" {
		spec.Name = "command"
	}

	return &CommandTool{
		spec:     spec,
		executor: executor,
	}
}

// Spec returns the registered tool metadata.
func (t *CommandTool) Spec() ToolSpec {
	return t.spec
}

// Execute delegates to the underlying command executor.
func (t *CommandTool) Execute(ctx context.Context, request ToolRequest) (ToolOutcome, error) {
	if t.executor == nil {
		return ToolOutcome{}, ErrToolNotFound
	}

	commandRequest, err := decodeCommandRequest(request)
	if err != nil {
		return ToolOutcome{}, err
	}

	result, execErr := t.executor.Execute(ctx, commandRequest)
	return outcomeFromCommandResult(result), execErr
}

type commandToolInput struct {
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Dir       string   `json:"dir"`
	Env       []string `json:"env"`
	Stdin     string   `json:"stdin"`
	TimeoutMS int64    `json:"timeout_ms"`
}

func decodeCommandRequest(request ToolRequest) (CommandRequest, error) {
	raw := request.Input
	if len(raw) == 0 {
		raw = request.Arguments
	}
	if len(raw) == 0 {
		return CommandRequest{}, errors.New("missing tool input")
	}

	var input commandToolInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return CommandRequest{}, err
	}
	if input.Command == "" {
		return CommandRequest{}, errors.New("command is required")
	}

	commandRequest := CommandRequest{
		Command: input.Command,
		Args:    append([]string(nil), input.Args...),
		Dir:     input.Dir,
		Env:     append([]string(nil), input.Env...),
		Stdin:   []byte(input.Stdin),
	}
	if input.TimeoutMS > 0 {
		commandRequest.Timeout = timeDurationFromMS(input.TimeoutMS)
	}

	return commandRequest, nil
}

func outcomeFromCommandResult(result CommandResult) ToolOutcome {
	metadata := map[string]string{
		"exit_code": strconv.Itoa(result.ExitCode),
		"stderr":    result.Stderr,
	}
	if result.Command != "" {
		metadata["command"] = result.Command
	}
	if result.Dir != "" {
		metadata["dir"] = result.Dir
	}
	if result.TimedOut {
		metadata["timed_out"] = "true"
	}
	if result.Canceled {
		metadata["canceled"] = "true"
	}

	output := result.Stdout
	if output == "" && result.Stderr != "" {
		output = result.Stderr
	}

	outcome := ToolOutcome{
		Output:   output,
		Metadata: metadata,
		Duration: result.Duration,
		Status:   core.ToolStatusSucceeded,
	}
	if result.Canceled {
		outcome.Status = core.ToolStatusCancelled
	}
	if structured, err := json.Marshal(result); err == nil {
		outcome.Structured = structured
	}

	return outcome
}

func timeDurationFromMS(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}
