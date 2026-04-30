package turnmesh

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunOneShotReturnsAssistantText(t *testing.T) {
	t.Parallel()

	var captured struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Tools       []json.RawMessage `json:"tools"`
		MaxTokens   *int              `json:"max_tokens"`
		Temperature *float64          `json:"temperature"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/chat/completions"; got != want {
			t.Fatalf("path = %s, want %s", got, want)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl_oneshot_1",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "rewritten query",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	temperature := 0.1
	maxTokens := 100
	result, err := RunOneShot(context.Background(), Config{
		Provider:        "openai-chatcompat",
		Model:           "gpt-4o-mini",
		BaseURL:         server.URL,
		APIKey:          "sk-test",
		Temperature:     &temperature,
		MaxOutputTokens: &maxTokens,
		Tools: []Tool{
			{
				Name:        "lookup",
				Description: "should be ignored in one-shot",
				InputSchema: MustJSONSchema(map[string]any{"type": "object"}),
				Handler: func(context.Context, ToolCall) (ToolOutcome, error) {
					return ToolOutcome{Output: "ignored", Status: ToolSucceeded}, nil
				},
			},
		},
		HTTPClient: server.Client(),
	}, OneShotRequest{
		SystemPrompt: "rewrite the query",
		Messages: []Message{
			{Role: RoleUser, Content: "这个怎么开"},
		},
	})
	if err != nil {
		t.Fatalf("RunOneShot() error = %v", err)
	}

	if result.Text != "rewritten query" {
		t.Fatalf("text = %q, want rewritten query", result.Text)
	}
	if result.Message == nil || result.Message.Role != RoleAssistant {
		t.Fatalf("message = %#v, want assistant message", result.Message)
	}
	if result.Status != TurnCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if captured.Model != "gpt-4o-mini" {
		t.Fatalf("model = %q, want gpt-4o-mini", captured.Model)
	}
	if len(captured.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(captured.Messages))
	}
	if captured.Messages[0].Role != "system" || captured.Messages[0].Content != "rewrite the query" {
		t.Fatalf("system message = %#v, want one-shot system prompt", captured.Messages[0])
	}
	if captured.Messages[1].Role != "user" || captured.Messages[1].Content != "这个怎么开" {
		t.Fatalf("user message = %#v, want original query", captured.Messages[1])
	}
	if len(captured.Tools) != 0 {
		t.Fatalf("tools = %#v, want none", captured.Tools)
	}
	if captured.MaxTokens == nil || *captured.MaxTokens != 100 {
		t.Fatalf("max_tokens = %#v, want 100", captured.MaxTokens)
	}
	if captured.Temperature == nil || *captured.Temperature != 0.1 {
		t.Fatalf("temperature = %#v, want 0.1", captured.Temperature)
	}
}

func TestRunOneShotSendsMultimodalParts(t *testing.T) {
	t.Parallel()

	var captured struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role":    "assistant",
						"content": "seen",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	result, err := RunOneShot(context.Background(), Config{
		Provider:   "openai-chatcompat",
		Model:      "gpt-4o-mini",
		BaseURL:    server.URL,
		APIKey:     "sk-test",
		HTTPClient: server.Client(),
	}, OneShotRequest{
		Messages: []Message{
			UserParts(
				TextPart("inspect this screenshot"),
				ImageBytesPart("", pngHeader, WithPartDetail("low"), WithPartSourcePath("/tmp/screenshot.png")),
			),
		},
	})
	if err != nil {
		t.Fatalf("RunOneShot() error = %v", err)
	}
	if result.Text != "seen" {
		t.Fatalf("text = %q, want seen", result.Text)
	}
	if len(captured.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(captured.Messages))
	}

	var content []struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		ImageURL *struct {
			URL    string `json:"url"`
			Detail string `json:"detail,omitempty"`
		} `json:"image_url,omitempty"`
	}
	if err := json.Unmarshal(captured.Messages[0].Content, &content); err != nil {
		t.Fatalf("decode content parts: %v; raw=%s", err, string(captured.Messages[0].Content))
	}
	if len(content) != 2 {
		t.Fatalf("content parts = %#v, want text + image", content)
	}
	if content[0].Type != "text" || content[0].Text != "inspect this screenshot" {
		t.Fatalf("text part = %#v", content[0])
	}
	if content[1].Type != "image_url" || content[1].ImageURL == nil {
		t.Fatalf("image part = %#v", content[1])
	}
	if !strings.HasPrefix(content[1].ImageURL.URL, "data:image/png;base64,") {
		t.Fatalf("image url = %q, want data:image/png", content[1].ImageURL.URL)
	}
	if content[1].ImageURL.Detail != "low" {
		t.Fatalf("detail = %q, want low", content[1].ImageURL.Detail)
	}
}

func TestMessagePartConstructorsCloneAndNormalize(t *testing.T) {
	t.Parallel()

	data := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	part := ImageBytesPart("", data, WithPartName("shot"), WithPartMetadata(map[string]string{"k": "v"}))
	data[0] = 0

	if part.Type != MessagePartImage {
		t.Fatalf("type = %q, want image", part.Type)
	}
	if part.MIMEType != "image/png" {
		t.Fatalf("mime = %q, want image/png", part.MIMEType)
	}
	if part.Data[0] != 0x89 {
		t.Fatalf("part data was not cloned")
	}
	if !part.IsImage() || !part.HasInlineData() || !part.HasMedia() {
		t.Fatalf("image helpers returned false for %#v", part)
	}
	part.Metadata["k"] = "changed"

	message := UserParts(part)
	part.Data[1] = 0
	part.Metadata["k"] = "changed-again"
	if message.Parts[0].Data[1] != 0x50 {
		t.Fatalf("message part data was not cloned")
	}
	if message.Parts[0].Metadata["k"] != "changed" {
		t.Fatalf("message metadata = %q, want changed", message.Parts[0].Metadata["k"])
	}
}

func TestRunOneShotRejectsInvalidMediaParts(t *testing.T) {
	t.Parallel()

	_, err := RunOneShot(context.Background(), Config{
		Provider:      "openai-chatcompat",
		Model:         "gpt-4o-mini",
		MaxMediaBytes: 3,
	}, OneShotRequest{
		Messages: []Message{
			UserParts(ImageBytesPart("image/png", []byte("1234"))),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "max_media_bytes") {
		t.Fatalf("err = %v, want max_media_bytes error", err)
	}

	_, err = RunOneShot(context.Background(), Config{
		Provider: "openai-chatcompat",
		Model:    "gpt-4o-mini",
	}, OneShotRequest{
		Messages: []Message{
			UserParts(MessagePart{
				Type:     MessagePartImage,
				MIMEType: "image/png",
				Data:     []byte("x"),
				URL:      "https://example.com/image.png",
			}),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "either url or data") {
		t.Fatalf("err = %v, want ambiguous source error", err)
	}
}

func TestRunOneShotRejectsToolCalls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl_oneshot_2",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      "lookup",
									"arguments": `{"query":"alpha"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		})
	}))
	defer server.Close()

	_, err := RunOneShot(context.Background(), Config{
		Provider:   "openai-chatcompat",
		Model:      "gpt-4o-mini",
		BaseURL:    server.URL,
		APIKey:     "sk-test",
		HTTPClient: server.Client(),
	}, OneShotRequest{
		Messages: []Message{
			{Role: RoleUser, Content: "say hello"},
		},
	})
	if err == nil {
		t.Fatal("RunOneShot() error = nil, want tool call error")
	}
	if !strings.Contains(err.Error(), "use RunTurn") {
		t.Fatalf("error = %q, want use RunTurn", err.Error())
	}
}

func TestRunOneShotExposesProviderCauseAndDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	_, err := RunOneShot(context.Background(), Config{
		Provider:   "openai-chatcompat",
		Model:      "gpt-4o-mini",
		BaseURL:    server.URL,
		APIKey:     "sk-test",
		HTTPClient: server.Client(),
	}, OneShotRequest{
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
		},
	})
	if err == nil {
		t.Fatal("RunOneShot() error = nil, want provider error")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("error = %q, want status 401 cause", err.Error())
	}

	tmErr, ok := AsError(err)
	if !ok {
		t.Fatalf("AsError() = false, want true; err=%T", err)
	}
	if tmErr.Code != "unauthorized" {
		t.Fatalf("code = %q, want unauthorized", tmErr.Code)
	}
	if tmErr.Details["http_status"] != "401" {
		t.Fatalf("details = %#v, want http_status=401", tmErr.Details)
	}
	if !strings.Contains(tmErr.Cause, "invalid api key") {
		t.Fatalf("cause = %q, want invalid api key", tmErr.Cause)
	}

	wrapped := errors.Join(errors.New("outer"), err)
	if _, ok := AsError(wrapped); !ok {
		t.Fatal("AsError() on wrapped error = false, want true")
	}
}

func TestEmitEventFromContextForwardsGenericEvents(t *testing.T) {
	t.Parallel()

	var events []Event
	ctx := WithEventEmitter(context.Background(), func(event Event) bool {
		events = append(events, event)
		return true
	})

	if ok := EmitEvent(ctx, Event{
		Kind:    EventCitation,
		Status:  TurnRunning,
		Payload: json.RawMessage(`{"source":"doc-1","text":"alpha"}`),
	}); !ok {
		t.Fatal("EmitEvent() for citation returned false")
	}
	if ok := EmitEvent(ctx, Event{
		Kind:    EventClarification,
		Status:  TurnWaiting,
		Payload: json.RawMessage(`{"question":"need more context"}`),
	}); !ok {
		t.Fatal("EmitEvent() for clarification returned false")
	}

	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Kind != EventCitation || string(events[0].Payload) != `{"source":"doc-1","text":"alpha"}` {
		t.Fatalf("citation event = %#v, want citation payload", events[0])
	}
	if events[1].Kind != EventClarification || string(events[1].Payload) != `{"question":"need more context"}` {
		t.Fatalf("clarification event = %#v, want clarification payload", events[1])
	}
}
