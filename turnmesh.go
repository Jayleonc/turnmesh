package turnmesh

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/executor"
	"github.com/Jayleonc/turnmesh/internal/model"
	"github.com/Jayleonc/turnmesh/internal/model/anthropic"
	"github.com/Jayleonc/turnmesh/internal/model/openai"
	"github.com/Jayleonc/turnmesh/internal/model/openaichat"
	"github.com/Jayleonc/turnmesh/internal/orchestrator"
)

type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

type TurnStatus string

const (
	TurnPending     TurnStatus = "pending"
	TurnRunning     TurnStatus = "running"
	TurnWaiting     TurnStatus = "waiting"
	TurnCompleted   TurnStatus = "completed"
	TurnFailed      TurnStatus = "failed"
	TurnCancelled   TurnStatus = "cancelled"
	TurnInterrupted TurnStatus = "interrupted"
)

type ToolStatus string

const (
	ToolQueued    ToolStatus = "queued"
	ToolRunning   ToolStatus = "running"
	ToolSucceeded ToolStatus = "succeeded"
	ToolFailed    ToolStatus = "failed"
	ToolCancelled ToolStatus = "cancelled"
	ToolSkipped   ToolStatus = "skipped"
)

type EventKind string

const (
	EventStarted    EventKind = "started"
	EventMessage    EventKind = "message"
	EventToolCall   EventKind = "tool_call"
	EventToolResult EventKind = "tool_result"
	EventCompleted  EventKind = "completed"
	EventError      EventKind = "error"
)

type Error struct {
	Code    string
	Message string
	Cause   string
	Details map[string]string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" && e.Cause != "" {
		return e.Message + ": " + e.Cause
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != "" {
		return e.Cause
	}
	return e.Code
}

