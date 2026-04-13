package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
)

type stubPolicy struct {
	allowRead     func(context.Context, Entry) bool
	allowWrite    func(context.Context, Entry) bool
	shouldCompact bool
	compactResult CompactResult
}

func (p stubPolicy) AllowRead(ctx context.Context, entry Entry) bool {
	if p.allowRead != nil {
		return p.allowRead(ctx, entry)
	}
	return true
}

func (p stubPolicy) AllowWrite(ctx context.Context, entry Entry) bool {
	if p.allowWrite != nil {
		return p.allowWrite(ctx, entry)
	}
	return true
}

func (p stubPolicy) ShouldCompact(context.Context, []Entry) (bool, error) {
	return p.shouldCompact, nil
}

func (p stubPolicy) PlanCompact(context.Context, CompactRequest) (CompactResult, error) {
	return p.compactResult, nil
}

func TestRuntimeSnapshotFiltersBySessionAndPolicy(t *testing.T) {
	clock := fixedClock(time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC))
	store := NewInMemoryStoreWithClock(clock)
	_, _ = store.Put(context.Background(), WriteRequest{Entry: Entry{
		Scope:    ScopeSession,
		Kind:     "summary",
		Content:  "keep me",
		Metadata: map[string]string{"session_id": "session-a"},
	}})
	_, _ = store.Put(context.Background(), WriteRequest{Entry: Entry{
		Scope:    ScopeSession,
		Kind:     "summary",
		Content:  "skip me",
		Metadata: map[string]string{"session_id": "session-a", "private": "true"},
	}})
	_, _ = store.Put(context.Background(), WriteRequest{Entry: Entry{
		Scope:    ScopeSession,
		Kind:     "summary",
		Content:  "other session",
		Metadata: map[string]string{"session_id": "session-b"},
	}})

	runtime := NewRuntime(store, stubPolicy{
		allowRead: func(_ context.Context, entry Entry) bool {
			return entry.Metadata["private"] != "true"
		},
	})

	entries, err := runtime.Snapshot(context.Background(), Request{SessionID: "session-a"})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Content != "keep me" {
		t.Fatalf("entry content = %q, want keep me", entries[0].Content)
	}
}

func TestRuntimeWritebackBuildsSummaryAndCommits(t *testing.T) {
	runtime := NewRuntime(NewInMemoryStoreWithClock(fixedClock(time.Date(2026, 4, 13, 10, 30, 0, 0, time.UTC))), nil)

	requests, err := runtime.Writeback(context.Background(), Record{
		SessionID: "session-write",
		TurnID:    "turn-1",
		Messages: []core.Message{
			{Role: core.MessageRoleUser, Content: "hello"},
			{Role: core.MessageRoleAssistant, Content: "world"},
		},
	})
	if err != nil {
		t.Fatalf("Writeback() error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(requests))
	}

	if _, err := runtime.CommitWrites(context.Background(), requests); err != nil {
		t.Fatalf("CommitWrites() error = %v", err)
	}

	entries, err := runtime.Store.List(context.Background(), Query{
		Scope:    ScopeSession,
		Metadata: map[string]string{"session_id": "session-write"},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Kind != "summary" {
		t.Fatalf("entry kind = %q, want summary", entries[0].Kind)
	}
	if entries[0].Metadata["turn_id"] != "turn-1" {
		t.Fatalf("turn metadata = %q, want turn-1", entries[0].Metadata["turn_id"])
	}
}

func TestRuntimePlanAndApplyCompact(t *testing.T) {
	clock := fixedClock(time.Date(2026, 4, 13, 11, 0, 0, 0, time.UTC))
	store := NewInMemoryStoreWithClock(clock)
	first, _ := store.Put(context.Background(), WriteRequest{Entry: Entry{
		Scope:    ScopeSession,
		Kind:     "summary",
		Content:  "old-a",
		Metadata: map[string]string{"session_id": "session-compact"},
	}})
	second, _ := store.Put(context.Background(), WriteRequest{Entry: Entry{
		Scope:    ScopeSession,
		Kind:     "summary",
		Content:  "old-b",
		Metadata: map[string]string{"session_id": "session-compact"},
	}})

	runtime := NewRuntime(store, stubPolicy{
		shouldCompact: true,
		compactResult: CompactResult{
			Summary:   "compacted summary",
			RemovedID: []string{first.Entry.ID, second.Entry.ID},
			Retained:  []Entry{{Scope: ScopeSession}},
			Metadata:  map[string]string{"compacted": "true"},
		},
	})

	plan, err := runtime.PlanCompact(context.Background(), Request{
		SessionID:     "session-compact",
		CompactBudget: 1,
		CompactReason: "test",
	})
	if err != nil {
		t.Fatalf("PlanCompact() error = %v", err)
	}
	if plan.Summary != "compacted summary" {
		t.Fatalf("plan summary = %q, want compacted summary", plan.Summary)
	}

	if _, err := runtime.ApplyCompact(context.Background(), plan, map[string]string{"session_id": "session-compact"}); err != nil {
		t.Fatalf("ApplyCompact() error = %v", err)
	}

	entries, err := store.List(context.Background(), Query{
		Scope:    ScopeSession,
		Metadata: map[string]string{"session_id": "session-compact"},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Content != "compacted summary" {
		t.Fatalf("summary = %q, want compacted summary", entries[0].Content)
	}
	if entries[0].Metadata["compacted"] != "true" {
		t.Fatalf("compacted metadata = %q, want true", entries[0].Metadata["compacted"])
	}
}

func TestRuntimeApplyCompactDoesNotDeleteBeforeSummaryWriteSucceeds(t *testing.T) {
	clock := fixedClock(time.Date(2026, 4, 13, 11, 30, 0, 0, time.UTC))
	store := &failingPutStore{
		InMemoryStore: NewInMemoryStoreWithClock(clock),
		failPut:       true,
	}
	first, _ := store.InMemoryStore.Put(context.Background(), WriteRequest{Entry: Entry{
		Scope:    ScopeSession,
		Kind:     "summary",
		Content:  "keep-a",
		Metadata: map[string]string{"session_id": "session-safe"},
	}})
	second, _ := store.InMemoryStore.Put(context.Background(), WriteRequest{Entry: Entry{
		Scope:    ScopeSession,
		Kind:     "summary",
		Content:  "keep-b",
		Metadata: map[string]string{"session_id": "session-safe"},
	}})

	runtime := NewRuntime(store, nil)
	_, err := runtime.ApplyCompact(context.Background(), CompactResult{
		Summary:   "new summary",
		RemovedID: []string{first.Entry.ID, second.Entry.ID},
		Retained:  []Entry{{Scope: ScopeSession}},
	}, map[string]string{"session_id": "session-safe"})
	if err == nil {
		t.Fatal("ApplyCompact() error = nil, want failure")
	}

	entries, listErr := store.List(context.Background(), Query{
		Scope:    ScopeSession,
		Metadata: map[string]string{"session_id": "session-safe"},
	})
	if listErr != nil {
		t.Fatalf("List() error = %v", listErr)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want original 2 entries preserved", len(entries))
	}
}

type failingPutStore struct {
	*InMemoryStore
	failPut bool
}

func (s *failingPutStore) Put(ctx context.Context, request WriteRequest) (WriteResult, error) {
	if s.failPut {
		return WriteResult{}, errors.New("put failed")
	}
	return s.InMemoryStore.Put(ctx, request)
}
