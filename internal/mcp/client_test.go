package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type stubTransport struct {
	mu     sync.Mutex
	opened bool
	closed bool
	sent   []Message
	recvCh chan Message
	errCh  chan error
}

func newStubTransport() *stubTransport {
	return &stubTransport{
		recvCh: make(chan Message, 16),
		errCh:  make(chan error, 1),
	}
}

func (t *stubTransport) Name() string { return "stub" }

func (t *stubTransport) Open(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.opened = true
	return nil
}

func (t *stubTransport) Close(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.recvCh)
	}
	return nil
}

func (t *stubTransport) Send(_ context.Context, message Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrClientClosed
	}
	t.sent = append(t.sent, message)
	return nil
}

func (t *stubTransport) Recv(ctx context.Context) (Message, error) {
	select {
	case message, ok := <-t.recvCh:
		if !ok {
			return Message{}, ioErrClosed()
		}
		return message, nil
	case err := <-t.errCh:
		if err != nil {
			return Message{}, err
		}
		return Message{}, ioErrClosed()
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}

func ioErrClosed() error {
	return errors.New("stub transport closed")
}

func TestClientCorrelatesResponses(t *testing.T) {
	ctx := context.Background()
	tr := newStubTransport()
	client := NewClient(tr)

	if err := client.Open(ctx); err != nil {
		t.Fatalf("open client: %v", err)
	}
	defer client.Close(ctx)

	listDone := make(chan error, 1)
	callDone := make(chan error, 1)

	go func() {
		_, err := client.Tools(ctx)
		listDone <- err
	}()
	go func() {
		_, err := client.Call(ctx, CallRequest{Name: "ping", Arguments: map[string]any{"value": "ok"}})
		callDone <- err
	}()

	waitForSends := func(n int) []Message {
		t.Helper()
		for i := 0; i < 1000; i++ {
			tr.mu.Lock()
			if len(tr.sent) >= n {
				out := append([]Message(nil), tr.sent[:n]...)
				tr.mu.Unlock()
				return out
			}
			tr.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("expected %d sends, got %d", n, len(tr.sent))
		return nil
	}

	sent := waitForSends(2)
	if sent[0].Method != "tools/list" && sent[1].Method != "tools/list" {
		t.Fatalf("expected tools/list request, got %#v", sent)
	}
	if sent[0].Method != "tools/call" && sent[1].Method != "tools/call" {
		t.Fatalf("expected tools/call request, got %#v", sent)
	}

	tr.recvCh <- Message{
		JSONRPC: "2.0",
		ID:      sent[1].ID,
		Result:  CallResult{Content: "pong"},
	}
	tr.recvCh <- Message{
		JSONRPC: "2.0",
		ID:      sent[0].ID,
		Result:  ListToolsResult{Tools: []Tool{{Name: "ping"}}},
	}

	if err := <-listDone; err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if err := <-callDone; err != nil {
		t.Fatalf("tools/call: %v", err)
	}
}

func TestClientInitializeAndCapabilities(t *testing.T) {
	ctx := context.Background()
	tr := newStubTransport()
	client := NewClient(tr)

	if err := client.Open(ctx); err != nil {
		t.Fatalf("open client: %v", err)
	}
	defer client.Close(ctx)

	done := make(chan struct{})
	go func() {
		_, _ = client.Initialize(ctx, InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo: map[string]any{
				"name":    "test",
				"version": "0.1.0",
			},
			Capabilities: map[string]any{
				"tools": map[string]any{"listChanged": true},
			},
		})
		close(done)
	}()

	sent := waitForStubSend(t, tr, 1)
	tr.recvCh <- Message{
		JSONRPC: "2.0",
		ID:      sent[0].ID,
		Result: InitializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities: map[string]any{
				"tools": map[string]any{"listChanged": true},
			},
		},
	}

	<-done

	caps, err := client.Capabilities(ctx)
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if len(caps) != 1 || caps[0].Name != "tools" {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
}

func waitForStubSend(t *testing.T, tr *stubTransport, n int) []Message {
	t.Helper()
	for i := 0; i < 1000; i++ {
		tr.mu.Lock()
		if len(tr.sent) >= n {
			out := append([]Message(nil), tr.sent[:n]...)
			tr.mu.Unlock()
			return out
		}
		tr.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("expected %d sends, got %d", n, len(tr.sent))
	return nil
}
