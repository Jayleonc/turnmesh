package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Jayleonc/turnmesh/internal/agent"
	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/mcp"
	"github.com/Jayleonc/turnmesh/internal/memory"
	"github.com/Jayleonc/turnmesh/internal/model"
)

func TestBuildRuntimeWithoutProviderBuildsBootableEngine(t *testing.T) {
	t.Parallel()

	runtime, err := BuildRuntime(context.Background(), Config{})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer runtime.Close()

	if runtime.Engine == nil {
		t.Fatal("Engine is nil")
	}
	if runtime.Providers == nil {
		t.Fatal("Providers is nil")
	}
	if runtime.Tools == nil {
		t.Fatal("Tools is nil")
	}
	if runtime.Batch == nil {
		t.Fatal("Batch is nil")
	}
	if runtime.Memory == nil {
		t.Fatal("Memory is nil")
	}
	if runtime.Agents == nil {
		t.Fatal("Agents is nil")
	}
	if runtime.Session != nil {
		t.Fatalf("Session = %#v, want nil", runtime.Session)
	}

	if err := runtime.Engine.Boot(context.Background()); err != nil {
		t.Fatalf("Boot() error = %v", err)
	}

	specs := runtime.Tools.List()
	if len(specs) == 0 || specs[0].Name != "shell" {
		t.Fatalf("tools = %#v, want shell registered", specs)
	}
}

func TestBuildRuntimeAssemblesProviderAndMCPToolSurface(t *testing.T) {
	t.Parallel()

	registry := model.NewRegistry()
	stubProvider := &bootstrapStubProvider{name: "stub"}
	if err := registry.Register(stubProvider); err != nil {
		t.Fatalf("Register(stub) error = %v", err)
	}

	mcpProvider := &bootstrapStubMCPProvider{
		tools: []mcp.Tool{{
			Name:        "echo",
			Description: "Echo one message",
			InputSchema: map[string]any{"type": "object"},
		}},
		callResult: mcp.CallResult{
			Content: map[string]any{"ok": true},
		},
	}

	runtime, err := BuildRuntime(context.Background(), Config{
		Provider:  "stub",
		Providers: registry,
		MCPServers: []MCPServer{{
			Name:     "slack",
			Provider: mcpProvider,
		}},
	})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer runtime.Close()

	if runtime.Session == nil {
		t.Fatal("Session is nil")
	}
	if err := runtime.Engine.Boot(context.Background()); err != nil {
		t.Fatalf("Boot() error = %v", err)
	}

	if len(stubProvider.lastOptions.Tools) != 2 {
		t.Fatalf("session tool catalog len = %d, want 2", len(stubProvider.lastOptions.Tools))
	}
	if stubProvider.lastOptions.Tools[0].Name != "mcp__slack__echo" && stubProvider.lastOptions.Tools[1].Name != "mcp__slack__echo" {
		t.Fatalf("session tools = %#v, want mcp__slack__echo present", stubProvider.lastOptions.Tools)
	}

	dispatcherResult, err := runtime.Engine.StreamTurn(context.Background(), core.TurnInput{})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}
	events := collectTurnEvents(dispatcherResult)
	if len(events) < 5 {
		t.Fatalf("events len = %d, want >= 5", len(events))
	}
	if events[1].Kind != core.TurnEventToolCall {
		t.Fatalf("event[1] kind = %q, want tool_call", events[1].Kind)
	}
	if events[2].Kind != core.TurnEventToolResult {
		t.Fatalf("event[2] kind = %q, want tool_result", events[2].Kind)
	}
	if events[2].ToolResult == nil || events[2].ToolResult.Tool != "mcp__slack__echo" {
		t.Fatalf("tool result = %#v, want mcp__slack__echo", events[2].ToolResult)
	}
	if events[3].Kind != core.TurnEventMessage || events[3].Message == nil || events[3].Message.Content != "done" {
		t.Fatalf("event[3] = %#v, want assistant done message", events[3])
	}

	if mcpProvider.lastCall.Name != "echo" {
		t.Fatalf("mcp call name = %q, want echo", mcpProvider.lastCall.Name)
	}
	if mcpProvider.lastCall.Arguments["message"] != "hello" {
		t.Fatalf("mcp call args = %#v, want hello", mcpProvider.lastCall.Arguments)
	}
}

