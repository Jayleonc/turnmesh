package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Jayleonc/turnmesh/internal/core"
)

type adapterStubProvider struct {
	tools    []Tool
	callReq  CallRequest
	callResp CallResult
	callErr  error
}

func (p *adapterStubProvider) Capabilities(context.Context) ([]Capability, error) { return nil, nil }

func (p *adapterStubProvider) Tools(context.Context) ([]Tool, error) {
	return append([]Tool(nil), p.tools...), nil
}

func (p *adapterStubProvider) Resources(context.Context) ([]Resource, error) { return nil, nil }

func (p *adapterStubProvider) Prompts(context.Context) ([]Prompt, error) { return nil, nil }

func (p *adapterStubProvider) Call(_ context.Context, request CallRequest) (CallResult, error) {
	p.callReq = request
	return p.callResp, p.callErr
}

func TestBuildAndParseToolName(t *testing.T) {
	t.Parallel()

	if got := BuildToolName("slack", "read_channel"); got != "mcp__slack__read_channel" {
		t.Fatalf("build tool name = %q", got)
	}

	server, tool, ok := ParseToolName("mcp__my__server__tool")
	if !ok {
		t.Fatal("expected tool name to parse")
	}
	if server != "my" || tool != "server__tool" {
		t.Fatalf("unexpected parse result: %q %q", server, tool)
	}

	if _, _, ok := ParseToolName("read_channel"); ok {
		t.Fatal("expected plain tool name to be rejected")
	}
}

func TestToolAdapterListToolsQualifiesAndPreservesMetadata(t *testing.T) {
	t.Parallel()

	provider := &adapterStubProvider{
		tools: []Tool{
			{
				Name:        "mcp__slack__echo",
				Description: "Echo a message",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"message": map[string]any{"type": "string"},
					},
				},
				Metadata: map[string]string{"source": "unit"},
			},
		},
	}
	adapter, err := NewToolAdapter("slack", provider)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	specs, err := adapter.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}

	spec := specs[0]
	if spec.Name != "mcp__slack__echo" {
		t.Fatalf("unexpected spec name: %q", spec.Name)
	}
	if spec.Description != "Echo a message" {
		t.Fatalf("unexpected description: %q", spec.Description)
	}

	var schema map[string]any
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("unexpected schema type: %#v", schema)
	}

	if spec.Metadata["source"] != "unit" {
		t.Fatalf("missing source metadata: %#v", spec.Metadata)
	}
	if spec.Metadata[MetadataServerNameKey] != "slack" {
		t.Fatalf("missing server metadata: %#v", spec.Metadata)
	}
	if spec.Metadata[MetadataToolNameKey] != "echo" {
		t.Fatalf("missing tool metadata: %#v", spec.Metadata)
	}
	if spec.Metadata[MetadataNameKey] != "mcp__slack__echo" {
		t.Fatalf("missing fq name metadata: %#v", spec.Metadata)
	}
}

func TestToolAdapterInvokeConvertsRequestAndResult(t *testing.T) {
	t.Parallel()

	provider := &adapterStubProvider{
		callResp: CallResult{
			Content: map[string]any{"ok": true},
			Metadata: map[string]string{
				"trace": "123",
			},
		},
	}
	adapter, err := NewToolAdapter("slack", provider)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	result, err := adapter.Invoke(context.Background(), core.ToolInvocation{
		ID:    "inv-1",
		Tool:  "mcp__slack__echo",
		Input: json.RawMessage(`{"message":"hello"}`),
		Metadata: map[string]string{
			"request": "abc",
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}

	if provider.callReq.Name != "echo" {
		t.Fatalf("unexpected call request name: %q", provider.callReq.Name)
	}
	if provider.callReq.Arguments["message"] != "hello" {
		t.Fatalf("unexpected call arguments: %#v", provider.callReq.Arguments)
	}
	if provider.callReq.Metadata["request"] != "abc" {
		t.Fatalf("missing request metadata: %#v", provider.callReq.Metadata)
	}
	if provider.callReq.Metadata[MetadataServerNameKey] != "slack" {
		t.Fatalf("missing server metadata: %#v", provider.callReq.Metadata)
	}
	if provider.callReq.Metadata[MetadataToolNameKey] != "echo" {
		t.Fatalf("missing tool metadata: %#v", provider.callReq.Metadata)
	}

	if result.InvocationID != "inv-1" {
		t.Fatalf("unexpected invocation id: %q", result.InvocationID)
	}
	if result.Tool != "mcp__slack__echo" {
		t.Fatalf("unexpected result tool: %q", result.Tool)
	}
	if result.Status != core.ToolStatusSucceeded {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.Metadata["trace"] != "123" {
		t.Fatalf("missing result metadata: %#v", result.Metadata)
	}
	if result.Metadata[MetadataServerNameKey] != "slack" {
		t.Fatalf("missing result server metadata: %#v", result.Metadata)
	}

	var structured map[string]any
	if err := json.Unmarshal(result.Structured, &structured); err != nil {
		t.Fatalf("unmarshal structured result: %v", err)
	}
	if structured["ok"] != true {
		t.Fatalf("unexpected structured result: %#v", structured)
	}
}

func TestToolAdapterInvokeRejectsMismatchedServer(t *testing.T) {
	t.Parallel()

	provider := &adapterStubProvider{}
	adapter, err := NewToolAdapter("slack", provider)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	result, err := adapter.Invoke(context.Background(), core.ToolInvocation{
		Tool: "mcp__other__echo",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidMcpToolName) {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != core.ToolStatusFailed {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if provider.callReq.Name != "" {
		t.Fatalf("provider should not be called: %#v", provider.callReq)
	}
}

func TestToolAdapterInvokeMapsApplicationError(t *testing.T) {
	t.Parallel()

	provider := &adapterStubProvider{
		callResp: CallResult{Error: "boom"},
	}
	adapter, err := NewToolAdapter("slack", provider)
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}

	result, err := adapter.Invoke(context.Background(), core.ToolInvocation{
		Tool: "mcp__slack__echo",
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result.Status != core.ToolStatusFailed {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	if result.Error == nil || result.Error.Message != "boom" {
		t.Fatalf("unexpected result error: %#v", result.Error)
	}
}
