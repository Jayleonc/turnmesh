package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/model"
)

type sessionConfig struct {
	id           string
	provider     string
	model        string
	apiKey       string
	baseURL      string
	client       *http.Client
	systemPrompt string
	temperature  *float64
	maxTokens    int
	tools        []core.ToolSpec
}

type Session struct {
	mu sync.Mutex

	id       string
	provider string
	model    string
	caps     model.Capabilities

	apiKey       string
	baseURL      string
	client       *http.Client
	systemPrompt string
	temperature  *float64
	maxTokens    int
	tools        []core.ToolSpec

	history []messageItem
}

func newSession(cfg sessionConfig) *Session {
	return &Session{
		id:       cfg.id,
		provider: cfg.provider,
		model:    cfg.model,
		caps: model.Capabilities{
			CanStream:           true,
			CanToolCall:         true,
			CanParallelToolUse:  true,
			CanStructuredOutput: true,
			CanImageInput:       true,
			CanThinking:         true,
			CanSystemPrompt:     true,
		},
		apiKey:       cfg.apiKey,
		baseURL:      cfg.baseURL,
		client:       cfg.client,
		systemPrompt: cfg.systemPrompt,
		temperature:  cfg.temperature,
		maxTokens:    cfg.maxTokens,
		tools:        append([]core.ToolSpec(nil), cfg.tools...),
	}
}

func (s *Session) ID() string {
	return s.id
}

func (s *Session) Provider() string {
	return s.provider
}

func (s *Session) Model() string {
	return s.model
}

func (s *Session) Capabilities() model.Capabilities {
	return s.caps
}

func (s *Session) Close() error {
	return nil
}