func TestBuildRuntimeWritesSessionMemory(t *testing.T) {
	t.Parallel()

	registry := model.NewRegistry()
	stubProvider := &bootstrapStubProvider{
		name: "stub",
		sessionFactory: func(context.Context, model.SessionOptions) (model.Session, error) {
			return &bootstrapStubSession{
				streams: [][]core.TurnEvent{{
					{
						Kind:    core.TurnEventMessage,
						Message: &core.Message{Role: core.MessageRoleAssistant, Content: "stored"},
					},
					{Kind: core.TurnEventCompleted},
				}},
			}, nil
		},
	}
	if err := registry.Register(stubProvider); err != nil {
		t.Fatalf("Register(stub) error = %v", err)
	}

	mem := memory.NewRuntime(memory.NewInMemoryStore(), nil)
	runtime, err := BuildRuntime(context.Background(), Config{
		Provider:  "stub",
		Providers: registry,
		Memory:    mem,
	})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer runtime.Close()

	if err := runtime.Engine.Boot(context.Background()); err != nil {
		t.Fatalf("Boot() error = %v", err)
	}

	stream, err := runtime.Engine.StreamTurn(context.Background(), core.TurnInput{
		SessionID: "session-memory",
		TurnID:    "turn-memory",
		Messages: []core.Message{
			{Role: core.MessageRoleUser, Content: "remember this"},
		},
	})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}
	_ = collectTurnEvents(stream)

	entries, err := mem.Store.List(context.Background(), memory.Query{
		Scope:    memory.ScopeSession,
		Metadata: map[string]string{"session_id": "session-memory"},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected memory writeback entry")
	}
	if entries[0].Metadata["turn_id"] != "turn-memory" {
		t.Fatalf("turn metadata = %q, want turn-memory", entries[0].Metadata["turn_id"])
	}
	if entries[0].Content == "" {
		t.Fatal("memory entry content is empty")
	}
}

func TestBuildRuntimeExposesIntegratedAgentRuntime(t *testing.T) {
	t.Parallel()

	registry := model.NewRegistry()
	stubProvider := &bootstrapStubProvider{
		name: "stub",
		sessionFactory: func(context.Context, model.SessionOptions) (model.Session, error) {
			return &bootstrapStubSession{
				streams: [][]core.TurnEvent{{
					{
						Kind:    core.TurnEventMessage,
						Message: &core.Message{Role: core.MessageRoleAssistant, Content: "agent done"},
					},
					{Kind: core.TurnEventCompleted},
				}},
			}, nil
		},
	}
	if err := registry.Register(stubProvider); err != nil {
		t.Fatalf("Register(stub) error = %v", err)
	}

	runtime, err := BuildRuntime(context.Background(), Config{
		Provider:  "stub",
		Providers: registry,
	})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	defer runtime.Close()

	task, events, err := runtime.Agents.Start(context.Background(), agent.StartRequest{
		TaskID: "agent-task",
		Definition: agent.Definition{
			ID:           "agent-1",
			Name:         "helper",
			SystemPrompt: "be helpful",
			AllowedTools: []string{"shell"},
		},
		Input: "say hi",
		Context: agent.TaskContext{
			SessionID: "agent-session",
		},
	})
	if err != nil {
		t.Fatalf("Agents.Start() error = %v", err)
	}

	agentEvents := collectAgentEvents(t, events, 5, time.Second)
	if len(agentEvents) != 5 {
		t.Fatalf("len(agentEvents) = %d, want 5", len(agentEvents))
	}
	if task.ID() != "agent-task" {
		t.Fatalf("task id = %q, want agent-task", task.ID())
	}
	if agentEvents[2].Type != agent.EventTypeMessage {
		t.Fatalf("event[2] type = %q, want message", agentEvents[2].Type)
	}
	if stubProvider.lastOptions.SystemPrompt != "be helpful" {
		t.Fatalf("session prompt = %q, want be helpful", stubProvider.lastOptions.SystemPrompt)
	}
	if len(stubProvider.lastOptions.Tools) != 1 || stubProvider.lastOptions.Tools[0].Name != "shell" {
		t.Fatalf("session tools = %#v, want filtered shell catalog", stubProvider.lastOptions.Tools)
	}

	snapshot, err := runtime.Agents.GetTask(context.Background(), "agent-task")
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if snapshot.Status != agent.TaskStatusCompleted {
		t.Fatalf("snapshot.Status = %q, want completed", snapshot.Status)
	}
}

