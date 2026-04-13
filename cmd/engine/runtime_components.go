package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Jayleonc/turnmesh/internal/agent"
	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/executor"
	"github.com/Jayleonc/turnmesh/internal/feedback"
	"github.com/Jayleonc/turnmesh/internal/memory"
	"github.com/Jayleonc/turnmesh/internal/model"
	"github.com/Jayleonc/turnmesh/internal/orchestrator"
)

type memoryCoordinator struct {
	runtime *memory.Runtime
}

func newMemoryCoordinator(runtime *memory.Runtime) *memoryCoordinator {
	return &memoryCoordinator{runtime: runtime}
}

func (c *memoryCoordinator) PrepareTurn(ctx context.Context, input core.TurnInput) (core.TurnInput, error) {
	if c == nil || c.runtime == nil {
		return cloneTurnInput(input), nil
	}

	prepared := cloneTurnInput(input)
	snapshot, err := c.runtime.Snapshot(ctx, memory.Request{
		SessionID: input.SessionID,
		TurnID:    input.TurnID,
		Query:     snapshotQuery(input),
		Metadata:  cloneStringMap(input.Metadata),
	})
	if err != nil {
		return core.TurnInput{}, err
	}
	if len(snapshot) == 0 {
		return prepared, nil
	}

	prepared.Memory = append(prepared.Memory, coreMemoryEntries(snapshot)...)
	return prepared, nil
}

func (c *memoryCoordinator) FinalizeTurn(ctx context.Context, report orchestrator.TurnReport) error {
	if c == nil || c.runtime == nil {
		return nil
	}

	record := memory.Record{
		SessionID:   report.Prepared.SessionID,
		TurnID:      report.Prepared.TurnID,
		Transcript:  turnTranscript(report),
		Messages:    turnMessages(report.Events),
		Events:      cloneTurnEvents(report.Events),
		ToolResults: turnToolResults(report.Events),
		Metadata:    memoryMetadata(report.Prepared),
	}

	writes, err := c.runtime.Writeback(ctx, record)
	if err != nil {
		return err
	}
	if _, err := c.runtime.CommitWrites(ctx, writes); err != nil {
		return err
	}

	plan, err := c.runtime.PlanCompact(ctx, memory.Request{
		SessionID:     report.Prepared.SessionID,
		TurnID:        report.Prepared.TurnID,
		Query:         snapshotQuery(report.Prepared),
		CompactBudget: compactBudget(report.Prepared.Metadata),
		CompactReason: "turn_complete",
		Metadata:      memoryMetadata(report.Prepared),
	})
	if err != nil {
		return err
	}
	if len(plan.RemovedID) == 0 && strings.TrimSpace(plan.Summary) == "" {
		return nil
	}
	_, err = c.runtime.ApplyCompact(ctx, plan, memoryMetadata(report.Prepared))
	return err
}

type filteredToolRuntime struct {
	base    executor.Runtime
	allowed map[string]struct{}
}

func newFilteredToolRuntime(base executor.Runtime, allowed []string) executor.Runtime {
	if base == nil {
		base = executor.NewRegistryStore()
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		allowedSet[name] = struct{}{}
	}
	if len(allowedSet) == 0 {
		return base
	}

	return &filteredToolRuntime{
		base:    base,
		allowed: allowedSet,
	}
}

func (r *filteredToolRuntime) Register(tool executor.ToolRuntime) error {
	if r == nil || r.base == nil {
		return errors.New("engine bootstrap: nil tool runtime")
	}
	return r.base.Register(tool)
}

func (r *filteredToolRuntime) Lookup(name string) (executor.ToolRuntime, bool) {
	if r == nil || r.base == nil || !r.isAllowed(name) {
		return nil, false
	}
	return r.base.Lookup(name)
}