func (s *Session) StreamTurn(ctx context.Context, input core.TurnInput) (<-chan core.TurnEvent, error) {
	if ctx == nil {
		return nil, errors.New("anthropic session: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	req, newHistory := s.buildRequest(input)
	events := make(chan core.TurnEvent, 8)

	go func() {
		defer close(events)

		emit := func(event core.TurnEvent) bool {
			if event.Timestamp.IsZero() {
				event.Timestamp = time.Now().UTC()
			}
			select {
			case events <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}

		if !emit(core.TurnEvent{
			Kind:   core.TurnEventStarted,
			Status: core.TurnStatusRunning,
			Metadata: map[string]string{
				"provider": s.provider,
				"model":    s.model,
			},
		}) {
			return
		}

		resp, err := s.doRequest(ctx, req)
		if err != nil {
			emit(core.TurnEvent{
				Kind:   core.TurnEventError,
				Status: core.TurnStatusFailed,
				Error:  core.WrapError(core.ErrorCodeInternal, "anthropic messages request failed", err),
			})
			return
		}

		if resp.Error != nil {
			emit(core.TurnEvent{
				Kind:   core.TurnEventError,
				Status: core.TurnStatusFailed,
				Error:  core.WrapError(core.ErrorCodeInternal, "anthropic provider error", errors.New(resp.Error.Message)),
				Metadata: map[string]string{
					"provider_error_type": resp.Error.Type,
				},
			})
			return
		}

		assistantMessage := messageItem{Role: "assistant"}
		toolUseSeen := false
		textBuilder := strings.Builder{}

		flushText := func() bool {
			text := strings.TrimSpace(textBuilder.String())
			if text == "" {
				textBuilder.Reset()
				return true
			}
			assistantMessage.Content = append(assistantMessage.Content, contentBlock{
				Type: "text",
				Text: text,
			})
			ok := emit(core.TurnEvent{
				Kind:   core.TurnEventMessage,
				Status: core.TurnStatusRunning,
				Message: &core.Message{
					Role:    core.MessageRoleAssistant,
					Content: text,
					Metadata: map[string]string{
						"provider":    s.provider,
						"model":       s.model,
						"response_id": resp.ID,
					},
				},
				Metadata: map[string]string{
					"response_id": resp.ID,
				},
			})
			textBuilder.Reset()
			return ok
		}

		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				if block.Text == "" {
					continue
				}
				if textBuilder.Len() > 0 {
					textBuilder.WriteString("\n")
				}
				textBuilder.WriteString(block.Text)
			case "tool_use":
				if !flushText() {
					return
				}
				toolUseSeen = true
				assistantMessage.Content = append(assistantMessage.Content, contentBlock{
					Type:  "tool_use",
					ID:    block.ID,
					Name:  block.Name,
					Input: cloneRawMessage(block.Input),
				})
				if !emit(core.TurnEvent{
					Kind:   core.TurnEventToolCall,
					Status: core.TurnStatusWaiting,
					ToolCall: &core.ToolInvocation{
						ID:        block.ID,
						Tool:      block.Name,
						Arguments: cloneRawMessage(block.Input),
						Metadata: map[string]string{
							"provider":    s.provider,
							"model":       s.model,
							"response_id": resp.ID,
						},
					},
					Metadata: map[string]string{
						"response_id": resp.ID,
					},
				}) {
					return
				}
			}
		}

		if !flushText() {
			return
		}

		s.mu.Lock()
		s.history = append(s.history, newHistory...)
		if len(assistantMessage.Content) > 0 {
			s.history = append(s.history, assistantMessage)
		}
		s.mu.Unlock()

		if toolUseSeen || resp.StopReason == "tool_use" {
			emit(core.TurnEvent{
				Kind:   core.TurnEventCompleted,
				Status: core.TurnStatusWaiting,
				Metadata: map[string]string{
					"response_id": resp.ID,
					"stop_reason": resp.StopReason,
				},
			})
			return
		}

		emit(core.TurnEvent{
			Kind:   core.TurnEventCompleted,
			Status: core.TurnStatusCompleted,
			Metadata: map[string]string{
				"response_id": resp.ID,
				"stop_reason": resp.StopReason,
			},
		})
	}()

	return events, nil
}

func (s *Session) buildRequest(input core.TurnInput) (messagesCreateRequest, []messageItem) {
	s.mu.Lock()
	history := append([]messageItem(nil), s.history...)
	s.mu.Unlock()

	req := messagesCreateRequest{
		Model:     s.model,
		MaxTokens: s.maxTokens,
		Tools:     buildTools(s.tools),
		Messages:  append([]messageItem(nil), history...),
	}
	if s.temperature != nil {
		req.Temperature = s.temperature
	}
	req.System = joinSystemPrompts(s.systemPrompt, input.SystemPrompt, input.Messages)

	newMessages := make([]messageItem, 0, len(input.Messages))
	for _, msg := range input.Messages {
		mapped, ok := buildMessage(msg)
		if !ok {
			continue
		}
		newMessages = append(newMessages, mapped)
		req.Messages = append(req.Messages, mapped)
	}
	req.Metadata = cloneStringMap(input.Metadata)
	return req, newMessages
}

func (s *Session) doRequest(ctx context.Context, req messagesCreateRequest) (*messagesCreateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(s.baseURL, "/") + messagesCreatePath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-api-key", s.apiKey)
	httpReq.Header.Set("anthropic-version", defaultAPIVersion)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic messages status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var out messagesCreateResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func buildTools(tools []core.ToolSpec) []toolDefinition {
	if len(tools) == 0 {
		return nil
	}
	out := make([]toolDefinition, 0, len(tools))
	for _, tool := range tools {
		schema := tool.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":true}`)
		}
		out = append(out, toolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: cloneRawMessage(schema),
		})
	}
	return out
}

func buildMessage(msg core.Message) (messageItem, bool) {
	switch msg.Role {
	case core.MessageRoleSystem:
		return messageItem{}, false
	case core.MessageRoleAssistant:
		return buildAssistantMessage(msg)
	case core.MessageRoleTool:
		return buildToolResultMessage(msg)
	default:
		return buildUserMessage(msg)
	}
}