func collectTurnEvents(ch <-chan core.TurnEvent) []core.TurnEvent {
	var events []core.TurnEvent
	for event := range ch {
		events = append(events, event)
	}
	return events
}

func collectAgentEvents(t *testing.T, ch <-chan agent.Event, want int, timeout time.Duration) []agent.Event {
	t.Helper()

	events := make([]agent.Event, 0, want)
	deadline := time.After(timeout)
	for len(events) < want {
		select {
		case event := <-ch:
			events = append(events, event)
		case <-deadline:
			t.Fatalf("timed out waiting for %d agent events, got %d", want, len(events))
		}
	}
	return events
}

type bootstrapStubProvider struct {
	name           string
	lastOptions    model.SessionOptions
	sessionFactory func(context.Context, model.SessionOptions) (model.Session, error)
}

func (p *bootstrapStubProvider) Name() string {
	return p.name
}

func (p *bootstrapStubProvider) ListModels(context.Context) ([]model.ModelInfo, error) {
	return []model.ModelInfo{{Name: "stub-model"}}, nil
}

func (p *bootstrapStubProvider) NewSession(ctx context.Context, opts model.SessionOptions) (model.Session, error) {
	p.lastOptions = opts
	if p.sessionFactory != nil {
		return p.sessionFactory(ctx, opts)
	}
	return &bootstrapStubSession{
		streams: [][]core.TurnEvent{
			{
				{
					Kind:     core.TurnEventToolCall,
					ToolCall: &core.ToolInvocation{ID: "tool-1", Tool: "mcp__slack__echo", Arguments: json.RawMessage(`{"message":"hello"}`)},
				},
				{Kind: core.TurnEventCompleted},
			},
			{
				{
					Kind:    core.TurnEventMessage,
					Message: &core.Message{Role: core.MessageRoleAssistant, Content: "done"},
				},
				{Kind: core.TurnEventCompleted},
			},
		},
	}, nil
}

type bootstrapStubSession struct {
	streams [][]core.TurnEvent
}

func (s *bootstrapStubSession) ID() string       { return "stub-session" }
func (s *bootstrapStubSession) Provider() string { return "stub" }
func (s *bootstrapStubSession) Model() string    { return "stub-model" }
func (s *bootstrapStubSession) Capabilities() model.Capabilities {
	return model.Capabilities{CanToolCall: true}
}
func (s *bootstrapStubSession) Close() error { return nil }

func (s *bootstrapStubSession) StreamTurn(context.Context, core.TurnInput) (<-chan core.TurnEvent, error) {
	var events []core.TurnEvent
	if len(s.streams) > 0 {
		events = s.streams[0]
		s.streams = s.streams[1:]
	}
	ch := make(chan core.TurnEvent, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

type bootstrapStubMCPProvider struct {
	tools      []mcp.Tool
	lastCall   mcp.CallRequest
	callResult mcp.CallResult
}

func (p *bootstrapStubMCPProvider) Capabilities(context.Context) ([]mcp.Capability, error) {
	return nil, nil
}

func (p *bootstrapStubMCPProvider) Tools(context.Context) ([]mcp.Tool, error) {
	return append([]mcp.Tool(nil), p.tools...), nil
}

func (p *bootstrapStubMCPProvider) Resources(context.Context) ([]mcp.Resource, error) {
	return nil, nil
}

func (p *bootstrapStubMCPProvider) Prompts(context.Context) ([]mcp.Prompt, error) {
	return nil, nil
}

func (p *bootstrapStubMCPProvider) Call(_ context.Context, request mcp.CallRequest) (mcp.CallResult, error) {
	p.lastCall = request
	return p.callResult, nil
}