func (r *filteredToolRuntime) List() []executor.ToolSpec {
	if r == nil || r.base == nil {
		return nil
	}

	specs := r.base.List()
	filtered := make([]executor.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if r.isAllowed(spec.Name) {
			filtered = append(filtered, spec)
		}
	}
	return filtered
}

func (r *filteredToolRuntime) Execute(ctx context.Context, name string, request executor.ToolRequest) (executor.ToolOutcome, error) {
	if r == nil || r.base == nil {
		return executor.ToolOutcome{}, errors.New("engine bootstrap: nil tool runtime")
	}
	if !r.isAllowed(name) {
		return executor.ToolOutcome{}, fmt.Errorf("%w: %s", executor.ErrToolNotFound, name)
	}
	return r.base.Execute(ctx, name, request)
}

func (r *filteredToolRuntime) isAllowed(name string) bool {
	if r == nil || len(r.allowed) == 0 {
		return true
	}
	_, ok := r.allowed[name]
	return ok
}

type kernelAgentRunner struct {
	sink      feedback.Sink
	provider  string
	providers *model.Registry
	tools     executor.Runtime
	memory    *memory.Runtime
}

func (r *kernelAgentRunner) Run(ctx context.Context, req agent.RunRequest, emit func(agent.Event)) error {
	if r == nil {
		return errors.New("engine bootstrap: nil agent runner")
	}
	if r.providers == nil {
		return errors.New("engine bootstrap: provider registry is required for agent runtime")
	}

	providerName := strings.TrimSpace(r.provider)
	if providerName == "" {
		return errors.New("engine bootstrap: provider is required for agent runtime")
	}

	toolRuntime := newFilteredToolRuntime(r.tools, req.Definition.AllowedTools)
	session, err := r.providers.NewSession(ctx, providerName, model.SessionOptions{
		Model:        req.Definition.ModelHint,
		SystemPrompt: req.Definition.SystemPrompt,
		Metadata:     cloneStringMap(req.Context.Metadata),
		Tools:        coreToolCatalog(toolRuntime.List()),
	})
	if err != nil {
		return err
	}
	defer session.Close()

	coordinator := newMemoryCoordinator(r.memory)
	engine := orchestrator.New(orchestrator.Config{
		Preparer:  coordinator,
		Finalizer: coordinator,
		Session:   session,
		Tools:     executor.NewToolDispatcher(toolRuntime),
		ToolBatch: executor.NewBatchRuntime(toolRuntime),
		Sink:      r.sink,
	})
	if err := engine.Boot(ctx); err != nil {
		return err
	}

	events, err := engine.StreamTurn(ctx, turnInputFromAgentRun(req))
	if err != nil {
		return err
	}
	for event := range events {
		if err := forwardAgentEvent(event, emit); err != nil {
			return err
		}
	}
	return nil
}

func snapshotQuery(input core.TurnInput) memory.Query {
	query := memory.Query{
		Scope: memory.ScopeSession,
	}
	if input.SessionID != "" {
		query.Metadata = map[string]string{
			"session_id": input.SessionID,
		}
	}

	if limit, err := strconv.Atoi(strings.TrimSpace(input.Metadata["memory.limit"])); err == nil && limit > 0 {
		query.Limit = limit
	}
	if scope := parseMemoryScope(input.Metadata["memory.scope"]); scope != memory.ScopeUnknown {
		query.Scope = scope
	}
	if text := strings.TrimSpace(input.Metadata["memory.text"]); text != "" {
		query.Text = text
	}
	return query
}

func compactBudget(metadata map[string]string) int {
	value := strings.TrimSpace(metadata["memory.compact_budget"])
	if value == "" {
		return 0
	}
	budget, err := strconv.Atoi(value)
	if err != nil || budget <= 0 {
		return 0
	}
	return budget
}

