package openaichat

import "encoding/json"

const (
	defaultAPIBaseURL      = "https://api.openai.com/v1"
	chatCompletionsPath    = "/chat/completions"
	defaultModelName       = "gpt-4o-mini"
	defaultToolChoiceValue = "auto"
)

type chatCompletionsRequest struct {
	Model             string            `json:"model"`
	Messages          []chatMessage     `json:"messages,omitempty"`
	Tools             []chatTool        `json:"tools,omitempty"`
	ToolChoice        any               `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool             `json:"parallel_tool_calls,omitempty"`
	Temperature       *float64          `json:"temperature,omitempty"`
	MaxTokens         *int              `json:"max_tokens,omitempty"`
	Seed              *int64            `json:"seed,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	Stream            bool              `json:"stream,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatToolCall struct {
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type,omitempty"`
	Function chatToolCallFunction `json:"function"`
}

type chatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatCompletionsResponse struct {
	ID      string                 `json:"id"`
	Choices []chatCompletionChoice `json:"choices,omitempty"`
	Error   *chatError             `json:"error,omitempty"`
}

type chatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type chatError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
}
