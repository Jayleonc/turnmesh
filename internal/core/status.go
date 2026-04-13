package core

type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

type TurnStatus string

const (
	TurnStatusPending       TurnStatus = "pending"
	TurnStatusRunning       TurnStatus = "running"
	TurnStatusWaiting       TurnStatus = "waiting"
	TurnStatusCompleted     TurnStatus = "completed"
	TurnStatusFailed        TurnStatus = "failed"
	TurnStatusCancelled     TurnStatus = "cancelled"
	TurnStatusInterrupted   TurnStatus = "interrupted"
	TurnStatusNeedsApproval TurnStatus = "needs_approval"
	TurnStatusNeedsRetry    TurnStatus = "needs_retry"
	TurnStatusModelFallback TurnStatus = "model_fallback"
	TurnStatusPromptTooLong TurnStatus = "prompt_too_long"
)

type ToolStatus string

const (
	ToolStatusQueued    ToolStatus = "queued"
	ToolStatusRunning   ToolStatus = "running"
	ToolStatusSucceeded ToolStatus = "succeeded"
	ToolStatusFailed    ToolStatus = "failed"
	ToolStatusCancelled ToolStatus = "cancelled"
	ToolStatusSkipped   ToolStatus = "skipped"
)

type ApprovalState string

const (
	ApprovalStateRequested ApprovalState = "requested"
	ApprovalStateAllowed   ApprovalState = "allowed"
	ApprovalStateDenied    ApprovalState = "denied"
	ApprovalStateDeferred  ApprovalState = "deferred"
	ApprovalStateExpired   ApprovalState = "expired"
)

type MemoryScope string

const (
	MemoryScopeWorking    MemoryScope = "working"
	MemoryScopeSession    MemoryScope = "session"
	MemoryScopeProject    MemoryScope = "project"
	MemoryScopePersistent MemoryScope = "persistent"
	MemoryScopeReference  MemoryScope = "reference"
)

type MemoryKind string

const (
	MemoryKindFact        MemoryKind = "fact"
	MemoryKindPref        MemoryKind = "preference"
	MemoryKindSummary     MemoryKind = "summary"
	MemoryKindObservation MemoryKind = "observation"
	MemoryKindPlan        MemoryKind = "plan"
)

type TaskKind string

const (
	TaskKindTurn        TaskKind = "turn"
	TaskKindBackground  TaskKind = "background"
	TaskKindSubagent    TaskKind = "subagent"
	TaskKindMaintenance TaskKind = "maintenance"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusBlocked   TaskStatus = "blocked"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
	TaskStatusPaused    TaskStatus = "paused"
)

type ApprovalOutcome string

const (
	ApprovalOutcomeAllow ApprovalOutcome = "allow"
	ApprovalOutcomeDeny  ApprovalOutcome = "deny"
	ApprovalOutcomeAsk   ApprovalOutcome = "ask"
)
