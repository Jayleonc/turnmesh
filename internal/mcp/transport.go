package mcp

import "context"

// Message is the transport-agnostic envelope used by MCP adapters.
type Message struct {
	JSONRPC  string            `json:"jsonrpc,omitempty"`
	ID       any               `json:"id,omitempty"`
	Method   string            `json:"method,omitempty"`
	Params   map[string]any    `json:"params,omitempty"`
	Result   any               `json:"result,omitempty"`
	Error    *MessageError     `json:"error,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Transport abstracts how MCP messages move between processes or runtimes.
type Transport interface {
	Name() string
	Open(ctx context.Context) error
	Close(ctx context.Context) error
	Send(ctx context.Context, message Message) error
	Recv(ctx context.Context) (Message, error)
}
