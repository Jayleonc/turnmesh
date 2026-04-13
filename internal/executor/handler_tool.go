package executor

import "context"

// ToolHandler is a generic function adapter for unified tool execution.
type ToolHandler func(context.Context, ToolRequest) (ToolOutcome, error)

// HandlerTool adapts a ToolHandler into a ToolRuntime.
type HandlerTool struct {
	spec    ToolSpec
	handler ToolHandler
}

// NewHandlerTool creates a named generic tool backed by a function handler.
func NewHandlerTool(spec ToolSpec, handler ToolHandler) *HandlerTool {
	return &HandlerTool{
		spec:    spec,
		handler: handler,
	}
}

// Spec returns the registered tool metadata.
func (t *HandlerTool) Spec() ToolSpec {
	return t.spec
}

// Execute delegates to the configured generic handler.
func (t *HandlerTool) Execute(ctx context.Context, request ToolRequest) (ToolOutcome, error) {
	if t == nil || t.handler == nil {
		return ToolOutcome{}, ErrToolNotFound
	}
	return t.handler(ctx, request)
}