func buildUserMessage(msg core.Message) (messageItem, bool) {
	blocks := make([]contentBlock, 0, len(msg.Parts)+1)
	if text := strings.TrimSpace(msg.Content); text != "" {
		blocks = append(blocks, contentBlock{Type: "text", Text: text})
	}
	for _, part := range msg.Parts {
		if part.Type == core.MessagePartText && part.Text != "" {
			blocks = append(blocks, contentBlock{Type: "text", Text: part.Text})
		}
	}
	if len(blocks) == 0 {
		return messageItem{}, false
	}
	return messageItem{Role: "user", Content: blocks}, true
}

func buildAssistantMessage(msg core.Message) (messageItem, bool) {
	blocks := make([]contentBlock, 0, len(msg.Parts)+1)
	if text := strings.TrimSpace(msg.Content); text != "" {
		blocks = append(blocks, contentBlock{Type: "text", Text: text})
	}
	for _, part := range msg.Parts {
		if part.ToolCall != nil {
			blocks = append(blocks, contentBlock{
				Type:  "tool_use",
				ID:    part.ToolCall.ID,
				Name:  part.ToolCall.Tool,
				Input: cloneRawMessage(part.ToolCall.Input),
			})
		}
		if part.Type == core.MessagePartText && part.Text != "" {
			blocks = append(blocks, contentBlock{Type: "text", Text: part.Text})
		}
	}
	if len(blocks) == 0 {
		return messageItem{}, false
	}
	return messageItem{Role: "assistant", Content: blocks}, true
}

func buildToolResultMessage(msg core.Message) (messageItem, bool) {
	resultBlocks := make([]contentBlock, 0, len(msg.Parts)+1)
	textBlocks := make([]contentBlock, 0, 1)
	for _, part := range msg.Parts {
		if part.ToolResult != nil {
			resultBlocks = append(resultBlocks, toolResultBlock(*part.ToolResult, msg.ID))
		}
		if part.Type == core.MessagePartText && part.Text != "" {
			textBlocks = append(textBlocks, contentBlock{Type: "text", Text: part.Text})
		}
	}
	if text := strings.TrimSpace(msg.Content); text != "" {
		textBlocks = append(textBlocks, contentBlock{Type: "text", Text: text})
	}
	if len(resultBlocks) == 0 && len(textBlocks) == 0 {
		return messageItem{}, false
	}
	blocks := append(resultBlocks, textBlocks...)
	return messageItem{Role: "user", Content: blocks}, true
}

func toolResultBlock(result core.ToolResult, fallbackID string) contentBlock {
	content := result.Output
	if content == "" && len(result.Structured) > 0 {
		content = string(result.Structured)
	}
	if content == "" && result.Error != nil {
		content = result.Error.Message
	}
	return contentBlock{
		Type:      "tool_result",
		ToolUseID: firstNonEmpty(result.InvocationID, fallbackID),
		Content:   content,
		IsError:   result.Status == core.ToolStatusFailed || result.Error != nil,
	}
}

func joinSystemPrompts(base string, extra string, messages []core.Message) string {
	parts := make([]string, 0, 3)
	if trimmed := strings.TrimSpace(base); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if trimmed := strings.TrimSpace(extra); trimmed != "" {
		parts = append(parts, trimmed)
	}
	for _, msg := range messages {
		if msg.Role != core.MessageRoleSystem {
			continue
		}
		if trimmed := strings.TrimSpace(messageText(msg)); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, "\n\n")
}

func messageText(msg core.Message) string {
	if trimmed := strings.TrimSpace(msg.Content); trimmed != "" {
		return trimmed
	}
	var b strings.Builder
	for _, part := range msg.Parts {
		if part.Type == core.MessagePartText && part.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(part.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func cloneRawMessage(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), in...)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
