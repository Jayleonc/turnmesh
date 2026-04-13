package openai

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

type Session struct {
	mu sync.Mutex

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
	seed            *int64
	tools           []core.ToolSpec

	previousResponseID string
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
		return nil, errors.New("openai session: nil context")
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

		resp, err := s.doResponse(ctx, req)
		if err != nil {
			emit(core.TurnEvent{
				Kind:   core.TurnEventError,
				Status: core.TurnStatusFailed,
				Error:  core.WrapError(core.ErrorCodeInternal, "openai responses request failed", err),
			})
			return
		}

		s.mu.Lock()
		s.previousResponseID = resp.ID
		s.mu.Unlock()

		if resp.Error != nil {
			message := resp.Error.Message
			if message == "" {
				message = resp.Error.Code
			}
			emit(core.TurnEvent{
				Kind:   core.TurnEventError,
				Status: core.TurnStatusFailed,
				Error:  core.NewError(core.ErrorCodeInternal, message),
				Metadata: map[string]string{
					"provider_error_code": resp.Error.Code,
					"provider_error_type": resp.Error.Type,
				},
			})
			return
		}

		if len(resp.Output) == 0 {
			emit(core.TurnEvent{
				Kind:   core.TurnEventCompleted,
				Status: core.TurnStatusCompleted,
				Metadata: map[string]string{
					"response_id": resp.ID,
				},
			})
			return
		}

		hadMessage := false
		hadToolCall := false

		for _, raw := range resp.Output {
			header := responseOutputHeader{}
			if err := json.Unmarshal(raw, &header); err != nil {
				emit(core.TurnEvent{
					Kind:   core.TurnEventError,
					Status: core.TurnStatusFailed,
					Error:  core.WrapError(core.ErrorCodeInternal, "failed to decode openai response output", err),
				})
				return
			}

			switch header.Type {
			case "message":
				msg, err := decodeResponseMessage(raw)
				if err != nil {
					emit(core.TurnEvent{
						Kind:   core.TurnEventError,
						Status: core.TurnStatusFailed,
						Error:  core.WrapError(core.ErrorCodeInternal, "failed to decode response message", err),
					})
					return
				}
				text := messageText(msg)
				if text == "" {
					continue
				}
				hadMessage = true
				if !emit(core.TurnEvent{
					Kind:   core.TurnEventMessage,
					Status: core.TurnStatusRunning,
					Message: &core.Message{
						Role:    core.MessageRoleAssistant,
						Content: text,
						Metadata: map[string]string{
							"provider":    s.provider,
							"response_id": resp.ID,
						},
					},
					Metadata: map[string]string{
						"response_id": resp.ID,
					},
				}) {
					return
				}
			case "function_call":
				call, err := decodeResponseFunctionCall(raw)
				if err != nil {
					emit(core.TurnEvent{
						Kind:   core.TurnEventError,
						Status: core.TurnStatusFailed,
						Error:  core.WrapError(core.ErrorCodeInternal, "failed to decode function call", err),
					})
					return
				}
				hadToolCall = true
				if !emit(core.TurnEvent{
					Kind:   core.TurnEventToolCall,
					Status: core.TurnStatusWaiting,
					ToolCall: &core.ToolInvocation{
						ID:        call.CallID,
						Tool:      call.Name,
						Arguments: json.RawMessage(call.Arguments),
						Metadata: map[string]string{
							"openai_response_id": resp.ID,
							"openai_item_id":     call.ID,
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

		if hadToolCall && !hadMessage {
			emit(core.TurnEvent{
				Kind:   core.TurnEventCompleted,
				Status: core.TurnStatusWaiting,
				Metadata: map[string]string{
					"response_id": resp.ID,
				},
			})
			return
		}

		emit(core.TurnEvent{
			Kind:   core.TurnEventCompleted,
			Status: core.TurnStatusCompleted,
			Metadata: map[string]string{
				"response_id": resp.ID,
			},
		})
	}()

	return events, nil
}

func (s *Session) buildRequest(input core.TurnInput) responsesCreateRequest {
	s.mu.Lock()
	previousResponseID := s.previousResponseID
	s.mu.Unlock()

	req := responsesCreateRequest{
		Model:    s.model,
		Metadata: cloneStringMap(input.Metadata),
	}

	if previousResponseID != "" {
		req.PreviousResponseID = previousResponseID
	}
	if s.systemPrompt != "" {
		req.Instructions = s.systemPrompt
	}
	if input.SystemPrompt != "" {
		if req.Instructions != "" {
			req.Instructions = req.Instructions + "\n\n" + input.SystemPrompt
		} else {
			req.Instructions = input.SystemPrompt
		}
	}
	req.Input = buildInputItems(input.Messages)
	req.Tools = buildTools(s.tools)
	req.Temperature = s.temperature
	req.MaxOutputTokens = s.maxOutputTokens
	req.Seed = s.seed
	if len(req.Tools) > 0 {
		parallel := true
		req.ParallelToolCalls = &parallel
	}
	return req
}

func (s *Session) doResponse(ctx context.Context, req responsesCreateRequest) (*responsesCreateResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(s.baseURL, "/") + responsesCreatePath
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
		return nil, fmt.Errorf("openai responses status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var out responsesCreateResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func buildInputItems(messages []core.Message) []responsesInputItem {
	items := make([]responsesInputItem, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case core.MessageRoleSystem:
			continue
		case core.MessageRoleTool:
			if toolItems := buildFunctionCallOutputItems(msg); len(toolItems) > 0 {
				items = append(items, toolItems...)
			}
		default:
			content := buildInputContent(msg)
			if len(content) == 0 {
				continue
			}
			items = append(items, responsesInputItem{
				Type:    "message",
				Role:    string(msg.Role),
				Content: content,
			})
		}
	}
	return items
}

func buildFunctionCallOutputItems(msg core.Message) []responsesInputItem {
	items := make([]responsesInputItem, 0, len(msg.Parts)+1)
	for _, part := range msg.Parts {
		if part.ToolResult == nil {
			continue
		}
		items = append(items, responsesInputItem{
			Type:   "function_call_output",
			CallID: part.ToolResult.InvocationID,
			Output: toolResultOutput(part.ToolResult),
		})
	}
	if msg.Content != "" {
		items = append(items, responsesInputItem{
			Type:   "function_call_output",
			CallID: msg.ID,
			Output: msg.Content,
		})
	}
	return items
}

func toolResultOutput(result *core.ToolResult) string {
	if result == nil {
		return ""
	}
	if result.Output != "" {
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

func buildInputContent(msg core.Message) []responsesInputContent {
	content := make([]responsesInputContent, 0, len(msg.Parts)+1)
	if msg.Content != "" {
		content = append(content, responsesInputContent{Type: "input_text", Text: msg.Content})
	}
	for _, part := range msg.Parts {
		if part.Type == core.MessagePartText && part.Text != "" {
			content = append(content, responsesInputContent{Type: "input_text", Text: part.Text})
		}
	}
	return content
}

func buildTools(tools []core.ToolSpec) []responsesTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]responsesTool, 0, len(tools))
	for _, tool := range tools {
		schema := tool.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":true}`)
		}
		out = append(out, responsesTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  schema,
		})
	}
	return out
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

func decodeResponseMessage(raw json.RawMessage) (responseMessageItem, error) {
	var msg responseMessageItem
	if err := json.Unmarshal(raw, &msg); err != nil {
		return msg, err
	}
	return msg, nil
}

func decodeResponseFunctionCall(raw json.RawMessage) (responseFunctionCallItem, error) {
	var call responseFunctionCallItem
	if err := json.Unmarshal(raw, &call); err != nil {
		return call, err
	}
	return call, nil
}

func messageText(msg responseMessageItem) string {
	var b strings.Builder
	for _, part := range msg.Content {
		switch part.Type {
		case "output_text", "text":
			if part.Text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func newSessionID() string {
	return fmt.Sprintf("openai-%d", time.Now().UnixNano())
}