func parseMemoryScope(value string) memory.Scope {
	switch strings.TrimSpace(value) {
	case string(memory.ScopeSession):
		return memory.ScopeSession
	case string(memory.ScopeWorking):
		return memory.ScopeWorking
	case string(memory.ScopeDurable):
		return memory.ScopeDurable
	case string(memory.ScopeProject):
		return memory.ScopeProject
	case string(memory.ScopeReference):
		return memory.ScopeReference
	default:
		return memory.ScopeUnknown
	}
}

func memoryMetadata(input core.TurnInput) map[string]string {
	metadata := cloneStringMap(input.Metadata)
	if metadata == nil {
		metadata = make(map[string]string, 2)
	}
	if input.SessionID != "" {
		metadata["session_id"] = input.SessionID
	}
	if input.TurnID != "" {
		metadata["turn_id"] = input.TurnID
	}
	return metadata
}

func coreMemoryEntries(entries []memory.Entry) []core.MemoryEntry {
	if len(entries) == 0 {
		return nil
	}

	out := make([]core.MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, core.MemoryEntry{
			ID:        entry.ID,
			Scope:     coreMemoryScope(entry.Scope),
			Kind:      coreMemoryKind(entry.Kind),
			Text:      entry.Content,
			CreatedAt: entry.CreatedAt,
			UpdatedAt: entry.UpdatedAt,
			Metadata:  cloneStringMap(entry.Metadata),
		})
	}
	return out
}

func coreMemoryScope(scope memory.Scope) core.MemoryScope {
	switch scope {
	case memory.ScopeWorking:
		return core.MemoryScopeWorking
	case memory.ScopeProject:
		return core.MemoryScopeProject
	case memory.ScopeDurable:
		return core.MemoryScopePersistent
	case memory.ScopeReference:
		return core.MemoryScopeReference
	default:
		return core.MemoryScopeSession
	}
}

func coreMemoryKind(kind string) core.MemoryKind {
	switch strings.TrimSpace(kind) {
	case string(core.MemoryKindFact):
		return core.MemoryKindFact
	case string(core.MemoryKindPref):
		return core.MemoryKindPref
	case string(core.MemoryKindSummary):
		return core.MemoryKindSummary
	case string(core.MemoryKindPlan):
		return core.MemoryKindPlan
	default:
		return core.MemoryKindObservation
	}
}

func turnTranscript(report orchestrator.TurnReport) []core.Message {
	transcript := cloneMessages(report.Requested.Messages)
	transcript = append(transcript, turnMessages(report.Events)...)
	if len(transcript) > 0 {
		return transcript
	}
	return cloneMessages(report.Prepared.Messages)
}

func turnMessages(events []core.TurnEvent) []core.Message {
	if len(events) == 0 {
		return nil
	}

	messages := make([]core.Message, 0, len(events))
	for _, event := range events {
		if event.Message == nil {
			continue
		}
		messages = append(messages, cloneMessage(*event.Message))
	}
	return messages
}

func turnToolResults(events []core.TurnEvent) []core.ToolResult {
	if len(events) == 0 {
		return nil
	}

	results := make([]core.ToolResult, 0, len(events))
	for _, event := range events {
		if event.ToolResult == nil {
			continue
		}
		results = append(results, cloneToolResult(*event.ToolResult))
	}
	return results
}

