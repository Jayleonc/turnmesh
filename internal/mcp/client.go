package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var ErrClientClosed = errors.New("mcp client closed")

// Client is the minimal JSON-RPC capability client for MCP transports.
type Client struct {
	transport Transport

	readerOnce sync.Once
	closeOnce  sync.Once
	notifyOnce sync.Once

	mu      sync.Mutex
	pending map[string]chan Message
	nextID  atomic.Int64
	closed  bool

	notifications chan Message

	initParams *InitializeParams

	stateMu     sync.RWMutex
	initResult  InitializeResult
	initialized bool
}

// NewClient creates a client around a transport.
func NewClient(transport Transport, opts ...ClientOption) *Client {
	client := &Client{
		transport:     transport,
		pending:       make(map[string]chan Message),
		notifications: make(chan Message, 32),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client
}

// WithInitialize configures the client to perform an initialize handshake on Open.
func WithInitialize(params InitializeParams) ClientOption {
	return func(client *Client) {
		copy := params
		client.initParams = &copy
	}
}

// Open opens the underlying transport and optionally performs initialize.
func (c *Client) Open(ctx context.Context) error {
	if err := c.transport.Open(ctx); err != nil {
		return err
	}
	c.startReader()
	if c.initParams != nil {
		if _, err := c.Initialize(ctx, *c.initParams); err != nil {
			_ = c.transport.Close(context.Background())
			return err
		}
		if err := c.Notify(ctx, "initialized", nil); err != nil {
			_ = c.transport.Close(context.Background())
			return err
		}
	}
	return nil
}

// Close closes the client and transport.
func (c *Client) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		for id, ch := range c.pending {
			delete(c.pending, id)
			close(ch)
		}
		c.mu.Unlock()
		c.closeNotifications()
	})
	return c.transport.Close(ctx)
}

// Messages returns server-initiated messages and notifications.
func (c *Client) Messages() <-chan Message {
	return c.notifications
}

// Initialize performs the JSON-RPC initialize handshake.
func (c *Client) Initialize(ctx context.Context, params InitializeParams) (InitializeResult, error) {
	var result InitializeResult
	if err := c.request(ctx, "initialize", params, &result); err != nil {
		return InitializeResult{}, err
	}
	c.stateMu.Lock()
	c.initResult = result
	c.initialized = true
	c.stateMu.Unlock()
	return result, nil
}

// Capabilities exposes the last initialize handshake as normalized capabilities.
func (c *Client) Capabilities(ctx context.Context) ([]Capability, error) {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	if len(c.initResult.Capabilities) == 0 {
		return nil, nil
	}
	caps := make([]Capability, 0, len(c.initResult.Capabilities))
	for name, raw := range c.initResult.Capabilities {
		caps = append(caps, Capability{
			Name:     name,
			Metadata: map[string]string{"raw": fmt.Sprint(raw)},
		})
	}
	return caps, nil
}

// Tools lists server tools.
func (c *Client) Tools(ctx context.Context) ([]Tool, error) {
	var result ListToolsResult
	if err := c.request(ctx, "tools/list", nil, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

// Resources lists server resources.
func (c *Client) Resources(ctx context.Context) ([]Resource, error) {
	var result ListResourcesResult
	if err := c.request(ctx, "resources/list", nil, &result); err != nil {
		return nil, err
	}
	return result.Resources, nil
}

// Prompts lists server prompts.
func (c *Client) Prompts(ctx context.Context) ([]Prompt, error) {
	var result ListPromptsResult
	if err := c.request(ctx, "prompts/list", nil, &result); err != nil {
		return nil, err
	}
	return result.Prompts, nil
}

// Call invokes a single tool/capability.
func (c *Client) Call(ctx context.Context, request CallRequest) (CallResult, error) {
	var result CallResult
	params := map[string]any{
		"name":      request.Name,
		"arguments": request.Arguments,
	}
	if len(request.Metadata) != 0 {
		params["metadata"] = request.Metadata
	}
	if err := c.request(ctx, "tools/call", params, &result); err != nil {
		return CallResult{}, err
	}
	return result, nil
}

// ListTools is a convenience wrapper around Tools.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	return c.Tools(ctx)
}

// CallTool is a convenience wrapper around Call.
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (CallResult, error) {
	return c.Call(ctx, CallRequest{Name: name, Arguments: arguments})
}

// Notify sends a JSON-RPC notification.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	message := Message{
		JSONRPC: "2.0",
		Method:  method,
	}
	if params != nil {
		normalized, err := normalizeParams(params)
		if err != nil {
			return err
		}
		message.Params = normalized
	}
	return c.transport.Send(ctx, message)
}

func (c *Client) request(ctx context.Context, method string, params any, out any) error {
	c.startReader()

	id := c.nextID.Add(1)
	key := requestKey(id)
	reply := make(chan Message, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClientClosed
	}
	c.pending[key] = reply
	c.mu.Unlock()

	message := Message{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}
	if params != nil {
		normalized, err := normalizeParams(params)
		if err != nil {
			c.removePending(key)
			return err
		}
		message.Params = normalized
	}

	if err := c.transport.Send(ctx, message); err != nil {
		c.removePending(key)
		return err
	}

	select {
	case response, ok := <-reply:
		if !ok {
			return ErrClientClosed
		}
		if response.Error != nil {
			return response.Error
		}
		if out != nil {
			if err := decodeInto(response.Result, out); err != nil {
				return err
			}
		}
		return nil
	case <-ctx.Done():
		c.removePending(key)
		return ctx.Err()
	}
}

func (c *Client) startReader() {
	c.readerOnce.Do(func() {
		go c.readLoop()
	})
}

func (c *Client) readLoop() {
	for {
		message, err := c.transport.Recv(context.Background())
		if err != nil {
			c.failPending(err)
			return
		}

		key, ok := messageKey(message.ID)
		if ok {
			c.mu.Lock()
			closed := c.closed
			reply, found := c.pending[key]
			if found {
				delete(c.pending, key)
			}
			c.mu.Unlock()
			if closed {
				return
			}
			if found {
				if safeSendMessage(reply, message) {
					close(reply)
				}
				continue
			}
		}

		c.publish(message)
	}
}

func (c *Client) failPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	for id, ch := range c.pending {
		delete(c.pending, id)
		close(ch)
	}
	c.closeNotifications()
}

func (c *Client) removePending(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if reply, ok := c.pending[key]; ok {
		delete(c.pending, key)
		_ = reply
	}
}

func (c *Client) closeNotifications() {
	c.notifyOnce.Do(func() {
		close(c.notifications)
	})
}

func (c *Client) publish(message Message) {
	defer func() {
		_ = recover()
	}()
	select {
	case c.notifications <- message:
	default:
	}
}

func safeSendMessage(ch chan Message, message Message) bool {
	defer func() {
		_ = recover()
	}()
	ch <- message
	return true
}

func normalizeParams(params any) (map[string]any, error) {
	switch typed := params.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return typed, nil
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, nil
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

func decodeInto(value any, out any) error {
	if out == nil || value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func requestKey(id any) string {
	if id == nil {
		return ""
	}
	data, err := json.Marshal(id)
	if err != nil {
		return fmt.Sprint(id)
	}
	return string(data)
}

func messageKey(id any) (string, bool) {
	key := requestKey(id)
	return key, key != ""
}
