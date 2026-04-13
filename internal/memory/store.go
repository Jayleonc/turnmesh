package memory

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

func cloneEntry(entry Entry) Entry {
	cloned := entry
	cloned.Metadata = cloneMetadata(entry.Metadata)
	return cloned
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return map[string]string{}
	}

	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

func normalizeEntry(entry Entry, now time.Time, existing *Entry) Entry {
	normalized := cloneEntry(entry)
	if normalized.ID == "" {
		if existing != nil && existing.ID != "" {
			normalized.ID = existing.ID
		} else {
			normalized.ID = newID()
		}
	}

	if normalized.CreatedAt.IsZero() {
		if existing != nil && !existing.CreatedAt.IsZero() {
			normalized.CreatedAt = existing.CreatedAt
		} else {
			normalized.CreatedAt = now
		}
	}

	normalized.UpdatedAt = now
	if entry.Metadata == nil && existing != nil {
		normalized.Metadata = cloneMetadata(existing.Metadata)
	}
	return normalized
}

func entryTimestamp(entry Entry) time.Time {
	if !entry.CreatedAt.IsZero() {
		return entry.CreatedAt
	}
	return entry.UpdatedAt
}

func matchesQuery(entry Entry, query Query) bool {
	if query.Scope != ScopeUnknown && entry.Scope != query.Scope {
		return false
	}

	if len(query.Kinds) > 0 && !containsString(query.Kinds, entry.Kind) {
		return false
	}

	moment := entryTimestamp(entry)
	if !query.Before.IsZero() && !moment.Before(query.Before) {
		return false
	}
	if !query.After.IsZero() && !moment.After(query.After) {
		return false
	}

	if query.Text != "" {
		if !containsText(entry, query.Text) {
			return false
		}
	}

	if len(query.Metadata) > 0 {
		for key, expected := range query.Metadata {
			if entry.Metadata[key] != expected {
				return false
			}
		}
	}

	return true
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func containsText(entry Entry, text string) bool {
	needle := strings.ToLower(text)
	if strings.Contains(strings.ToLower(entry.ID), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Kind), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Content), needle) {
		return true
	}
	for key, value := range entry.Metadata {
		if strings.Contains(strings.ToLower(key), needle) || strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left := entryTimestamp(entries[i])
		right := entryTimestamp(entries[j])
		if left.Equal(right) {
			return entries[i].ID < entries[j].ID
		}
		return left.After(right)
	})
}

func applyLimit(entries []Entry, limit int) []Entry {
	if limit <= 0 || len(entries) <= limit {
		return entries
	}
	return entries[:limit]
}

func newID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return time.Now().UTC().Format("20060102150405.000000000")
}