func turnInputFromAgentRun(req agent.RunRequest) core.TurnInput {
	input := core.TurnInput{
		SessionID:    req.Context.SessionID,
		SystemPrompt: req.Definition.SystemPrompt,
		Metadata:     cloneStringMap(req.Context.Metadata),
	}
	if input.SessionID == "" {
		input.SessionID = "agent-" + req.TaskID
	}
	if input.Metadata == nil {
		input.Metadata = make(map[string]string, 4)
	}
	input.Metadata["task_id"] = req.TaskID
	if req.Definition.ID != "" {
		input.Metadata["agent_id"] = req.Definition.ID
	}
	if req.Context.ParentTaskID != "" {
		input.Metadata["parent_task_id"] = req.Context.ParentTaskID
	}

	switch payload := req.Input.(type) {
	case core.TurnInput:
		input = cloneTurnInput(payload)
		if input.SessionID == "" {
			input.SessionID = req.Context.SessionID
		}
		if input.SessionID == "" {
			input.SessionID = "agent-" + req.TaskID
		}
		if input.SystemPrompt == "" {
			input.SystemPrompt = req.Definition.SystemPrompt
		}
		input.Metadata = mergeStringMap(input.Metadata, cloneStringMap(req.Context.Metadata))
		if input.Metadata == nil {
			input.Metadata = make(map[string]string, 5)
		}
		input.Metadata["task_id"] = req.TaskID
		if req.Definition.ID != "" {
			input.Metadata["agent_id"] = req.Definition.ID
		}
		if req.Context.ParentTaskID != "" {
			input.Metadata["parent_task_id"] = req.Context.ParentTaskID
		}
	case *core.TurnInput:
		if payload != nil {
			return turnInputFromAgentRun(agent.RunRequest{
				TaskID:         req.TaskID,
				Definition:     req.Definition,
				BaseDefinition: req.BaseDefinition,
				Input:          *payload,
				Context:        req.Context,
				Overlay:        req.Overlay,
				StartedAt:      req.StartedAt,
			})
		}
	case []core.Message:
		input.Messages = cloneMessages(payload)
	case string:
		if strings.TrimSpace(payload) != "" {
			input.Messages = []core.Message{{Role: core.MessageRoleUser, Content: payload}}
		}
	case []byte:
		if strings.TrimSpace(string(payload)) != "" {
			input.Messages = []core.Message{{Role: core.MessageRoleUser, Content: string(payload)}}
		}
	default:
		if text := stringifyAgentInput(payload); text != "" {
			input.Messages = []core.Message{{Role: core.MessageRoleUser, Content: text}}
		}
	}

	return input
}

func forwardAgentEvent(event core.TurnEvent, emit func(agent.Event)) error {
	if emit == nil {
		if event.Kind == core.TurnEventError {
			if event.Error != nil {
				return event.Error
			}
			return errors.New("agent turn failed")
		}
		return nil
	}

	switch event.Kind {
	case core.TurnEventMessage, core.TurnEventDelta, core.TurnEventToolCall, core.TurnEventToolResult, core.TurnEventMemoryRead, core.TurnEventMemoryWrite:
		emit(agent.Event{
			TaskID:    event.TurnID,
			Type:      agent.EventTypeMessage,
			Payload:   cloneTurnEvent(event),
			Timestamp: event.Timestamp,
			Metadata:  cloneStringMap(event.Metadata),
		})
	case core.TurnEventError:
		emit(agent.Event{
			TaskID:    event.TurnID,
			Type:      agent.EventTypeError,
			Payload:   cloneTurnEvent(event),
			Timestamp: event.Timestamp,
			Metadata:  cloneStringMap(event.Metadata),
		})
		if event.Error != nil {
			return event.Error
		}
		return errors.New("agent turn failed")
	}
	return nil
}

func stringifyAgentInput(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	}

	raw, err := json.Marshal(value)
	if err == nil && len(raw) > 0 && string(raw) != "null" {
		return string(raw)
	}
	return fmt.Sprint(value)
}

func mergeStringMap(base map[string]string, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}

	merged := cloneStringMap(base)
	if merged == nil {
		merged = make(map[string]string, len(overlay))
	}
	for key, value := range overlay {
		merged[key] = value
	}
	return merged
}

