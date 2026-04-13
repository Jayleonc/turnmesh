package executor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jayleonc/turnmesh/internal/core"
)

func TestToolDispatcherExecutesRegisteredTool(t *testing.T) {
	registry := NewRegistryStore()
	if err := registry.Register(NewCommandTool(ToolSpec{Name: "shell"}, NewLocalCommandExecutor())); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	dispatcher := NewToolDispatcher(registry)
	input, err := json.Marshal(map[string]any{
		"command": "sh",
		"args":    []string{"-c", "printf 'hello'"},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	result, err := dispatcher.ExecuteTool(context.Background(), core.ToolInvocation{
		ID:    "tool-1",
		Tool:  "shell",
		Input: input,
	})
	if err != nil {
		t.Fatalf("ExecuteTool() error = %v", err)
	}

	if result.Status != core.ToolStatusSucceeded {
		t.Fatalf("Status = %q, want %q", result.Status, core.ToolStatusSucceeded)
	}
	if strings.TrimSpace(result.Output) != "hello" {
		t.Fatalf("Output = %q, want %q", result.Output, "hello")
	}
}
