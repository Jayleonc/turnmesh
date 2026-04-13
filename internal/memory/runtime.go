package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Jayleonc/turnmesh/internal/core"
)

// Runtime coordinates turn-level memory snapshot, writeback, and compaction.
type Runtime struct {
	Store  Store
	Policy Policy
}

// NewRuntime constructs a memory runtime from the provided store and policy.
func NewRuntime(store Store, policy Policy) *Runtime {
	return &Runtime{
		Store:  store,
		Policy: policy,
	}
}

// Snapshot resolves the memory view for a turn without mutating persistence.
func (r *Runtime) Snapshot(ctx context.Context, req Request) ([]Entry, error) {
	if ctx == nil {
		return nil, errors.New("memory runtime: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.Store == nil {
		return nil, nil
	}

	entries, err := r.Store.List(ctx, queryForRequest(req))
	if err != nil {
		return nil, err
	}
	if r.Policy == nil {
		return cloneEntries(entries), nil
	}

	filtered := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if r.Policy.AllowRead(ctx, entry) {
			filtered = append(filtered, cloneEntry(entry))
		}
	}
	return filtered, nil
}

// Writeback plans which entries should be persisted after a turn.
func (r *Runtime) Writeback(ctx context.Context, record Record) ([]WriteRequest, error) {
	if ctx == nil {
		return nil, errors.New("memory runtime: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.Store == nil {
		return nil, nil
	}

	candidates := cloneEntries(record.Candidates)
	if len(candidates) == 0 {
		candidates = defaultCandidates(record)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	requests := make([]WriteRequest, 0, len(candidates))
	for _, candidate := range candidates {
		entry := normalizeRuntimeEntry(candidate, record.SessionID, record.TurnID, record.Metadata)
		if r.Policy != nil && !r.Policy.AllowWrite(ctx, entry) {
			continue
		}
		requests = append(requests, WriteRequest{Entry: entry})
	}
	return requests, nil
}

// CommitWrites persists the provided write requests in order.
func (r *Runtime) CommitWrites(ctx context.Context, requests []WriteRequest) ([]WriteResult, error) {
	if ctx == nil {
		return nil, errors.New("memory runtime: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.Store == nil || len(requests) == 0 {
		return nil, nil
	}

	results := make([]WriteResult, 0, len(requests))
	for _, request := range requests {
		result, err := r.Store.Put(ctx, request)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// PlanCompact asks the policy whether the current memory set should compact.
func (r *Runtime) PlanCompact(ctx context.Context, req Request) (CompactResult, error) {
	if ctx == nil {
		return CompactResult{}, errors.New("memory runtime: nil context")
	}
	if err := ctx.Err(); err != nil {
		return CompactResult{}, err
	}
	if r == nil || r.Store == nil || r.Policy == nil {
		return CompactResult{}, nil
	}

	entries := cloneEntries(req.Memory)
	if len(entries) == 0 {
		var err error
		entries, err = r.Store.List(ctx, queryForRequest(req))
		if err != nil {
			return CompactResult{}, err
		}
	}
	if len(entries) == 0 {
		return CompactResult{}, nil
	}

	shouldCompact, err := r.Policy.ShouldCompact(ctx, cloneEntries(entries))
	if err != nil || !shouldCompact {
		return CompactResult{}, err
	}

	result, err := r.Policy.PlanCompact(ctx, CompactRequest{
		Scope:    queryForRequest(req).Scope,
		Entries:  cloneEntries(entries),
		Budget:   req.CompactBudget,
		Reason:   req.CompactReason,
		Metadata: runtimeMetadata(req.Metadata, req.SessionID, req.TurnID),
	})
	if err != nil {
		return CompactResult{}, err
	}
	return cloneCompactResult(result), nil
}

// ApplyCompact executes a previously planned compaction result.
func (r *Runtime) ApplyCompact(ctx context.Context, result CompactResult, metadata map[string]string) ([]WriteResult, error) {
	if ctx == nil {
		return nil, errors.New("memory runtime: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.Store == nil {
		return nil, nil
	}

	var writes []WriteResult
	if strings.TrimSpace(result.Summary) == "" {
		writes = nil
	} else {
		scope := ScopeSession
		if len(result.Retained) > 0 && result.Retained[0].Scope != ScopeUnknown {
			scope = result.Retained[0].Scope
		}

		var err error
		writes, err = r.CommitWrites(ctx, []WriteRequest{{
			Entry: Entry{
				Scope:    scope,
				Kind:     "summary",
				Content:  result.Summary,
				Metadata: mergeMetadata(runtimeMetadata(metadata, "", ""), result.Metadata),
			},
		}})
		if err != nil {
			return nil, err
		}
	}

	for _, id := range result.RemovedID {
		if err := r.Store.Delete(ctx, DeleteRequest{ID: id}); err != nil && !errors.Is(err, ErrNotFound) {
			return writes, err
		}
	}
	return writes, nil
}

func queryForRequest(req Request) Query {
	query := req.Query
	if query.Scope == ScopeUnknown {
		query.Scope = ScopeSession
	}
	query.Metadata = runtimeMetadata(query.Metadata, req.SessionID, "")
	return query
}

func defaultCandidates(record Record) []Entry {
	var lines []string

	transcript := record.Transcript
	if len(transcript) == 0 {
		transcript = record.Messages
	}
	for _, message := range transcript {
		text := strings.TrimSpace(messageText(message))
		if text == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", message.Role, text))
	}

	for _, result := range record.ToolResults {
		text := strings.TrimSpace(result.Output)
		if text == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("tool %s: %s", result.Tool, text))
	}

	if len(lines) == 0 {
		return nil
	}

	return []Entry{{
		Scope:    ScopeSession,
		Kind:     "summary",
		Content:  strings.Join(lines, "\n"),
		Metadata: runtimeMetadata(record.Metadata, record.SessionID, record.TurnID),
	}}
}

func normalizeRuntimeEntry(entry Entry, sessionID string, turnID string, metadata map[string]string) Entry {
	normalized := cloneEntry(entry)
	if normalized.Scope == ScopeUnknown {
		normalized.Scope = ScopeSession
	}
	if strings.TrimSpace(normalized.Kind) == "" {
		normalized.Kind = "summary"
	}
	normalized.Metadata = mergeMetadata(runtimeMetadata(metadata, sessionID, turnID), normalized.Metadata)
	return normalized
}

func runtimeMetadata(base map[string]string, sessionID string, turnID string) map[string]string {
	merged := cloneMetadata(base)
	if sessionID != "" {
		merged["session_id"] = sessionID
	}
	if turnID != "" {
		merged["turn_id"] = turnID
	}
	return merged
}

func mergeMetadata(base map[string]string, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return map[string]string{}
	}

	merged := cloneMetadata(base)
	for key, value := range overlay {
		merged[key] = value
	}
	return merged
}

func cloneEntries(entries []Entry) []Entry {
	if len(entries) == 0 {
		return nil
	}

	cloned := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		cloned = append(cloned, cloneEntry(entry))
	}
	return cloned
}

func cloneCompactResult(result CompactResult) CompactResult {
	cloned := result
	cloned.Retained = cloneEntries(result.Retained)
	if len(result.RemovedID) > 0 {
		cloned.RemovedID = append([]string(nil), result.RemovedID...)
	}
	cloned.Metadata = cloneMetadata(result.Metadata)
	return cloned
}

func messageText(message core.Message) string {
	if strings.TrimSpace(message.Content) != "" {
		return message.Content
	}

	if len(message.Parts) == 0 {
		return ""
	}

	var parts []string
	for _, part := range message.Parts {
		switch {
		case strings.TrimSpace(part.Text) != "":
			parts = append(parts, part.Text)
		case part.ToolResult != nil && strings.TrimSpace(part.ToolResult.Output) != "":
			parts = append(parts, part.ToolResult.Output)
		}
	}
	return strings.Join(parts, "\n")
}
