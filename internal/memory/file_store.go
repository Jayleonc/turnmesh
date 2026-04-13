package memory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type FileStore struct {
	mu      sync.RWMutex
	path    string
	entries map[string]Entry
	clock   func() time.Time
}

func NewFileStore(path string) (*FileStore, error) {
	return NewFileStoreWithClock(path, nil)
}

func NewFileStoreWithClock(path string, clock func() time.Time) (*FileStore, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}

	store := &FileStore{
		path:    path,
		entries: make(map[string]Entry),
		clock:   clock,
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileStore) Get(ctx context.Context, id string) (Entry, error) {
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

func (s *FileStore) List(ctx context.Context, query Query) ([]Entry, error) {
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

func (s *FileStore) Put(ctx context.Context, request WriteRequest) (WriteResult, error) {
	if err := ctx.Err(); err != nil {
		return WriteResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock()
	var existing *Entry
	if request.Entry.ID != "" {
		if current, ok := s.entries[request.Entry.ID]; ok {
			existing = &current
		}
	}

	entry := normalizeEntry(request.Entry, now, existing)
	s.entries[entry.ID] = entry
	if err := s.persistLocked(); err != nil {
		return WriteResult{}, err
	}

	return WriteResult{Entry: cloneEntry(entry)}, nil
}

func (s *FileStore) Delete(ctx context.Context, request DeleteRequest) error {
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
	return s.persistLocked()
}

func (s *FileStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}

	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return errors.Join(ErrCorruptFile, err)
	}

	for _, entry := range entries {
		if entry.ID == "" {
			return ErrCorruptFile
		}
		s.entries[entry.ID] = cloneEntry(entry)
	}
	return nil
}

func (s *FileStore) persistLocked() error {
	entries := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		entries = append(entries, cloneEntry(entry))
	}

	sortEntries(entries)
	payload, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}
