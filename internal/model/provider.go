package model

import (
	"context"

	"github.com/Jayleonc/turnmesh/internal/core"
)

type Provider interface {
	Name() string
	ListModels(ctx context.Context) ([]ModelInfo, error)
	NewSession(ctx context.Context, opts SessionOptions) (Session, error)
}

type ModelInfo struct {
	Name         string            `json:"name"`
	DisplayName  string            `json:"display_name,omitempty"`
	Capabilities Capabilities      `json:"capabilities"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Capabilities struct {
	CanStream           bool `json:"can_stream,omitempty"`
	CanToolCall         bool `json:"can_tool_call,omitempty"`
	CanParallelToolUse  bool `json:"can_parallel_tool_use,omitempty"`
	CanStructuredOutput bool `json:"can_structured_output,omitempty"`
	CanImageInput       bool `json:"can_image_input,omitempty"`
	CanAudioInput       bool `json:"can_audio_input,omitempty"`
	CanThinking         bool `json:"can_thinking,omitempty"`
	CanSystemPrompt     bool `json:"can_system_prompt,omitempty"`
}

type SessionOptions struct {
	Model           string            `json:"model,omitempty"`
	SystemPrompt    string            `json:"system_prompt,omitempty"`
	Temperature     *float64          `json:"temperature,omitempty"`
	MaxOutputTokens *int              `json:"max_output_tokens,omitempty"`
	Seed            *int64            `json:"seed,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	Tools           []core.ToolSpec   `json:"tools,omitempty"`
}
