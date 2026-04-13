package anthropic

import "encoding/json"

const (
	defaultAPIBaseURL  = "https://api.anthropic.com/v1"
	messagesCreatePath = "/messages"
	defaultModelName   = "claude-sonnet-4-20250514"
	defaultAPIVersion  = "2023-06-01"
	defaultMaxTokens   = 1024
)

type messagesCreateRequest struct {
	Model       string            `json:"model"`
	System      string            `json:"system,omitempty"`
	Messages    []messageItem     `json:"messages,omitempty"`
	Tools       []toolDefinition  `json:"tools,omitempty"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature *float64          `json:"temperature,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type messageItem struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content,omitempty"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Content   any             `json:"content,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type toolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type messagesCreateResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Model      string         `json:"model"`
	Content    []contentBlock `json:"content,omitempty"`
	StopReason string         `json:"stop_reason,omitempty"`
	Usage      responseUsage  `json:"usage,omitempty"`
	Error      *responseError `json:"error,omitempty"`
}

type responseUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

type responseError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
}
