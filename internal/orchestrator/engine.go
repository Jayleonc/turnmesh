package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/feedback"
)

var ErrNotBooted = errors.New("orchestrator: engine not booted")

const defaultMaxModelPasses = 16

type Engine struct {
	cfg     Config
	booted  atomic.Bool
	turnSeq atomic.Uint64
	once    sync.Once
}

func New(cfg Config) *Engine {
	if cfg.Sink == nil {
		cfg.Sink = noopSink{}
	}

	return &Engine{cfg: cfg}
}

func (e *Engine) Boot(ctx context.Context) error {
	if ctx == nil {
		return errors.New("orchestrator: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	e.once.Do(func() {
		e.booted.Store(true)
		e.emitFeedback(ctx, feedback.Event{
			Time:    time.Now().UTC(),
			Level:   feedback.LevelInfo,
			Kind:    "engine.booted",
			Message: "engine booted",
		})
	})

	if !e.booted.Load() {
		return fmt.Errorf("orchestrator: boot failed")
	}

	return nil
}

func (e *Engine) Booted() bool {
	return e.booted.Load()
}

func (e *Engine) StreamTurn(ctx context.Context, req core.TurnInput) (<-chan core.TurnEvent, error) {
	if ctx == nil {
		return nil, errors.New("orchestrator: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !e.booted.Load() {
		return nil, ErrNotBooted
	}

	turnID := req.TurnID
	if turnID == "" {
		turnID = fmt.Sprintf("turn-%d", e.turnSeq.Add(1))
	}

	events := make(chan core.TurnEvent, 16)
	go e.runTurn(ctx, turnID, req, events)
	return events, nil
}

func (e *Engine) runTurn(ctx context.Context, turnID string, req core.TurnInput, out chan<- core.TurnEvent) {
	defer close(out)

	sequence := int64(0)
	emittedEvents := make([]core.TurnEvent, 0, 16)
	emit := func(event core.TurnEvent) bool {
		event.TurnID = turnID
		event.Sequence = sequence
		if event.Timestamp.IsZero() {
			event.Timestamp = time.Now().UTC()
		}
		sequence++

		if ctx.Err() != nil {
			return false
		}

		select {
		case out <- event:
		case <-ctx.Done():
			return false
		}

		emittedEvents = append(emittedEvents, cloneTurnEvent(event))
		e.emitTurnEvent(ctx, event)
		return true
	}

	if !emit(core.TurnEvent{
		Kind:   core.TurnEventStarted,
		Status: core.TurnStatusRunning,
		Metadata: map[string]string{
			"reason": "turn started",
		},
	}) {
		return
	}

	normalized := req
	finalized := false
	shouldFinalize := false
	defer func() {
		if finalized || !shouldFinalize || e.cfg.Finalizer == nil {
			return
		}

		report := TurnReport{
			Requested: cloneTurnInput(req),
			Prepared:  cloneTurnInput(normalized),
			Events:    cloneTurnEvents(emittedEvents),
		}
		if err := e.cfg.Finalizer.FinalizeTurn(ctx, report); err != nil {
			e.emitFeedback(ctx, feedback.Event{
				Time:    time.Now().UTC(),
				Level:   feedback.LevelError,
				Kind:    "turn.finalizer_error",
				Message: err.Error(),
				Data: map[string]any{
					"turn_id": turnID,
				},
			})
		}
		finalized = true
	}()

	if e.cfg.Preparer != nil {
		prepared, err := e.cfg.Preparer.PrepareTurn(ctx, req)
		if err != nil {
			emit(core.TurnEvent{
				Kind:   core.TurnEventError,
				Status: core.TurnStatusFailed,
				Error:  core.WrapError(core.ErrorCodeValidation, "turn preparation failed", err),
			})
			return
		}
		normalized = prepared
	}
	shouldFinalize = true
	for _, entry := range normalized.Memory {
		memoryEntry := cloneMemoryEntry(entry)
		if !emit(core.TurnEvent{
			Kind:   core.TurnEventMemoryRead,
			Status: core.TurnStatusRunning,
			Memory: &memoryEntry,
		}) {
			return
		}
	}

	if e.cfg.Session == nil {
		emit(core.TurnEvent{
			Kind:   core.TurnEventCompleted,
			Status: core.TurnStatusCompleted,
			Metadata: map[string]string{
				"reason": "model session not configured",
			},
		})
		return
	}

	for pass := 0; pass < defaultMaxModelPasses; pass++ {
		stream, err := e.cfg.Session.StreamTurn(ctx, normalized)
		if err != nil {
			emit(core.TurnEvent{
				Kind:   core.TurnEventError,
				Status: core.TurnStatusFailed,
				Error:  core.WrapError(core.ErrorCodeInternal, "model stream unavailable", err),
			})
			return
		}

		cycleMessages, toolCalls, toolResults, terminal, ok := e.consumeModelPass(ctx, stream, emit)
		if !ok {
			return
		}
		if terminal {
			return
		}
		if len(toolCalls) == 0 {
			emit(core.TurnEvent{
				Kind:   core.TurnEventCompleted,
				Status: core.TurnStatusCompleted,
				Metadata: map[string]string{
					"reason": "model stream closed",
				},
			})
			return
		}

		normalized.Messages = append(normalized.Messages, continuationMessages(cycleMessages, toolCalls, toolResults)...)
	}

	emit(core.TurnEvent{
		Kind:   core.TurnEventError,
		Status: core.TurnStatusFailed,
		Error:  core.NewError(core.ErrorCodeInternal, "turn exceeded model pass limit"),
	})
}

func (e *Engine) consumeModelPass(
	ctx context.Context,
	stream <-chan core.TurnEvent,
	emit func(core.TurnEvent) bool,
) ([]core.Message, []core.ToolInvocation, []core.ToolResult, bool, bool) {
	var (
		cycleMessages   []core.Message
		toolCalls       []core.ToolInvocation
		toolResults     []core.ToolResult
		deferredSuccess *core.TurnEvent
	)

	for {
		select {
		case <-ctx.Done():
			return nil, nil, nil, false, emit(core.TurnEvent{
				Kind:   core.TurnEventError,
				Status: core.TurnStatusInterrupted,
				Error:  core.WrapError(core.ErrorCodeCancelled, "turn interrupted", ctx.Err()),
			})
		case event, ok := <-stream:
			if !ok {
				if len(toolCalls) == 0 {
					if deferredSuccess != nil {
						return cycleMessages, nil, nil, true, emit(*deferredSuccess)
					}
					return cycleMessages, nil, nil, false, true
				}

				results, ok := e.executeToolCalls(ctx, emit, toolCalls)
				if !ok {
					return cycleMessages, toolCalls, toolResults, false, false
				}
				toolResults = append(toolResults, results...)
				return cycleMessages, toolCalls, toolResults, false, true
			}

			if event.Status == "" {
				event.Status = defaultStatusForEvent(event.Kind)
			}

			switch event.Kind {
			case core.TurnEventStarted:
				continue
			case core.TurnEventMessage:
				if event.Message != nil {
					cycleMessages = append(cycleMessages, cloneMessage(*event.Message))
				}
			case core.TurnEventToolCall:
				if event.ToolCall != nil {
					toolCalls = append(toolCalls, cloneToolInvocation(*event.ToolCall))
				}
			case core.TurnEventCompleted:
				completed := event
				deferredSuccess = &completed
				continue
			}

			if !emit(event) {
				return cycleMessages, toolCalls, toolResults, false, false
			}
			if isTerminalEvent(event.Kind) {
				return cycleMessages, toolCalls, toolResults, true, true
			}
		}
	}
}

func (e *Engine) executeToolCalls(
	ctx context.Context,
	emit func(core.TurnEvent) bool,
	calls []core.ToolInvocation,
) ([]core.ToolResult, bool) {
	if len(calls) == 0 {
		return nil, true
	}

	if e.cfg.ToolBatch != nil {
		report, _ := e.cfg.ToolBatch.Run(ctx, calls)
		results := make([]core.ToolResult, 0, len(report.Results))
		for _, result := range report.Results {
			cloned := cloneToolResult(result)
			results = append(results, cloned)
			if !emit(core.TurnEvent{
				Kind:       core.TurnEventToolResult,
				Status:     core.TurnStatusRunning,
				ToolResult: &cloned,
			}) {
				return results, false
			}
		}
		return results, true
	}

	results := make([]core.ToolResult, 0, len(calls))
	for _, call := range calls {
		result, ok := e.executeToolCall(ctx, emit, call)
		if !ok {
			return results, false
		}
		results = append(results, result)
	}
	return results, true
}

func (e *Engine) executeToolCall(
	ctx context.Context,
	emit func(core.TurnEvent) bool,
	call core.ToolInvocation,
) (core.ToolResult, bool) {
	if e.cfg.Tools == nil {
		result := core.ToolResult{
			InvocationID: call.ID,
			Tool:         call.Tool,
			Status:       core.ToolStatusFailed,
			Error:        core.NewError(core.ErrorCodeUnsupported, "tool runtime not configured"),
		}
		return result, emit(core.TurnEvent{
			Kind:       core.TurnEventToolResult,
			Status:     core.TurnStatusRunning,
			ToolResult: &result,
			Metadata: map[string]string{
				"reason": "synthetic tool failure",
			},
		})
	}

	result, err := e.cfg.Tools.ExecuteTool(ctx, call)
	if result.InvocationID == "" {
		result.InvocationID = call.ID
	}
	if result.Tool == "" {
		result.Tool = call.Tool
	}
	if result.Status == "" {
		if err != nil {
			result.Status = core.ToolStatusFailed
		} else {
			result.Status = core.ToolStatusSucceeded
		}
	}
	if err != nil && result.Error == nil {
		result.Error = core.WrapError(core.ErrorCodeInternal, "tool execution failed", err)
	}

	return result, emit(core.TurnEvent{
		Kind:       core.TurnEventToolResult,
		Status:     core.TurnStatusRunning,
		ToolResult: &result,
	})
}

func continuationMessages(
	cycleMessages []core.Message,
	toolCalls []core.ToolInvocation,
	toolResults []core.ToolResult,
) []core.Message {
	messages := cloneMessages(cycleMessages)
	if len(toolCalls) == 0 {
		return messages
	}

	if len(messages) > 0 && messages[len(messages)-1].Role == core.MessageRoleAssistant {
		messages[len(messages)-1] = appendToolCallsToMessage(messages[len(messages)-1], toolCalls)
	} else {
		messages = append(messages, assistantToolCallMessage(toolCalls))
	}

	if len(toolResults) > 0 {
		messages = append(messages, toolResultMessage(toolResults))
	}
	return messages
}

func assistantToolCallMessage(toolCalls []core.ToolInvocation) core.Message {
	parts := make([]core.MessagePart, 0, len(toolCalls))
	for _, call := range toolCalls {
		cloned := cloneToolInvocation(call)
		parts = append(parts, core.MessagePart{
			Type:     core.MessagePartToolCall,
			ToolCall: &cloned,
		})
	}

	return core.Message{
		Role:  core.MessageRoleAssistant,
		Parts: parts,
	}
}

func toolResultMessage(results []core.ToolResult) core.Message {
	parts := make([]core.MessagePart, 0, len(results))
	for _, result := range results {
		cloned := cloneToolResult(result)
		parts = append(parts, core.MessagePart{
			Type:       core.MessagePartToolResult,
			ToolResult: &cloned,
		})
	}

	return core.Message{
		Role:  core.MessageRoleTool,
		Parts: parts,
	}
}

func appendToolCallsToMessage(message core.Message, toolCalls []core.ToolInvocation) core.Message {
	if message.Content != "" {
		message.Parts = append([]core.MessagePart{{
			Type: core.MessagePartText,
			Text: message.Content,
		}}, message.Parts...)
		message.Content = ""
	}

	for _, call := range toolCalls {
		cloned := cloneToolInvocation(call)
		message.Parts = append(message.Parts, core.MessagePart{
			Type:     core.MessagePartToolCall,
			ToolCall: &cloned,
		})
	}
	return message
}

func cloneMessages(messages []core.Message) []core.Message {
	if len(messages) == 0 {
		return nil
	}

	cloned := make([]core.Message, 0, len(messages))
	for _, message := range messages {
		cloned = append(cloned, cloneMessage(message))
	}
	return cloned
}

func cloneMessage(message core.Message) core.Message {
	cloned := message
	if len(message.Metadata) > 0 {
		cloned.Metadata = cloneMetadata(message.Metadata)
	}
	if len(message.Parts) > 0 {
		cloned.Parts = make([]core.MessagePart, 0, len(message.Parts))
		for _, part := range message.Parts {
			cloned.Parts = append(cloned.Parts, cloneMessagePart(part))
		}
	}
	return cloned
}

func cloneMessagePart(part core.MessagePart) core.MessagePart {
	cloned := part
	if len(part.Data) > 0 {
		cloned.Data = append([]byte(nil), part.Data...)
	}
	if len(part.Metadata) > 0 {
		cloned.Metadata = cloneMetadata(part.Metadata)
	}
	if part.ToolCall != nil {
		call := cloneToolInvocation(*part.ToolCall)
		cloned.ToolCall = &call
	}
	if part.ToolResult != nil {
		result := cloneToolResult(*part.ToolResult)
		cloned.ToolResult = &result
	}
	return cloned
}

func cloneToolInvocation(call core.ToolInvocation) core.ToolInvocation {
	cloned := call
	if len(call.Input) > 0 {
		cloned.Input = append([]byte(nil), call.Input...)
	}
	if len(call.Arguments) > 0 {
		cloned.Arguments = append([]byte(nil), call.Arguments...)
	}
	if len(call.Metadata) > 0 {
		cloned.Metadata = cloneMetadata(call.Metadata)
	}
	return cloned
}

func cloneToolResult(result core.ToolResult) core.ToolResult {
	cloned := result
	if len(result.Structured) > 0 {
		cloned.Structured = append([]byte(nil), result.Structured...)
	}
	if len(result.Metadata) > 0 {
		cloned.Metadata = cloneMetadata(result.Metadata)
	}
	if result.Error != nil {
		err := *result.Error
		cloned.Error = &err
	}
	return cloned
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func cloneTurnInput(input core.TurnInput) core.TurnInput {
	cloned := input
	cloned.Messages = cloneMessages(input.Messages)
	if len(input.Tools) > 0 {
		cloned.Tools = make([]core.ToolSpec, 0, len(input.Tools))
		for _, tool := range input.Tools {
			cloned.Tools = append(cloned.Tools, cloneToolSpec(tool))
		}
	}
	if len(input.Memory) > 0 {
		cloned.Memory = make([]core.MemoryEntry, 0, len(input.Memory))
		for _, entry := range input.Memory {
			cloned.Memory = append(cloned.Memory, cloneMemoryEntry(entry))
		}
	}
	if len(input.Tasks) > 0 {
		cloned.Tasks = make([]core.TaskState, 0, len(input.Tasks))
		for _, task := range input.Tasks {
			cloned.Tasks = append(cloned.Tasks, cloneTaskState(task))
		}
	}
	if len(input.Approvals) > 0 {
		cloned.Approvals = make([]core.ApprovalRequest, 0, len(input.Approvals))
		for _, approval := range input.Approvals {
			cloned.Approvals = append(cloned.Approvals, cloneApprovalRequest(approval))
		}
	}
	cloned.Metadata = cloneMetadata(input.Metadata)
	return cloned
}

func cloneTurnEvents(events []core.TurnEvent) []core.TurnEvent {
	if len(events) == 0 {
		return nil
	}

	cloned := make([]core.TurnEvent, 0, len(events))
	for _, event := range events {
		cloned = append(cloned, cloneTurnEvent(event))
	}
	return cloned
}

func cloneTurnEvent(event core.TurnEvent) core.TurnEvent {
	cloned := event
	if event.Message != nil {
		message := cloneMessage(*event.Message)
		cloned.Message = &message
	}
	if event.ToolCall != nil {
		call := cloneToolInvocation(*event.ToolCall)
		cloned.ToolCall = &call
	}
	if event.ToolResult != nil {
		result := cloneToolResult(*event.ToolResult)
		cloned.ToolResult = &result
	}
	if event.Approval != nil {
		approval := cloneApprovalRequest(*event.Approval)
		cloned.Approval = &approval
	}
	if event.Memory != nil {
		entry := cloneMemoryEntry(*event.Memory)
		cloned.Memory = &entry
	}
	if event.Task != nil {
		task := cloneTaskState(*event.Task)
		cloned.Task = &task
	}
	if event.Error != nil {
		err := *event.Error
		err.Details = cloneMetadata(event.Error.Details)
		cloned.Error = &err
	}
	cloned.Metadata = cloneMetadata(event.Metadata)
	return cloned
}

func cloneToolSpec(spec core.ToolSpec) core.ToolSpec {
	cloned := spec
	if len(spec.InputSchema) > 0 {
		cloned.InputSchema = append([]byte(nil), spec.InputSchema...)
	}
	if len(spec.OutputSchema) > 0 {
		cloned.OutputSchema = append([]byte(nil), spec.OutputSchema...)
	}
	cloned.Metadata = cloneMetadata(spec.Metadata)
	return cloned
}

func cloneMemoryEntry(entry core.MemoryEntry) core.MemoryEntry {
	cloned := entry
	if len(entry.Tags) > 0 {
		cloned.Tags = append([]string(nil), entry.Tags...)
	}
	cloned.Metadata = cloneMetadata(entry.Metadata)
	return cloned
}

func cloneTaskState(task core.TaskState) core.TaskState {
	cloned := task
	if task.Error != nil {
		err := *task.Error
		err.Details = cloneMetadata(task.Error.Details)
		cloned.Error = &err
	}
	cloned.Metadata = cloneMetadata(task.Metadata)
	return cloned
}

func cloneApprovalRequest(request core.ApprovalRequest) core.ApprovalRequest {
	cloned := request
	cloned.Metadata = cloneMetadata(request.Metadata)
	return cloned
}

func (e *Engine) emitTurnEvent(ctx context.Context, event core.TurnEvent) {
	level := feedback.LevelInfo
	if event.Kind == core.TurnEventError || event.Status == core.TurnStatusFailed {
		level = feedback.LevelError
	}

	data := map[string]any{
		"turn_id":  event.TurnID,
		"sequence": event.Sequence,
		"status":   event.Status,
	}
	if event.ToolCall != nil {
		data["tool"] = event.ToolCall.Tool
	}
	if event.ToolResult != nil {
		data["tool"] = event.ToolResult.Tool
		data["tool_status"] = event.ToolResult.Status
	}
	if event.Error != nil {
		data["error_code"] = event.Error.Code
	}

	e.emitFeedback(ctx, feedback.Event{
		Time:    event.Timestamp,
		Level:   level,
		Kind:    string(event.Kind),
		Message: eventMessage(event),
		Data:    data,
	})
}

func (e *Engine) emitFeedback(ctx context.Context, event feedback.Event) {
	if e.cfg.Sink == nil {
		return
	}
	_ = e.cfg.Sink.Emit(ctx, event)
}

func defaultStatusForEvent(kind core.TurnEventKind) core.TurnStatus {
	switch kind {
	case core.TurnEventCompleted:
		return core.TurnStatusCompleted
	case core.TurnEventError:
		return core.TurnStatusFailed
	default:
		return core.TurnStatusRunning
	}
}

func isTerminalEvent(kind core.TurnEventKind) bool {
	return kind == core.TurnEventCompleted || kind == core.TurnEventError
}

func eventMessage(event core.TurnEvent) string {
	switch {
	case event.Error != nil && event.Error.Message != "":
		return event.Error.Message
	case event.ToolCall != nil:
		return "tool requested"
	case event.ToolResult != nil:
		return "tool completed"
	case event.Message != nil && event.Message.Content != "":
		return event.Message.Content
	default:
		return string(event.Kind)
	}
}
