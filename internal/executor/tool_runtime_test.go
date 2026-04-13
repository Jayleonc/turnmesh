package executor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jayleonc/turnmesh/internal/core"
)

func TestCommandToolExecutesCommandPayload(t *testing.T) {
	tool := NewCommandTool(ToolSpec{Name: "shell"}, NewLocalCommandExecutor())

	outcome, err := tool.Execute(context.Background(), ToolRequest{
		Input: json.RawMessage(`{"command":"sh","args":["-c","printf 'hello'"]}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got, want := strings.TrimSpace(outcome.Output), "hello"; got != want {
		t.Fatalf("Output = %q, want %q", got, want)
	}
	if outcome.Status != core.ToolStatusSucceeded {
		t.Fatalf("Status = %q, want %q", outcome.Status, core.ToolStatusSucceeded)
	}
	if outcome.Metadata["exit_code"] != "0" {
		t.Fatalf("exit_code metadata = %q, want 0", outcome.Metadata["exit_code"])
	}
}

func TestToolDispatcherRoutesGenericToolWithoutCommandPayload(t *testing.T) {
	registry := NewRegistryStore()
	if err := registry.Register(&rawEchoTool{spec: ToolSpec{Name: "echo"}}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	dispatcher := NewToolDispatcher(registry)
	result, err := dispatcher.ExecuteTool(context.Background(), core.ToolInvocation{
		ID:        "tool-1",
		Tool:      "echo",
		Arguments: json.RawMessage(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}

	if result.Status != core.ToolStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, core.ToolStatusSucceeded)
	}
	if got, want := result.Output, `{"message":"hello"}`; got != want {
		t.Fatalf("Output = %q, want %q", got, want)
	}
	if got, want := string(result.Structured), `{"message":"hello"}`; got != want {
		t.Fatalf("Structured = %q, want %q", got, want)
	}
}

func TestToolDispatcherPreservesSemanticToolError(t *testing.T) {
	registry := NewRegistryStore()
	if err := registry.Register(NewHandlerTool(ToolSpec{Name: "semantic-fail"}, func(context.Context, ToolRequest) (ToolOutcome, error) {
		return ToolOutcome{
			Status: core.ToolStatusFailed,
			Error:  core.NewError(core.ErrorCodeInternal, "semantic failure"),
		}, nil
	})); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	dispatcher := NewToolDispatcher(registry)
	result, err := dispatcher.ExecuteTool(context.Background(), core.ToolInvocation{
		ID:   "tool-2",
		Tool: "semantic-fail",
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}
	if result.Status != core.ToolStatusFailed {
		t.Fatalf("Status = %q, want %q", result.Status, core.ToolStatusFailed)
	}
	if result.Error == nil || result.Error.Message != "semantic failure" {
		t.Fatalf("Error = %#v, want semantic failure", result.Error)
	}
}

type rawEchoTool struct {
	spec ToolSpec
}

func (t *rawEchoTool) Spec() ToolSpec {
	return t.spec
}

func (t *rawEchoTool) Execute(_ context.Context, request ToolRequest) (ToolOutcome, error) {
	raw := request.Input
	if len(raw) == 0 {
		raw = request.Arguments
	}

	cloned := append(json.RawMessage(nil), raw...)
	return ToolOutcome{
		Output:     string(raw),
		Structured: cloned,
		Metadata: map[string]string{
			"mode": "generic",
		},
		Status: core.ToolStatusSucceeded,
	}, nil
}
