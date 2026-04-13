package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
)

// StdioConfig configures a subprocess-backed JSON-RPC transport.
type StdioConfig struct {
	Command string
	Args    []string
	Dir     string
	Env     []string
	Stderr  io.Writer
}

// StdioTransport starts a subprocess and exchanges JSON messages via stdio.
type StdioTransport struct {
	cfg StdioConfig

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	recvCh    chan Message
	recvErr   error
	opened    bool
	closed    bool
	closeOnce sync.Once
}

// NewStdioTransport creates a stdio transport around a subprocess command.
func NewStdioTransport(cfg StdioConfig) *StdioTransport {
	return &StdioTransport{
		cfg:    cfg,
		recvCh: make(chan Message, 32),
	}
}

// Name identifies the transport.
func (t *StdioTransport) Name() string { return "stdio" }

// Open starts the subprocess and the decoder loop.
func (t *StdioTransport) Open(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.opened {
		return nil
	}
	if t.cfg.Command == "" {
		return errors.New("stdio transport requires a command")
	}

	cmd := exec.CommandContext(ctx, t.cfg.Command, t.cfg.Args...)
	if t.cfg.Dir != "" {
		cmd.Dir = t.cfg.Dir
	}
	if len(t.cfg.Env) != 0 {
		cmd.Env = append(os.Environ(), t.cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	if t.cfg.Stderr != nil {
		cmd.Stderr = t.cfg.Stderr
	} else {
		cmd.Stderr = io.Discard
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}

	t.cmd = cmd
	t.stdin = stdin
	t.opened = true

	go t.readLoop(stdout)
	return nil
}

// Close terminates the subprocess and stops the transport.
func (t *StdioTransport) Close(ctx context.Context) error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.closed = true
		if t.stdin != nil {
			_ = t.stdin.Close()
		}
		cmd := t.cmd
		t.mu.Unlock()

		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	return t.recvErr
}

// Send writes a single JSON-RPC envelope to the subprocess stdin.
func (t *StdioTransport) Send(ctx context.Context, message Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.opened || t.closed {
		return errors.New("stdio transport is closed")
	}
	encoder := json.NewEncoder(t.stdin)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(message)
}

// Recv blocks until the subprocess emits a JSON message or the transport closes.
func (t *StdioTransport) Recv(ctx context.Context) (Message, error) {
	select {
	case message, ok := <-t.recvCh:
		if !ok {
			if t.recvErr != nil {
				return Message{}, t.recvErr
			}
			return Message{}, io.EOF
		}
		return message, nil
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}

func (t *StdioTransport) readLoop(stdout io.ReadCloser) {
	defer func() {
		_ = stdout.Close()
		close(t.recvCh)
	}()

	decoder := json.NewDecoder(stdout)
	for {
		var message Message
		if err := decoder.Decode(&message); err != nil {
			if !errors.Is(err, io.EOF) {
				t.recvErr = err
			}
			return
		}
		t.mu.Lock()
		closed := t.closed
		t.mu.Unlock()
		if closed {
			return
		}
		t.recvCh <- message
	}
}
