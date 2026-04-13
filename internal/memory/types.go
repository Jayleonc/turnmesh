package memory

import (
	"context"
	"github.com/Jayleonc/turnmesh/internal/core"
	"time"
)

// Scope identifies the logical memory domain a record belongs to.
type Scope string

const (
	ScopeUnknown   Scope = ""
	ScopeSession   Scope = "session"
	ScopeWorking   Scope = "working"
	ScopeDurable   Scope = "durable"
	ScopeProject   Scope = "project"
	ScopeReference Scope = "reference"
)

// Entry is the minimal memory unit exchanged by the kernel.
type Entry struct {
	ID        string
	Scope     Scope
	Kind      string
	Content   string
	Metadata  map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Query describes a read against the memory store.
type Query struct {
	Scope    Scope
	Kinds    []string
	Limit    int
	Before   time.Time
	After    time.Time
	Text     string
	Metadata map[string]string
}

// WriteRequest carries a memory entry to persist.
type WriteRequest struct {
	Entry Entry
}

// WriteResult reports the stored entry after persistence.
type WriteResult struct {
	Entry Entry
}

// DeleteRequest identifies a record for removal.
type DeleteRequest struct {
	ID string
}

// CompactRequest describes a compaction attempt over a set of entries.
type CompactRequest struct {
	Scope    Scope
	Entries  []Entry
	Budget   int
	Reason   string
	Metadata map[string]string
}

// CompactResult reports the outcome of a compaction policy decision.
type CompactResult struct {
	Summary   string
	Retained  []Entry
	RemovedID []string
	Metadata  map[string]string
}

// Request carries the pure data inputs for a turn-level memory snapshot or
// compaction plan.
type Request struct {
	SessionID     string
	TurnID        string
	Query         Query
	Memory        []Entry
	CompactBudget int
	CompactReason string
	Metadata      map[string]string
}

// Record carries the pure data inputs for turn-level writeback planning.
type Record struct {
	SessionID   string
	TurnID      string
	Transcript  []core.Message
	Messages    []core.Message
	Events      []core.TurnEvent
	ToolResults []core.ToolResult
	Candidates  []Entry
	Metadata    map[string]string
}

// TurnContext and TurnRecord remain as compatibility aliases for older call sites.
type TurnContext = Request
type TurnRecord = Record

// Store abstracts memory persistence without tying the kernel to a backend.
type Store interface {
	Get(ctx context.Context, id string) (Entry, error)
	List(ctx context.Context, query Query) ([]Entry, error)
	Put(ctx context.Context, request WriteRequest) (WriteResult, error)
	Delete(ctx context.Context, request DeleteRequest) error
}

// Policy abstracts read/write/compaction rules for memory handling.
type Policy interface {
	AllowRead(ctx context.Context, entry Entry) bool
	AllowWrite(ctx context.Context, entry Entry) bool
	ShouldCompact(ctx context.Context, entries []Entry) (bool, error)
	PlanCompact(ctx context.Context, request CompactRequest) (CompactResult, error)
}
