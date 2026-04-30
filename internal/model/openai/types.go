package openai

import "encoding/json"

const (
	defaultAPIBaseURL   = "https://api.openai.com/v1"
	responsesCreatePath = "/responses"
	defaultModelName    = "gpt-4o-mini"
)

type responsesCreateRequest struct {
	Model              string               `json:"model"`
	Input              []responsesInputItem `json:"input,omitempty"`
	Instructions       string               `json:"instructions,omitempty"`
	PreviousResponseID string               `json:"previous_response_id,omitempty"`
	Tools              []responsesTool      `json:"tools,omitempty"`
	ParallelToolCalls  *bool                `json:"parallel_tool_calls,omitempty"`
	Temperature        *float64             `json:"temperature,omitempty"`
	MaxOutputTokens    *int                 `json:"max_output_tokens,omitempty"`
	Seed               *int64               `json:"seed,omitempty"`
	Metadata           map[string]string    `json:"metadata,omitempty"`
}

type responsesInputItem struct {
	Type    string                  `json:"type"`
	Role    string                  `json:"role,omitempty"`
	Content []responsesInputContent `json:"content,omitempty"`
	CallID  string                  `json:"call_id,omitempty"`
	Output  string                  `json:"output,omitempty"`
}

type responsesInputContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type responsesCreateResponse struct {
	ID     string            `json:"id"`
	Output []json.RawMessage `json:"output,omitempty"`
	Error  *responsesError   `json:"error,omitempty"`
}

type responsesError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
}

type responseOutputHeader struct {
	Type string `json:"type"`
}

type responseMessageItem struct {
	Type    string                `json:"type"`
	ID      string                `json:"id,omitempty"`
	Role    string                `json:"role,omitempty"`
	Content []responseMessagePart `json:"content,omitempty"`
	Status  string                `json:"status,omitempty"`
}

type responseMessagePart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type responseFunctionCallItem struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Status    string `json:"status,omitempty"`
}
