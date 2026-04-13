package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/model"
)

func TestSessionStreamsAssistantText(t *testing.T) {
	t.Parallel()

	var captured responsesCreateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("method = %s, want %s", got, want)
		}
		if got, want := r.URL.Path, "/responses"; got != want {
			t.Fatalf("path = %s, want %s", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer sk-test"; got != want {
			t.Fatalf("authorization = %q, want %q", got, want)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if captured.PreviousResponseID != "" {
			t.Fatalf("previous_response_id = %q, want empty", captured.PreviousResponseID)
		}
		if captured.Model != "gpt-4o-mini" {
			t.Fatalf("model = %q, want gpt-4o-mini", captured.Model)
		}

		_ = json.NewEncoder(w).Encode(responsesCreateResponse{
			ID: "resp_1",
			Output: []json.RawMessage{
				mustJSON(t, responseMessageItem{
					Type: "message",
					Role: "assistant",
					Content: []responseMessagePart{
						{Type: "output_text", Text: "hello from openai"},
					},
				}),
			},
		})
	}))
	defer server.Close()

	p := NewProvider(
		WithAPIKey("sk-test"),
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	sess, err := p.NewSession(context.Background(), modelSessionOptions())
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	events, err := sess.StreamTurn(context.Background(), core.TurnInput{
		Messages: []core.Message{
			{Role: core.MessageRoleUser, Content: "say hello"},
		},
	})
	if err != nil {
		t.Fatalf("StreamTurn() error = %v", err)
	}

	got := collectEvents(t, events)
	if len(got) < 3 {
		t.Fatalf("events = %d, want at least 3", len(got))
	}
	if got[0].Kind != core.TurnEventStarted {
		t.Fatalf("first event kind = %s, want started", got[0].Kind)
	}
	if got[1].Kind != core.TurnEventMessage {
		t.Fatalf("second event kind = %s, want message", got[1].Kind)
	}
	if got[1].Message == nil || got[1].Message.Content != "hello from openai" {
		t.Fatalf("message = %#v, want assistant text", got[1].Message)
	}
	if got[len(got)-1].Kind != core.TurnEventCompleted {
		t.Fatalf("last event kind = %s, want completed", got[len(got)-1].Kind)
	}
}

func TestSessionPersistsPreviousResponseAndToolOutputs(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		calls []responsesCreateRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req responsesCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		calls = append(calls, req)
		callCount := len(calls)
		mu.Unlock()

		switch callCount {
		case 1:
			_ = json.NewEncoder(w).Encode(responsesCreateResponse{
				ID: "resp_1",
				Output: []json.RawMessage{
					mustJSON(t, responseFunctionCallItem{
						Type:      "function_call",
						ID:        "item_1",
						CallID:    "call_1",
						Name:      "lookup",
						Arguments: `{"query":"alpha"}`,
					}),
				},
			})
		case 2:
			_ = json.NewEncoder(w).Encode(responsesCreateResponse{
				ID: "resp_2",
				Output: []json.RawMessage{
					mustJSON(t, responseMessageItem{
						Type: "message",
						Role: "assistant",
						Content: []responseMessagePart{
							{Type: "output_text", Text: "tool result accepted"},
						},
					}),
				},
			})
		default:
			t.Fatalf("unexpected request count %d", callCount)
		}
	}))
	defer server.Close()

	p := NewProvider(
		WithAPIKey("sk-test"),
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	sess, err := p.NewSession(context.Background(), modelSessionOptions())
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	first, err := sess.StreamTurn(context.Background(), core.TurnInput{
		Messages: []core.Message{
			{Role: core.MessageRoleUser, Content: "use the tool"},
		},
	})
	if err != nil {
		t.Fatalf("first StreamTurn() error = %v", err)
	}
	firstEvents := collectEvents(t, first)
	if got := firstEvents[len(firstEvents)-1].Kind; got != core.TurnEventCompleted {
		t.Fatalf("first turn last event = %s, want completed", got)
	}
	if firstEvents[1].Kind != core.TurnEventToolCall {
		t.Fatalf("first turn second event = %s, want tool_call", firstEvents[1].Kind)
	}

	second, err := sess.StreamTurn(context.Background(), core.TurnInput{
		Messages: []core.Message{
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
		t.Fatalf("second StreamTurn() error = %v", err)
	}
	secondEvents := collectEvents(t, second)
	if got := secondEvents[1].Kind; got != core.TurnEventMessage {
		t.Fatalf("second turn second event = %s, want message", got)
	}
	if secondEvents[1].Message == nil || secondEvents[1].Message.Content != "tool result accepted" {
		t.Fatalf("second turn message = %#v, want assistant text", secondEvents[1].Message)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("request count = %d, want 2", len(calls))
	}
	if calls[1].PreviousResponseID != "resp_1" {
		t.Fatalf("previous_response_id = %q, want resp_1", calls[1].PreviousResponseID)
	}
	if len(calls[1].Input) != 1 {
		t.Fatalf("second request input items = %d, want 1", len(calls[1].Input))
	}
	if calls[1].Input[0].Type != "function_call_output" {
		t.Fatalf("second request input type = %s, want function_call_output", calls[1].Input[0].Type)
	}
	if calls[1].Input[0].CallID != "call_1" {
		t.Fatalf("second request call_id = %q, want call_1", calls[1].Input[0].CallID)
	}
	if calls[1].Input[0].Output != "alpha-result" {
		t.Fatalf("second request output = %q, want alpha-result", calls[1].Input[0].Output)
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

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func modelSessionOptions() model.SessionOptions {
	return model.SessionOptions{
		Model: "gpt-4o-mini",
	}
}
