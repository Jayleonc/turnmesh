package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestInMemoryStoreCRUD(t *testing.T) {
	clock := fixedClock(time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC))
	store := NewInMemoryStoreWithClock(clock)

	result, err := store.Put(context.Background(), WriteRequest{
		Entry: Entry{
			Scope:   ScopeSession,
			Kind:    "note",
			Content: "hello world",
			Metadata: map[string]string{
				"source": "test",
			},
		},
	})
	if err != nil {
		t.Fatalf("put failed: %v", err)
	}
	if result.Entry.ID == "" {
		t.Fatalf("expected generated id")
	}
	if result.Entry.CreatedAt.IsZero() || result.Entry.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be set")
	}

	got, err := store.Get(context.Background(), result.Entry.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Metadata["source"] != "test" {
		t.Fatalf("expected metadata to survive round trip")
	}

	list, err := store.List(context.Background(), Query{
		Scope: ScopeSession,
		Text:  "hello",
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}

	err = store.Delete(context.Background(), DeleteRequest{ID: result.Entry.ID})
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err = store.Get(context.Background(), result.Entry.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestInMemoryStoreQueryFilters(t *testing.T) {
	clock := fixedClock(time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC))
	store := NewInMemoryStoreWithClock(clock)

	_, _ = store.Put(context.Background(), WriteRequest{Entry: Entry{Scope: ScopeProject, Kind: "summary", Content: "alpha", Metadata: map[string]string{"team": "core"}}})
	_, _ = store.Put(context.Background(), WriteRequest{Entry: Entry{Scope: ScopeProject, Kind: "summary", Content: "beta", Metadata: map[string]string{"team": "edge"}}})
	_, _ = store.Put(context.Background(), WriteRequest{Entry: Entry{Scope: ScopeSession, Kind: "chat", Content: "gamma", Metadata: map[string]string{"team": "core"}}})

	list, err := store.List(context.Background(), Query{
		Scope:    ScopeProject,
		Kinds:    []string{"summary"},
		Limit:    1,
		Metadata: map[string]string{"team": "core"},
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}
	if list[0].Content != "alpha" {
		t.Fatalf("expected alpha, got %q", list[0].Content)
	}
}

func TestInMemoryStoreConcurrentWrites(t *testing.T) {
	store := NewInMemoryStore()
	const count = 32

	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := store.Put(context.Background(), WriteRequest{
				Entry: Entry{
					Scope:   ScopeWorking,
					Kind:    "item",
					Content: "value",
					Metadata: map[string]string{
						"index": "x",
					},
				},
			})
			if err != nil {
				t.Errorf("put failed: %v", err)
			}
		}(i)
	}
	wg.Wait()

	list, err := store.List(context.Background(), Query{Scope: ScopeWorking})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != count {
		t.Fatalf("expected %d entries, got %d", count, len(list))
	}
}

func TestFileStorePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")

	store, err := NewFileStoreWithClock(path, fixedClock(time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("new file store failed: %v", err)
	}

	first, err := store.Put(context.Background(), WriteRequest{
		Entry: Entry{
			Scope:   ScopeDurable,
			Kind:    "fact",
			Content: "persist me",
			Metadata: map[string]string{
				"origin": "disk",
			},
		},
	})
	if err != nil {
		t.Fatalf("put failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}

	got, err := reopened.Get(context.Background(), first.Entry.ID)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Metadata["origin"] != "disk" {
		t.Fatalf("expected metadata to survive persistence")
	}

	list, err := reopened.List(context.Background(), Query{Text: "persist"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(list))
	}

	if err := reopened.Delete(context.Background(), DeleteRequest{ID: first.Entry.ID}); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	list, err = reopened.List(context.Background(), Query{})
	if err != nil {
		t.Fatalf("list after delete failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty store, got %d entries", len(list))
	}
}

func TestFileStoreConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("new file store failed: %v", err)
	}

	const count = 16
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := store.Put(context.Background(), WriteRequest{
				Entry: Entry{
					Scope:   ScopeProject,
					Kind:    "item",
					Content: "payload",
					Metadata: map[string]string{
						"index": "x",
					},
				},
			})
			if err != nil {
				t.Errorf("put failed: %v", err)
			}
		}(i)
	}
	wg.Wait()

	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}

	list, err := reopened.List(context.Background(), Query{Scope: ScopeProject})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != count {
		t.Fatalf("expected %d entries, got %d", count, len(list))
	}
}

func TestFileStoreLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	_, err := NewFileStore(path)
	if !errors.Is(err, ErrCorruptFile) {
		t.Fatalf("expected corrupt file error, got %v", err)
	}
}

func fixedClock(ts time.Time) func() time.Time {
	return func() time.Time {
		return ts
	}
}
