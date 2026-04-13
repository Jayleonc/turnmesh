package feedback

import "time"

// Level indicates the severity of a feedback event.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Event is a structured feedback message emitted by the kernel.
type Event struct {
	Time    time.Time      `json:"time"`
	Level   Level          `json:"level"`
	Kind    string         `json:"kind"`
	Message string         `json:"message,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}
