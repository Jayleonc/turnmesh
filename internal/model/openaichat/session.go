package openaichat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/model"
)

type requestError struct {
	StatusCode int
	Endpoint   string
	Body       string
}

func (e *requestError) Error() string {
	if e == nil {
		return ""
	}
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("openai chat completions status %d", e.StatusCode)
	}
	return fmt.Sprintf("openai chat completions status %d: %s", e.StatusCode, body)
}

type Session struct {
	id       string
	provider string
	model    string
	caps     model.Capabilities

	apiKey  string
	baseURL string
	client  *http.Client

	systemPrompt    string
	temperature     *float64
	maxOutputTokens *int
	tools           []core.ToolSpec
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
		return nil, errors.New("openai chat session: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	req := s.buildRequest(input)
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

		resp, err := s.doChatCompletion(ctx, req)
		if err != nil {
			wrapped := wrapRequestError(err).
				WithDetail("provider", s.provider).
				WithDetail("model", s.model)
			emit(core.TurnEvent{
				Kind:   core.TurnEventError,
				Status: core.TurnStatusFailed,
				Error:  wrapped,
			})
			return
		}
		if resp.Error != nil {
			message := strings.TrimSpace(resp.Error.Message)
			if message == "" {
				message = resp.Error.Code
			}
			emit(core.TurnEvent{
				Kind:   core.TurnEventError,
				Status: core.TurnStatusFailed,
				Error: core.NewError(core.ErrorCodeInternal, message).
					WithDetail("provider", s.provider).
					WithDetail("model", s.model).
					WithDetail("provider_error_code", resp.Error.Code).
					WithDetail("provider_error_type", resp.Error.Type),
				Metadata: map[string]string{
					"provider_error_code": resp.Error.Code,
					"provider_error_type": resp.Error.Type,
				},
			})
			return
		}
		if len(resp.Choices) == 0 {
			emit(core.TurnEvent{
				Kind:   core.TurnEventError,
				Status: core.TurnStatusFailed,
				Error:  core.NewError(core.ErrorCodeInternal, "openai chat completions returned empty choices"),
			})
			return
		}

		message := resp.Choices[0].Message
		hadToolCalls := false
		if text := messageText(message.Content); text != "" {
			if !emit(core.TurnEvent{
				Kind:   core.TurnEventMessage,
				Status: core.TurnStatusRunning,
				Message: &core.Message{
					Role:    core.MessageRoleAssistant,
					Content: text,
					Metadata: map[string]string{
						"provider": s.provider,
					},
				},
			}) {
				return
			}
		}

		for _, call := range message.ToolCalls {
			hadToolCalls = true
			if !emit(core.TurnEvent{
				Kind:   core.TurnEventToolCall,
				Status: core.TurnStatusWaiting,
				ToolCall: &core.ToolInvocation{
					ID:        call.ID,
					Tool:      call.Function.Name,
					Arguments: json.RawMessage(call.Function.Arguments),
				},
			}) {
				return
			}
		}

		status := core.TurnStatusCompleted
		if hadToolCalls {
			status = core.TurnStatusWaiting
		}
		emit(core.TurnEvent{
			Kind:   core.TurnEventCompleted,
			Status: status,
		})
	}()

	return events, nil
}

func (s *Session) buildRequest(input core.TurnInput) chatCompletionsRequest {
	req := chatCompletionsRequest{
		Model:    s.model,
		Metadata: cloneMetadata(input.Metadata),
	}
	if s.temperature != nil {
		value := *s.temperature
		req.Temperature = &value
	}
	if s.maxOutputTokens != nil {
		value := *s.maxOutputTokens
		req.MaxTokens = &value
	}

	systemPrompt := strings.TrimSpace(s.systemPrompt)
	if extra := strings.TrimSpace(input.SystemPrompt); extra != "" {
		if systemPrompt != "" {
			systemPrompt += "\n\n" + extra
		} else {
			systemPrompt = extra
		}
	}
	if systemPrompt != "" {
		req.Messages = append(req.Messages, chatMessage{
			Role:    string(core.MessageRoleSystem),
			Content: systemPrompt,
		})
	}
	req.Messages = append(req.Messages, buildChatMessages(input.Messages)...)
	req.Tools = buildTools(mergeTools(s.tools, input.Tools))
	if len(req.Tools) > 0 {
		req.ToolChoice = defaultToolChoiceValue
		parallel := true
		req.ParallelToolCalls = &parallel
	}
	return req
}

