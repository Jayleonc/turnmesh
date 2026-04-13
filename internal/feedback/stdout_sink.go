package feedback

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// StdoutSink emits feedback events as JSON lines.
type StdoutSink struct {
	mu  sync.Mutex
	out io.Writer
	now func() time.Time
}

// NewStdoutSink creates a sink that writes structured JSON to the provided writer.
func NewStdoutSink(out io.Writer) *StdoutSink {
	if out == nil {
		out = os.Stdout
	}

	return &StdoutSink{
		out: out,
		now: time.Now,
	}
}

// Emit writes one JSON event per line.
func (s *StdoutSink) Emit(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if event.Time.IsZero() {
		event.Time = s.now()
	}
	if event.Data == nil {
		event.Data = map[string]any{}
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.out.Write(append(payload, '\n'))
	return err
}
