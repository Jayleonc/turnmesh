package memory

import (
	"context"
	"sync"
	"time"
)

type InMemoryStore struct {
	mu      sync.RWMutex
	entries map[string]Entry
	clock   func() time.Time
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		entries: make(map[string]Entry),
		clock:   func() time.Time { return time.Now().UTC() },
	}
}

func NewInMemoryStoreWithClock(clock func() time.Time) *InMemoryStore {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}

	return &InMemoryStore{
		entries: make(map[string]Entry),
		clock:   clock,
	}
}

func (s *InMemoryStore) Get(ctx context.Context, id string) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	if id == "" {
		return Entry{}, ErrInvalidID
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[id]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return cloneEntry(entry), nil
}

func (s *InMemoryStore) List(ctx context.Context, query Query) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if matchesQuery(entry, query) {
			entries = append(entries, cloneEntry(entry))
		}
	}

	sortEntries(entries)
	return applyLimit(entries, query.Limit), nil
}

func (s *InMemoryStore) Put(ctx context.Context, request WriteRequest) (WriteResult, error) {
	if err := ctx.Err(); err != nil {
		return WriteResult{}, err
	}

	now := s.clock()
	s.mu.Lock()
	defer s.mu.Unlock()

	var existing *Entry
	if request.Entry.ID != "" {
		if current, ok := s.entries[request.Entry.ID]; ok {
			existing = &current
		}
	}

	entry := normalizeEntry(request.Entry, now, existing)
	s.entries[entry.ID] = entry
	return WriteResult{Entry: cloneEntry(entry)}, nil
}

func (s *InMemoryStore) Delete(ctx context.Context, request DeleteRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if request.ID == "" {
		return ErrInvalidID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.entries[request.ID]; !ok {
		return ErrNotFound
	}

	delete(s.entries, request.ID)
	return nil
}
