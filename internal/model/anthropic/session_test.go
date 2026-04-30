package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/model"
)

func TestProviderRequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	p := NewProvider(WithAPIKey(""))
	_, err := p.NewSession(context.Background(), model.SessionOptions{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestSessionStreamsTextAndToolCall(t *testing.T) {
	var gotRequest messagesCreateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != messagesCreatePath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("unexpected api key header: %s", got)
		}
		if got := r.Header.Get("anthropic-version"); got != defaultAPIVersion {
			t.Fatalf("unexpected version header: %s", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(messagesCreateResponse{
			ID:    "msg_1",
			Type:  "message",
			Role:  "assistant",
			Model: defaultModelName,
			Content: []contentBlock{
				{Type: "text", Text: "hello"},
			},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	sess := newSession(sessionConfig{
		id:        "anthropic-test",
		provider:  "anthropic",
		model:     defaultModelName,
		apiKey:    "test-key",
		baseURL:   server.URL,
		client:    server.Client(),
		maxTokens: 128,
		tools: []core.ToolSpec{
			{Name: "shell", Description: "run a command"},
		},
	})

	events, err := sess.StreamTurn(context.Background(), core.TurnInput{
		Messages: []core.Message{
			{Role: core.MessageRoleUser, Content: "hi"},
		},
		Tools: []core.ToolSpec{
			{Name: "shell", Description: "run a command"},
		},
	})
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}

	got := drainEvents(events)
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[1].Kind != core.TurnEventMessage || got[1].Message == nil || got[1].Message.Content != "hello" {
		t.Fatalf("unexpected message event: %#v", got[1])
	}
	if got[2].Kind != core.TurnEventCompleted || got[2].Status != core.TurnStatusCompleted {
		t.Fatalf("unexpected completed event: %#v", got[2])
	}
	if gotRequest.Model != defaultModelName {
		t.Fatalf("unexpected model: %s", gotRequest.Model)
	}
	if len(gotRequest.Messages) != 1 || gotRequest.Messages[0].Role != "user" {
		t.Fatalf("unexpected request messages: %#v", gotRequest.Messages)
	}
	if len(gotRequest.Tools) != 1 || gotRequest.Tools[0].Name != "shell" {
		t.Fatalf("unexpected tools: %#v", gotRequest.Tools)
	}
}

func TestSessionBuildsImageBlock(t *testing.T) {
	var gotRequest messagesCreateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(messagesCreateResponse{
			ID:    "msg_image",
			Type:  "message",
			Role:  "assistant",
			Model: defaultModelName,
			Content: []contentBlock{
				{Type: "text", Text: "seen"},
			},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	sess := newSession(sessionConfig{
		id:        "anthropic-test",
		provider:  "anthropic",
		model:     defaultModelName,
		apiKey:    "test-key",
		baseURL:   server.URL,
		client:    server.Client(),
		maxTokens: 128,
	})

	events, err := sess.StreamTurn(context.Background(), core.TurnInput{
		Messages: []core.Message{
			{
				Role:    core.MessageRoleUser,
				Content: "inspect this screenshot",
				Parts: []core.MessagePart{
					{
						Type:     core.MessagePartImage,
						MimeType: "image/png",
						Data:     []byte("png-bytes"),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("StreamTurn: %v", err)
	}
	_ = drainEvents(events)

	if len(gotRequest.Messages) != 1 || len(gotRequest.Messages[0].Content) != 2 {
		t.Fatalf("unexpected request messages: %#v", gotRequest.Messages)
	}
	image := gotRequest.Messages[0].Content[1]
	if image.Type != "image" || image.Source == nil {
		t.Fatalf("image block = %#v", image)
	}
	if image.Source.Type != "base64" || image.Source.MediaType != "image/png" || image.Source.Data == "" {
		t.Fatalf("image source = %#v", image.Source)
	}
}

func TestSessionContinuesAfterToolResult(t *testing.T) {
	var requestCount atomic.Int32
	var secondRequest messagesCreateRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestCount.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(messagesCreateResponse{
				ID:    "msg_1",
				Type:  "message",
				Role:  "assistant",
				Model: defaultModelName,
				Content: []contentBlock{
					{Type: "tool_use", ID: "toolu_1", Name: "shell", Input: json.RawMessage(`{"command":"pwd"}`)},
				},
				StopReason: "tool_use",
			})
			return
		}

		if err := json.NewDecoder(r.Body).Decode(&secondRequest); err != nil {
			t.Fatalf("decode second request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(messagesCreateResponse{
			ID:    "msg_2",
			Type:  "message",
			Role:  "assistant",
			Model: defaultModelName,
			Content: []contentBlock{
				{Type: "text", Text: "done"},
			},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	sess := newSession(sessionConfig{
		id:        "anthropic-test",
		provider:  "anthropic",
		model:     defaultModelName,
		apiKey:    "test-key",
		baseURL:   server.URL,
		client:    server.Client(),
		maxTokens: 128,
		tools: []core.ToolSpec{
			{Name: "shell"},
		},
	})

	firstEvents, err := sess.StreamTurn(context.Background(), core.TurnInput{
		Messages: []core.Message{{Role: core.MessageRoleUser, Content: "run pwd"}},
	})
	if err != nil {
		t.Fatalf("first StreamTurn: %v", err)
	}
	first := drainEvents(firstEvents)
	if len(first) != 3 {
		t.Fatalf("expected 3 events, got %d", len(first))
	}
	if first[1].Kind != core.TurnEventToolCall || first[1].ToolCall == nil || first[1].ToolCall.Tool != "shell" {
		t.Fatalf("unexpected tool call: %#v", first[1])
	}
	if first[2].Kind != core.TurnEventCompleted || first[2].Status != core.TurnStatusWaiting {
		t.Fatalf("unexpected waiting completed event: %#v", first[2])
	}

	secondEvents, err := sess.StreamTurn(context.Background(), core.TurnInput{
		Messages: []core.Message{
			{
				Role: core.MessageRoleTool,
				Parts: []core.MessagePart{
					{
						Type: core.MessagePartToolResult,
						ToolResult: &core.ToolResult{
							InvocationID: "toolu_1",
							Tool:         "shell",
							Status:       core.ToolStatusSucceeded,
							Output:       "/tmp",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("second StreamTurn: %v", err)
	}
	second := drainEvents(secondEvents)
	if len(second) != 3 {
		t.Fatalf("expected 3 events, got %d", len(second))
	}
	if second[1].Kind != core.TurnEventMessage || second[1].Message == nil || second[1].Message.Content != "done" {
		t.Fatalf("unexpected second message: %#v", second[1])
	}
	if secondRequest.Model != defaultModelName {
		t.Fatalf("unexpected second request model: %s", secondRequest.Model)
	}
	if len(secondRequest.Messages) != 3 {
		t.Fatalf("expected 3 messages in history, got %d", len(secondRequest.Messages))
	}
	if secondRequest.Messages[1].Role != "assistant" || len(secondRequest.Messages[1].Content) != 1 || secondRequest.Messages[1].Content[0].Type != "tool_use" {
		t.Fatalf("expected assistant tool_use in history, got %#v", secondRequest.Messages[1])
	}
	if secondRequest.Messages[2].Role != "user" || len(secondRequest.Messages[2].Content) != 1 || secondRequest.Messages[2].Content[0].Type != "tool_result" {
		t.Fatalf("expected tool_result in history, got %#v", secondRequest.Messages[2])
	}
	if secondRequest.Messages[2].Content[0].ToolUseID != "toolu_1" {
		t.Fatalf("unexpected tool_use_id: %s", secondRequest.Messages[2].Content[0].ToolUseID)
	}
}

func drainEvents(ch <-chan core.TurnEvent) []core.TurnEvent {
	var out []core.TurnEvent
	for event := range ch {
		out = append(out, event)
	}
	return out
}