func cloneTurnInput(input core.TurnInput) core.TurnInput {
	cloned := input
	cloned.Messages = cloneMessages(input.Messages)
	cloned.Memory = cloneMemoryEntries(input.Memory)
	cloned.Tools = cloneToolSpecs(input.Tools)
	cloned.Tasks = cloneTaskStates(input.Tasks)
	cloned.Approvals = cloneApprovals(input.Approvals)
	cloned.Metadata = cloneStringMap(input.Metadata)
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
		approval := *event.Approval
		approval.Metadata = cloneStringMap(event.Approval.Metadata)
		cloned.Approval = &approval
	}
	if event.Memory != nil {
		entry := *event.Memory
		entry.Tags = append([]string(nil), event.Memory.Tags...)
		entry.Metadata = cloneStringMap(event.Memory.Metadata)
		cloned.Memory = &entry
	}
	if event.Task != nil {
		task := *event.Task
		task.Metadata = cloneStringMap(event.Task.Metadata)
		if event.Task.Error != nil {
			err := *event.Task.Error
			err.Details = cloneStringMap(event.Task.Error.Details)
			task.Error = &err
		}
		cloned.Task = &task
	}
	if event.Error != nil {
		err := *event.Error
		err.Details = cloneStringMap(event.Error.Details)
		cloned.Error = &err
	}
	cloned.Metadata = cloneStringMap(event.Metadata)
	return cloned
}

func cloneTurnEvents(events []core.TurnEvent) []core.TurnEvent {
	if len(events) == 0 {
		return nil
	}
	out := make([]core.TurnEvent, 0, len(events))
	for _, event := range events {
		out = append(out, cloneTurnEvent(event))
	}
	return out
}

func cloneMessages(messages []core.Message) []core.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]core.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, cloneMessage(message))
	}
	return out
}

func cloneMessage(message core.Message) core.Message {
	cloned := message
	cloned.Metadata = cloneStringMap(message.Metadata)
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
	cloned.Data = append([]byte(nil), part.Data...)
	cloned.Metadata = cloneStringMap(part.Metadata)
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
	cloned.Input = cloneRawMessage(call.Input)
	cloned.Arguments = cloneRawMessage(call.Arguments)
	cloned.Metadata = cloneStringMap(call.Metadata)
	return cloned
}

func cloneToolResult(result core.ToolResult) core.ToolResult {
	cloned := result
	cloned.Structured = cloneRawMessage(result.Structured)
	cloned.Metadata = cloneStringMap(result.Metadata)
	if result.Error != nil {
		err := *result.Error
		err.Details = cloneStringMap(result.Error.Details)
		cloned.Error = &err
	}
	return cloned
}

func cloneMemoryEntries(entries []core.MemoryEntry) []core.MemoryEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]core.MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		cloned := entry
		cloned.Tags = append([]string(nil), entry.Tags...)
		cloned.Metadata = cloneStringMap(entry.Metadata)
		out = append(out, cloned)
	}
	return out
}

func cloneToolSpecs(specs []core.ToolSpec) []core.ToolSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]core.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		cloned := spec
		cloned.InputSchema = cloneRawMessage(spec.InputSchema)
		cloned.OutputSchema = cloneRawMessage(spec.OutputSchema)
		cloned.Metadata = cloneStringMap(spec.Metadata)
		out = append(out, cloned)
	}
	return out
}

func cloneTaskStates(tasks []core.TaskState) []core.TaskState {
	if len(tasks) == 0 {
		return nil
	}
	out := make([]core.TaskState, 0, len(tasks))
	for _, task := range tasks {
		cloned := task
		cloned.Metadata = cloneStringMap(task.Metadata)
		if task.Error != nil {
			err := *task.Error
			err.Details = cloneStringMap(task.Error.Details)
			cloned.Error = &err
		}
		out = append(out, cloned)
	}
	return out
}

func cloneApprovals(approvals []core.ApprovalRequest) []core.ApprovalRequest {
	if len(approvals) == 0 {
		return nil
	}
	out := make([]core.ApprovalRequest, 0, len(approvals))
	for _, approval := range approvals {
		cloned := approval
		cloned.Metadata = cloneStringMap(approval.Metadata)
		out = append(out, cloned)
	}
	return out
}
