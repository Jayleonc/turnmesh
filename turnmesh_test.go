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