func (s *Session) doChatCompletion(ctx context.Context, req chatCompletionsRequest) (*chatCompletionsResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(s.baseURL, "/") + chatCompletionsPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

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
		return nil, &requestError{
			StatusCode: resp.StatusCode,
			Endpoint:   url,
			Body:       strings.TrimSpace(string(payload)),
		}
	}

	var out chatCompletionsResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func wrapRequestError(err error) *core.Error {
	code := core.ErrorCodeInternal
	var reqErr *requestError
	if errors.As(err, &reqErr) {
		switch reqErr.StatusCode {
		case http.StatusBadRequest:
			code = core.ErrorCodeValidation
		case http.StatusUnauthorized, http.StatusForbidden:
			code = core.ErrorCodeUnauthorized
		case http.StatusNotFound:
			code = core.ErrorCodeNotFound
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			code = core.ErrorCodeTimeout
		}
	}

	wrapped := core.WrapError(code, "openai chat completions request failed", err)
	if reqErr != nil {
		wrapped = wrapped.
			WithDetail("http_status", fmt.Sprintf("%d", reqErr.StatusCode)).
			WithDetail("endpoint", reqErr.Endpoint)
		if reqErr.Body != "" {
			wrapped = wrapped.WithDetail("response_body", truncateDetail(reqErr.Body, 512))
		}
	}
	return wrapped
}

func truncateDetail(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "...(truncated)"
}

func buildChatMessages(messages []core.Message) []chatMessage {
	out := make([]chatMessage, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case core.MessageRoleSystem, core.MessageRoleUser, core.MessageRoleAssistant:
			chatMsg, ok := buildStandardMessage(msg)
			if ok {
				out = append(out, chatMsg)
			}
		case core.MessageRoleTool:
			out = append(out, buildToolMessages(msg)...)
		}
	}
	return out
}

func buildStandardMessage(msg core.Message) (chatMessage, bool) {
	chatMsg := chatMessage{Role: string(msg.Role)}
	text := strings.TrimSpace(messageTextFromCoreMessage(msg))
	if text != "" {
		chatMsg.Content = text
	}
	for _, part := range msg.Parts {
		if part.ToolCall == nil {
			continue
		}
		chatMsg.ToolCalls = append(chatMsg.ToolCalls, chatToolCall{
			ID:   part.ToolCall.ID,
			Type: "function",
			Function: chatToolCallFunction{
				Name:      part.ToolCall.Tool,
				Arguments: string(argumentsForCall(*part.ToolCall)),
			},
		})
	}
	if text == "" && len(chatMsg.ToolCalls) == 0 {
		return chatMessage{}, false
	}
	return chatMsg, true
}

func buildToolMessages(msg core.Message) []chatMessage {
	toolMessages := make([]chatMessage, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		if part.ToolResult == nil {
			continue
		}
		result := part.ToolResult
		toolMessages = append(toolMessages, chatMessage{
			Role:       string(core.MessageRoleTool),
			Content:    toolResultOutput(result),
			ToolCallID: result.InvocationID,
		})
	}
	if len(toolMessages) > 0 {
		return toolMessages
	}
	if text := strings.TrimSpace(msg.Content); text != "" {
		return []chatMessage{{
			Role:    string(core.MessageRoleTool),
			Content: text,
		}}
	}
	return nil
}

func buildTools(tools []core.ToolSpec) []chatTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]chatTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  cloneRawMessage(tool.InputSchema),
			},
		})
	}
	return out
}

func mergeTools(base, extra []core.ToolSpec) []core.ToolSpec {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	byName := make(map[string]core.ToolSpec, len(base)+len(extra))
	order := make([]string, 0, len(base)+len(extra))
	add := func(tool core.ToolSpec) {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return
		}
		if _, exists := byName[name]; !exists {
			order = append(order, name)
		}
		byName[name] = tool
	}
	for _, tool := range base {
		add(tool)
	}
	for _, tool := range extra {
		add(tool)
	}
	out := make([]core.ToolSpec, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

func messageTextFromCoreMessage(msg core.Message) string {
	if strings.TrimSpace(msg.Content) != "" {
		return msg.Content
	}
	parts := make([]string, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		if part.Type == core.MessagePartText && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func messageText(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch obj["type"] {
			case "text", "output_text":
				if text, _ := obj["text"].(string); strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}

func argumentsForCall(call core.ToolInvocation) json.RawMessage {
	if len(call.Arguments) > 0 {
		return cloneRawMessage(call.Arguments)
	}
	if len(call.Input) > 0 {
		return cloneRawMessage(call.Input)
	}
	return json.RawMessage(`{}`)
}

func toolResultOutput(result *core.ToolResult) string {
	if result == nil {
		return ""
	}
	if strings.TrimSpace(result.Output) != "" {
		return result.Output
	}
	if len(result.Structured) > 0 {
		return string(result.Structured)
	}
	if result.Error != nil {
		return result.Error.Message
	}
	return ""
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func cloneMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
