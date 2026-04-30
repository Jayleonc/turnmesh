package core

import (
	"encoding/json"
	"time"
)

type Message struct {
	ID        string            `json:"id,omitempty"`
	Role      MessageRole       `json:"role"`
	Content   string            `json:"content,omitempty"`
	Parts     []MessagePart     `json:"parts,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type MessagePartType string

const (
	MessagePartText       MessagePartType = "text"
	MessagePartImage      MessagePartType = "image"
	MessagePartToolCall   MessagePartType = "tool_call"
	MessagePartToolResult MessagePartType = "tool_result"
	MessagePartFile       MessagePartType = "file"
)

type MessagePart struct {
	Type       MessagePartType   `json:"type"`
	Text       string            `json:"text,omitempty"`
	Name       string            `json:"name,omitempty"`
	MimeType   string            `json:"mime_type,omitempty"`
	Data       []byte            `json:"data,omitempty"`
	URL        string            `json:"url,omitempty"`
	Detail     string            `json:"detail,omitempty"`
	ToolCall   *ToolInvocation   `json:"tool_call,omitempty"`
	ToolResult *ToolResult       `json:"tool_result,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type TurnInput struct {
	SessionID    string            `json:"session_id,omitempty"`
	TurnID       string            `json:"turn_id,omitempty"`
	Messages     []Message         `json:"messages,omitempty"`
	Tools        []ToolSpec        `json:"tools,omitempty"`
	Memory       []MemoryEntry     `json:"memory,omitempty"`
	Tasks        []TaskState       `json:"tasks,omitempty"`
	Approvals    []ApprovalRequest `json:"approvals,omitempty"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type TurnEventKind string

const (
	TurnEventStarted          TurnEventKind = "started"
	TurnEventMessage          TurnEventKind = "message"
	TurnEventCitation         TurnEventKind = "citation"
	TurnEventClarification    TurnEventKind = "clarification"
	TurnEventDelta            TurnEventKind = "delta"
	TurnEventToolCall         TurnEventKind = "tool_call"
	TurnEventToolResult       TurnEventKind = "tool_result"
	TurnEventApprovalRequest  TurnEventKind = "approval_request"
	TurnEventApprovalDecision TurnEventKind = "approval_decision"
	TurnEventMemoryRead       TurnEventKind = "memory_read"
	TurnEventMemoryWrite      TurnEventKind = "memory_write"
	TurnEventTaskStarted      TurnEventKind = "task_started"
	TurnEventTaskUpdated      TurnEventKind = "task_updated"
	TurnEventTaskFinished     TurnEventKind = "task_finished"
	TurnEventStatusChanged    TurnEventKind = "status_changed"
	TurnEventCompleted        TurnEventKind = "completed"
	TurnEventError            TurnEventKind = "error"
)

type TurnEvent struct {
	ID         string            `json:"id,omitempty"`
	TurnID     string            `json:"turn_id,omitempty"`
	Kind       TurnEventKind     `json:"kind"`
	Sequence   int64             `json:"sequence,omitempty"`
	Timestamp  time.Time         `json:"timestamp,omitempty"`
	Status     TurnStatus        `json:"status,omitempty"`
	Message    *Message          `json:"message,omitempty"`
	Payload    json.RawMessage   `json:"payload,omitempty"`
	ToolCall   *ToolInvocation   `json:"tool_call,omitempty"`
	ToolResult *ToolResult       `json:"tool_result,omitempty"`
	Approval   *ApprovalRequest  `json:"approval,omitempty"`
	Memory     *MemoryEntry      `json:"memory,omitempty"`
	Task       *TaskState        `json:"task,omitempty"`
	Error      *Error            `json:"error,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type ToolSpec struct {
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	InputSchema     json.RawMessage   `json:"input_schema,omitempty"`
	OutputSchema    json.RawMessage   `json:"output_schema,omitempty"`
	ReadOnly        bool              `json:"read_only,omitempty"`
	ConcurrencySafe bool              `json:"concurrency_safe,omitempty"`
	Timeout         time.Duration     `json:"timeout,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type ToolInvocation struct {
	ID         string            `json:"id,omitempty"`
	Tool       string            `json:"tool"`
	Input      json.RawMessage   `json:"input,omitempty"`
	Arguments  json.RawMessage   `json:"arguments,omitempty"`
	Caller     string            `json:"caller,omitempty"`
	ApprovalID string            `json:"approval_id,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type ToolResult struct {
	InvocationID string            `json:"invocation_id,omitempty"`
	Tool         string            `json:"tool"`
	Status       ToolStatus        `json:"status"`
	Output       string            `json:"output,omitempty"`
	Structured   json.RawMessage   `json:"structured,omitempty"`
	Error        *Error            `json:"error,omitempty"`
	Duration     time.Duration     `json:"duration,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type ApprovalRequest struct {
	ID                string            `json:"id,omitempty"`
	Subject           string            `json:"subject,omitempty"`
	Reason            string            `json:"reason,omitempty"`
	RequestedBy       string            `json:"requested_by,omitempty"`
	RequestedAt       time.Time         `json:"requested_at,omitempty"`
	State             ApprovalState     `json:"state,omitempty"`
	SuggestedDecision ApprovalOutcome   `json:"suggested_decision,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

type ApprovalDecision struct {
	RequestID string            `json:"request_id,omitempty"`
	Outcome   ApprovalOutcome   `json:"outcome"`
	Reason    string            `json:"reason,omitempty"`
	DecidedAt time.Time         `json:"decided_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type MemoryEntry struct {
	ID         string            `json:"id,omitempty"`
	Scope      MemoryScope       `json:"scope"`
	Kind       MemoryKind        `json:"kind"`
	Title      string            `json:"title,omitempty"`
	Text       string            `json:"text,omitempty"`
	Source     string            `json:"source,omitempty"`
	Confidence float64           `json:"confidence,omitempty"`
	CreatedAt  time.Time         `json:"created_at,omitempty"`
	UpdatedAt  time.Time         `json:"updated_at,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type TaskState struct {
	ID        string            `json:"id,omitempty"`
	ParentID  string            `json:"parent_id,omitempty"`
	Name      string            `json:"name,omitempty"`
	Kind      TaskKind          `json:"kind"`
	Status    TaskStatus        `json:"status"`
	Progress  float64           `json:"progress,omitempty"`
	Summary   string            `json:"summary,omitempty"`
	Error     *Error            `json:"error,omitempty"`
	StartedAt time.Time         `json:"started_at,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type AgentDefinition struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Tools        []string          `json:"tools,omitempty"`
	MemoryScope  MemoryScope       `json:"memory_scope,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}
