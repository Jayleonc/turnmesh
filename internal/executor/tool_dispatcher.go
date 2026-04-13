package executor

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Jayleonc/turnmesh/internal/core"
)

// ToolDispatcher adapts the command runtime to the kernel tool protocol.
type ToolDispatcher struct {
	runtime Runtime
}

// NewToolDispatcher creates a dispatcher backed by the provided runtime.
func NewToolDispatcher(runtime Runtime) *ToolDispatcher {
	if runtime == nil {
		runtime = NewRegistryStore()
	}

	return &ToolDispatcher{runtime: runtime}
}

// ExecuteTool decodes the invocation payload and routes it through the runtime.
func (d *ToolDispatcher) ExecuteTool(ctx context.Context, call core.ToolInvocation) (core.ToolResult, error) {
	result, execErr := d.runtime.Execute(ctx, call.Tool, toolRequestFromInvocation(call))
	toolResult := core.ToolResult{
		InvocationID: call.ID,
		Tool:         call.Tool,
		Status:       result.Status,
		Output:       result.Output,
		Duration:     result.Duration,
		Metadata:     cloneStringMap(result.Metadata),
		Structured:   result.Structured,
		Error:        result.Error,
	}

	if toolResult.Status == "" {
		toolResult.Status = core.ToolStatusSucceeded
	}

	if execErr != nil {
		toolResult.Status = core.ToolStatusFailed
		if toolResult.Error == nil {
			toolResult.Error = mapExecutorError(execErr)
		}
		return toolResult, execErr
	}

	return toolResult, nil
}

func toolRequestFromInvocation(call core.ToolInvocation) ToolRequest {
	request := ToolRequest{
		Tool:       call.Tool,
		Input:      cloneRawMessage(call.Input),
		Arguments:  cloneRawMessage(call.Arguments),
		Caller:     call.Caller,
		ApprovalID: call.ApprovalID,
		Metadata:   cloneStringMap(call.Metadata),
	}

	return request
}

func mapExecutorError(err error) *core.Error {
	switch {
	case errors.Is(err, context.Canceled):
		return core.WrapError(core.ErrorCodeCancelled, "tool execution cancelled", err)
	case errors.Is(err, context.DeadlineExceeded):
		return core.WrapError(core.ErrorCodeTimeout, "tool execution timed out", err)
	case errors.Is(err, ErrToolNotFound):
		return core.WrapError(core.ErrorCodeNotFound, "tool not found", err)
	default:
		return core.WrapError(core.ErrorCodeInternal, "tool execution failed", err)
	}
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}

	return append(json.RawMessage(nil), raw...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}
