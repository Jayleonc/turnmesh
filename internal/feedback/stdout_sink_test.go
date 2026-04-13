package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestStdoutSinkEmitsStructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	sink := NewStdoutSink(&buf)

	err := sink.Emit(context.Background(), Event{
		Time:    time.Unix(123, 0).UTC(),
		Level:   LevelInfo,
		Kind:    "tool.result",
		Message: "done",
		Data: map[string]any{
			"exit_code": 0,
		},
	})
	if err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	var got Event
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Kind != "tool.result" {
		t.Fatalf("Kind = %q, want %q", got.Kind, "tool.result")
	}
	if got.Level != LevelInfo {
		t.Fatalf("Level = %q, want %q", got.Level, LevelInfo)
	}
	if got.Message != "done" {
		t.Fatalf("Message = %q, want %q", got.Message, "done")
	}
}