func AsError(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

type Message struct {
	Role     MessageRole
	Content  string
	Metadata map[string]string
}

type Tool struct {
	Name            string
	Description     string
	InputSchema     any
	OutputSchema    any
	ReadOnly        bool
	ConcurrencySafe bool
	Timeout         time.Duration
	Handler         ToolHandler
	Metadata        map[string]string
}

type ToolCall struct {
	ID         string
	Name       string
	Input      json.RawMessage
	Arguments  json.RawMessage
	Caller     string
	ApprovalID string
	Metadata   map[string]string
}

type ToolResult struct {
	InvocationID string
	Tool         string
	Status       ToolStatus
	Output       string
	Structured   json.RawMessage
	Error        *Error
	Duration     time.Duration
	Metadata     map[string]string
}

type Event struct {
	Kind       EventKind
	Status     TurnStatus
	Message    *Message
	ToolCall   *ToolCall
	ToolResult *ToolResult
	Error      *Error
	Metadata   map[string]string
}

type ToolOutcome struct {
	Output     string
	Structured json.RawMessage
	Metadata   map[string]string
	Duration   time.Duration
	Status     ToolStatus
	Error      *Error
}

type ToolHandler func(context.Context, ToolCall) (ToolOutcome, error)

type Config struct {
	Provider        string
	Model           string
	BaseURL         string
	APIKey          string
	Temperature     *float64
	MaxOutputTokens *int
	HTTPClient      *http.Client
	Tools           []Tool
}

type TurnRequest struct {
	SessionID    string
	TurnID       string
	SystemPrompt string
	Messages     []Message
	Metadata     map[string]string
}

type TurnResult struct {
	Text        string
	Status      TurnStatus
	Messages    []Message
	ToolResults []ToolResult
	Events      []Event
}

type OneShotRequest struct {
	SystemPrompt string
	Messages     []Message
	Metadata     map[string]string
}

type OneShotResult struct {
	Text    string
	Status  TurnStatus
	Message *Message
	Events  []Event
}

type Runtime struct {
	engine  *orchestrator.Engine
	session model.Session
}

func New(ctx context.Context, cfg Config) (*Runtime, error) {
	if ctx == nil {
		return nil, errors.New("turnmesh: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	session, dispatcher, batch, err := buildRuntimeParts(ctx, cfg)
	if err != nil {
		return nil, err
	}

	engine := orchestrator.New(orchestrator.Config{
		Session:   session,
		Tools:     dispatcher,
		ToolBatch: batch,
	})
	if err := engine.Boot(ctx); err != nil {
		return nil, err
	}

	return &Runtime{
		engine:  engine,
		session: session,
	}, nil
}

func RunOneShot(ctx context.Context, cfg Config, req OneShotRequest) (OneShotResult, error) {
	if ctx == nil {
		return OneShotResult{}, errors.New("turnmesh: nil context")
	}
	if err := ctx.Err(); err != nil {
		return OneShotResult{}, err
	}

	session, err := newSession(ctx, cfg, nil)
	if err != nil {
		return OneShotResult{}, err
	}
	defer session.Close()

	return runOneShot(ctx, session, req)
}

func (r *Runtime) Close() error {
	if r == nil || r.session == nil {
		return nil
	}
	return r.session.Close()
}

func (r *Runtime) StreamTurn(ctx context.Context, req TurnRequest) (<-chan Event, error) {
	if r == nil || r.engine == nil {
		return nil, errors.New("turnmesh: runtime is not initialized")
	}

	stream, err := r.engine.StreamTurn(ctx, core.TurnInput{
		SessionID:    req.SessionID,
		TurnID:       req.TurnID,
		SystemPrompt: req.SystemPrompt,
		Messages:     coreMessages(req.Messages),
		Metadata:     cloneMetadata(req.Metadata),
	})
	if err != nil {
		return nil, err
	}

	out := make(chan Event, 16)
	go func() {
		defer close(out)
		for event := range stream {
			select {
			case out <- publicEvent(event):
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (r *Runtime) RunTurn(ctx context.Context, req TurnRequest) (TurnResult, error) {
	stream, err := r.StreamTurn(ctx, req)
	if err != nil {
		return TurnResult{}, err
	}

	result := TurnResult{}
	for event := range stream {
		result.Events = append(result.Events, event)
		result.Status = event.Status
		switch event.Kind {
		case EventMessage:
			if event.Message != nil {
				result.Messages = append(result.Messages, *event.Message)
				if event.Message.Role == RoleAssistant && strings.TrimSpace(event.Message.Content) != "" {
					result.Text = strings.TrimSpace(event.Message.Content)
				}
			}
		case EventToolResult:
			if event.ToolResult != nil {
				result.ToolResults = append(result.ToolResults, *event.ToolResult)
			}
		case EventError:
			if event.Error != nil {
				return result, event.Error
			}
			return result, errors.New("turnmesh: turn failed")
		}
	}
	return result, nil
}

func runOneShot(ctx context.Context, session model.Session, req OneShotRequest) (OneShotResult, error) {
	if session == nil {
		return OneShotResult{}, errors.New("turnmesh: session is not initialized")
	}

	stream, err := session.StreamTurn(ctx, core.TurnInput{
		SystemPrompt: req.SystemPrompt,
		Messages:     coreMessages(req.Messages),
		Metadata:     cloneMetadata(req.Metadata),
	})
	if err != nil {
		return OneShotResult{}, err
	}

	result := OneShotResult{}
	hadToolCall := false
	for raw := range stream {
		event := publicEvent(raw)
		result.Events = append(result.Events, event)
		result.Status = event.Status

		switch event.Kind {
		case EventMessage:
			if event.Message == nil {
				continue
			}
			message := *event.Message
			result.Message = &message
			if message.Role == RoleAssistant && strings.TrimSpace(message.Content) != "" {
				result.Text = strings.TrimSpace(message.Content)
			}
		case EventToolCall:
			hadToolCall = true
		case EventError:
			if event.Error != nil {
				return result, event.Error
			}
			return result, errors.New("turnmesh: one-shot failed")
		}
	}

	if hadToolCall {
		return result, errors.New("turnmesh: one-shot produced tool calls; use RunTurn for tool execution")
	}
	return result, nil
}

func MustJSONSchema(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func buildRuntimeParts(ctx context.Context, cfg Config) (model.Session, executor.Dispatcher, executor.BatchRuntime, error) {
	tools := executor.NewRegistryStore()
	for _, tool := range cfg.Tools {
		spec, err := executorSpec(tool)
		if err != nil {
			return nil, nil, nil, err
		}
		handler := tool.Handler
		if handler == nil {
			return nil, nil, nil, errors.New("turnmesh: tool handler is required")
		}
		handlerCopy := handler
		if err := tools.Register(executor.NewHandlerTool(spec, func(ctx context.Context, req executor.ToolRequest) (executor.ToolOutcome, error) {
			outcome, err := handlerCopy(ctx, ToolCall{
				Name:       req.Tool,
				Input:      cloneRaw(req.Input),
				Arguments:  cloneRaw(req.Arguments),
				Caller:     req.Caller,
				ApprovalID: req.ApprovalID,
				Metadata:   cloneMetadata(req.Metadata),
			})
			return executor.ToolOutcome{
				Output:     outcome.Output,
				Structured: cloneRaw(outcome.Structured),
				Metadata:   cloneMetadata(outcome.Metadata),
				Duration:   outcome.Duration,
				Status:     core.ToolStatus(outcome.Status),
				Error:      coreError(outcome.Error),
			}, err
		})); err != nil {
			return nil, nil, nil, err
		}
	}

	session, err := newSession(ctx, cfg, cfg.Tools)
	if err != nil {
		return nil, nil, nil, err
	}
	return session, executor.NewToolDispatcher(tools), executor.NewBatchRuntime(tools), nil
}

func newSession(ctx context.Context, cfg Config, tools []Tool) (model.Session, error) {
	provider, err := buildProvider(cfg)
	if err != nil {
		return nil, err
	}

	registry := model.NewRegistry()
	if err := registry.Register(provider); err != nil {
		return nil, err
	}

	return registry.NewSession(ctx, provider.Name(), model.SessionOptions{
		Model:           cfg.Model,
		Temperature:     cfg.Temperature,
		MaxOutputTokens: cfg.MaxOutputTokens,
		Tools:           coreToolSpecs(tools),
	})
}

func buildProvider(cfg Config) (model.Provider, error) {
	name := strings.TrimSpace(cfg.Provider)
	switch name {
	case "openai":
		opts := make([]openai.Option, 0, 3)
		if cfg.APIKey != "" {
			opts = append(opts, openai.WithAPIKey(cfg.APIKey))
		}
		if cfg.BaseURL != "" {
			opts = append(opts, openai.WithBaseURL(cfg.BaseURL))
		}
		if cfg.HTTPClient != nil {
			opts = append(opts, openai.WithHTTPClient(cfg.HTTPClient))
		}
		return openai.NewProvider(opts...), nil
	case "anthropic":
		opts := make([]anthropic.Option, 0, 3)
		if cfg.APIKey != "" {
			opts = append(opts, anthropic.WithAPIKey(cfg.APIKey))
		}
		if cfg.BaseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(cfg.BaseURL))
		}
		if cfg.HTTPClient != nil {
			opts = append(opts, anthropic.WithHTTPClient(cfg.HTTPClient))
		}
		return anthropic.NewProvider(opts...), nil
	case "openai-chatcompat":
		opts := make([]openaichat.Option, 0, 3)
		if cfg.APIKey != "" {
			opts = append(opts, openaichat.WithAPIKey(cfg.APIKey))
		}
		if cfg.BaseURL != "" {
			opts = append(opts, openaichat.WithBaseURL(cfg.BaseURL))
		}
		if cfg.HTTPClient != nil {
			opts = append(opts, openaichat.WithHTTPClient(cfg.HTTPClient))
		}
		return openaichat.NewProvider(opts...), nil
	default:
		return nil, errors.New("turnmesh: unsupported provider")
	}
}

func executorSpec(tool Tool) (executor.ToolSpec, error) {
	if strings.TrimSpace(tool.Name) == "" {
		return executor.ToolSpec{}, errors.New("turnmesh: tool name is required")
	}
	input, err := marshalSchema(tool.InputSchema)
	if err != nil {
		return executor.ToolSpec{}, err
	}
	output, err := marshalSchema(tool.OutputSchema)
	if err != nil {
		return executor.ToolSpec{}, err
	}
	return executor.ToolSpec{
		Name:            tool.Name,
		Description:     tool.Description,
		InputSchema:     input,
		OutputSchema:    output,
		ReadOnly:        tool.ReadOnly,
		ConcurrencySafe: tool.ConcurrencySafe,
		Timeout:         tool.Timeout,
		Metadata:        cloneMetadata(tool.Metadata),
	}, nil
}

func coreToolSpecs(tools []Tool) []core.ToolSpec {
	if len(tools) == 0 {
		return nil
	}
	out := make([]core.ToolSpec, 0, len(tools))
	for _, tool := range tools {
		input, _ := marshalSchema(tool.InputSchema)
		output, _ := marshalSchema(tool.OutputSchema)
		out = append(out, core.ToolSpec{
			Name:            tool.Name,
			Description:     tool.Description,
			InputSchema:     input,
			OutputSchema:    output,
			ReadOnly:        tool.ReadOnly,
			ConcurrencySafe: tool.ConcurrencySafe,
			Timeout:         tool.Timeout,
			Metadata:        cloneMetadata(tool.Metadata),
		})
	}
	return out
}

func marshalSchema(schema any) (json.RawMessage, error) {
	switch value := schema.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		return cloneRaw(value), nil
	case []byte:
		return cloneRaw(value), nil
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(b), nil
	}
}

func coreMessages(messages []Message) []core.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]core.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, core.Message{
			Role:     core.MessageRole(message.Role),
			Content:  message.Content,
			Metadata: cloneMetadata(message.Metadata),
		})
	}
	return out
}

func publicEvent(event core.TurnEvent) Event {
	out := Event{
		Kind:     EventKind(event.Kind),
		Status:   TurnStatus(event.Status),
		Metadata: cloneMetadata(event.Metadata),
		Error:    publicError(event.Error),
	}
	if event.Message != nil {
		out.Message = &Message{
			Role:     MessageRole(event.Message.Role),
			Content:  event.Message.Content,
			Metadata: cloneMetadata(event.Message.Metadata),
		}
	}
	if event.ToolCall != nil {
		out.ToolCall = &ToolCall{
			ID:         event.ToolCall.ID,
			Name:       event.ToolCall.Tool,
			Input:      cloneRaw(event.ToolCall.Input),
			Arguments:  cloneRaw(event.ToolCall.Arguments),
			Caller:     event.ToolCall.Caller,
			ApprovalID: event.ToolCall.ApprovalID,
			Metadata:   cloneMetadata(event.ToolCall.Metadata),
		}
	}
	if event.ToolResult != nil {
		out.ToolResult = &ToolResult{
			InvocationID: event.ToolResult.InvocationID,
			Tool:         event.ToolResult.Tool,
			Status:       ToolStatus(event.ToolResult.Status),
			Output:       event.ToolResult.Output,
			Structured:   cloneRaw(event.ToolResult.Structured),
			Error:        publicError(event.ToolResult.Error),
			Duration:     event.ToolResult.Duration,
			Metadata:     cloneMetadata(event.ToolResult.Metadata),
		}
	}
	return out
}

func publicError(err *core.Error) *Error {
	if err == nil {
		return nil
	}
	cause := ""
	if err.Cause != nil {
		cause = err.Cause.Error()
	}
	return &Error{
		Code:    string(err.Code),
		Message: err.Message,
		Cause:   cause,
		Details: cloneMetadata(err.Details),
	}
}

func coreError(err *Error) *core.Error {
	if err == nil {
		return nil
	}
	var cause error
	if err.Cause != "" {
		cause = errors.New(err.Cause)
	}
	return &core.Error{
		Code:    core.ErrorCode(err.Code),
		Message: err.Message,
		Cause:   cause,
		Details: cloneMetadata(err.Details),
	}
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

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
