package openaichat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/model"
)

func TestSessionStreamsAssistantTextAndToolCalls(t *testing.T) {
	t.Parallel()

	var captured chatCompletionsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("method = %s, want %s", got, want)
		}
		if got, want := r.URL.Path, "/chat/completions"; got != want {
			t.Fatalf("path = %s, want %s", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer sk-test"; got != want {
			t.Fatalf("authorization = %q, want %q", got, want)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		_ = json.NewEncoder(w).Encode(chatCompletionsResponse{
			ID: "chatcmpl_1",
			Choices: []chatCompletionChoice{
				{
					Index: 0,
					Message: chatMessage{
						Role:    string(core.MessageRoleAssistant),
						Content: "hello from openaichat",
						ToolCalls: []chatToolCall{
							{
								ID:   "call_1",
								Type: "function",
								Function: chatToolCallFunction{
									Name:      "lookup",
									Arguments: `{"query":"alpha"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		})
	}))
	defer server.Close()

	p := NewProvider(
		WithAPIKey("sk-test"),
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	sess, err := p.NewSession(context.Background(), model.SessionOptions{
		Model:        "gpt-4o-mini",
		SystemPrompt: "be concise",
		Metadata:     map[string]string{"origin": "test"},
		Tools: []core.ToolSpec{
			{Name: "lookup", Description: "look something up"},
		},
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	events, err := sess.StreamTurn(context.Background(), core.TurnInput{
		SystemPrompt: "prefer short answers",
		Messages: []core.Message{
			{Role: core.MessageRoleUser, Content: "say hello"},
		},
		Tools: []core.ToolSpec{
			{Name: "lookup", Description: "look something up"},
		},
	})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}

	got := collectEvents(t, events)
	if len(got) != 4 {
		t.Fatalf("events = %d, want 4", len(got))
	}
	if got[1].Kind != core.TurnEventMessage || got[1].Message == nil || got[1].Message.Content != "hello from openaichat" {
		t.Fatalf("message event = %#v, want assistant text", got[1])
	}
	if got[2].Kind != core.TurnEventToolCall || got[2].ToolCall == nil || got[2].ToolCall.Tool != "lookup" {
		t.Fatalf("tool call event = %#v, want lookup", got[2])
	}
	if got[2].ToolCall.ID != "call_1" {
		t.Fatalf("tool call id = %q, want call_1", got[2].ToolCall.ID)
	}
	if string(got[2].ToolCall.Arguments) != `{"query":"alpha"}` {
		t.Fatalf("tool call arguments = %s, want %s", string(got[2].ToolCall.Arguments), `{"query":"alpha"}`)
	}
	if got[3].Kind != core.TurnEventCompleted || got[3].Status != core.TurnStatusWaiting {
		t.Fatalf("completed event = %#v, want waiting", got[3])
	}

	if captured.Model != "gpt-4o-mini" {
		t.Fatalf("model = %s, want gpt-4o-mini", captured.Model)
	}
	if len(captured.Messages) != 2 {
		t.Fatalf("request messages = %d, want 2", len(captured.Messages))
	}
	if captured.Messages[0].Role != string(core.MessageRoleSystem) || captured.Messages[0].Content != "be concise\n\nprefer short answers" {
		t.Fatalf("system message = %#v, want merged prompt", captured.Messages[0])
	}
	if captured.Messages[1].Role != string(core.MessageRoleUser) || captured.Messages[1].Content != "say hello" {
		t.Fatalf("user message = %#v, want user text", captured.Messages[1])
	}
	if len(captured.Tools) != 1 || captured.Tools[0].Function.Name != "lookup" {
		t.Fatalf("tools = %#v, want lookup", captured.Tools)
	}
	if captured.ToolChoice != defaultToolChoiceValue {
		t.Fatalf("tool_choice = %#v, want %q", captured.ToolChoice, defaultToolChoiceValue)
	}
	if captured.ParallelToolCalls == nil || !*captured.ParallelToolCalls {
		t.Fatal("parallel_tool_calls not enabled")
	}
}

func TestSessionBuildsToolResultMessages(t *testing.T) {
	t.Parallel()

	var captured chatCompletionsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(chatCompletionsResponse{
			ID: "chatcmpl_2",
			Choices: []chatCompletionChoice{
				{
					Index: 0,
					Message: chatMessage{
						Role:    string(core.MessageRoleAssistant),
						Content: "done",
					},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer server.Close()

	p := NewProvider(
		WithAPIKey("sk-test"),
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	sess, err := p.NewSession(context.Background(), model.SessionOptions{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	events, err := sess.StreamTurn(context.Background(), core.TurnInput{
		Messages: []core.Message{
			{
				Role: core.MessageRoleAssistant,
				Parts: []core.MessagePart{
					{
						Type: core.MessagePartToolCall,
						ToolCall: &core.ToolInvocation{
							ID:        "call_1",
							Tool:      "lookup",
							Arguments: json.RawMessage(`{"query":"alpha"}`),
						},
					},
				},
			},
			{
				Role: core.MessageRoleTool,
				Parts: []core.MessagePart{
					{
						Type: core.MessagePartToolResult,
						ToolResult: &core.ToolResult{
							InvocationID: "call_1",
							Tool:         "lookup",
							Status:       core.ToolStatusSucceeded,
							Output:       "alpha-result",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}

	got := collectEvents(t, events)
	if len(got) != 3 {
		t.Fatalf("events = %d, want 3", len(got))
	}
	if got[1].Kind != core.TurnEventMessage || got[1].Message == nil || got[1].Message.Content != "done" {
		t.Fatalf("message event = %#v, want assistant text", got[1])
	}

	if len(captured.Messages) != 2 {
		t.Fatalf("request messages = %d, want 2", len(captured.Messages))
	}
	if captured.Messages[0].Role != string(core.MessageRoleAssistant) || len(captured.Messages[0].ToolCalls) != 1 {
		t.Fatalf("assistant history = %#v, want tool call", captured.Messages[0])
	}
	if captured.Messages[0].ToolCalls[0].ID != "call_1" || captured.Messages[0].ToolCalls[0].Function.Name != "lookup" {
		t.Fatalf("assistant tool call = %#v, want call_1 lookup", captured.Messages[0].ToolCalls[0])
	}
	if captured.Messages[1].Role != string(core.MessageRoleTool) || captured.Messages[1].ToolCallID != "call_1" {
		t.Fatalf("tool history = %#v, want tool_call_id call_1", captured.Messages[1])
	}
	if captured.Messages[1].Content != "alpha-result" {
		t.Fatalf("tool history content = %#v, want alpha-result", captured.Messages[1].Content)
	}
}

func TestSessionBuildsImageURLContent(t *testing.T) {
	t.Parallel()

	var captured chatCompletionsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(chatCompletionsResponse{
			ID: "chatcmpl_image",
			Choices: []chatCompletionChoice{
				{
					Index: 0,
					Message: chatMessage{
						Role:    string(core.MessageRoleAssistant),
						Content: "seen",
					},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer server.Close()

	p := NewProvider(
		WithAPIKey("sk-test"),
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	sess, err := p.NewSession(context.Background(), model.SessionOptions{Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	events, err := sess.StreamTurn(context.Background(), core.TurnInput{
		Messages: []core.Message{
			{
				Role:    core.MessageRoleUser,
				Content: "inspect this screenshot",
				Parts: []core.MessagePart{
					{
						Type:     core.MessagePartImage,
						MimeType: "image/png",
						URL:      "https://example.com/screenshot.png",
						Detail:   "low",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}
	_ = collectEvents(t, events)

	if len(captured.Messages) != 1 {
		t.Fatalf("request messages = %d, want 1", len(captured.Messages))
	}
	parts, ok := captured.Messages[0].Content.([]any)
	if !ok {
		t.Fatalf("content = %#v, want []any", captured.Messages[0].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("content parts = %#v, want 2", parts)
	}
	imagePart, ok := parts[1].(map[string]any)
	if !ok || imagePart["type"] != "image_url" {
		t.Fatalf("image part = %#v", parts[1])
	}
	imageURL, ok := imagePart["image_url"].(map[string]any)
	if !ok || imageURL["url"] != "https://example.com/screenshot.png" || imageURL["detail"] != "low" {
		t.Fatalf("image_url = %#v", imagePart["image_url"])
	}
}

func collectEvents(t *testing.T, ch <-chan core.TurnEvent) []core.TurnEvent {
	t.Helper()

	var events []core.TurnEvent
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, event)
		case <-timeout:
			t.Fatal("timed out waiting for turn events")
		}
	}
}
