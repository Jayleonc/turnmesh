package mcp

import (
	"context"
	"fmt"
	"strings"
)

// MessageError is the JSON-RPC error envelope used by MCP messages.
type MessageError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *MessageError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("mcp error %d", e.Code)
}

// Capability describes a feature exposed by an MCP endpoint.
type Capability struct {
	Name        string
	Description string
	Version     string
	Metadata    map[string]string
}

// InitializeParams carries the minimal handshake metadata for a client.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion,omitempty"`
	ClientInfo      map[string]any `json:"clientInfo,omitempty"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
}

// InitializeResult captures the server handshake response.
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion,omitempty"`
	ServerInfo      map[string]any `json:"serverInfo,omitempty"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
}

// ListToolsResult is the normalized result of tools/list.
type ListToolsResult struct {
	Tools []Tool `json:"tools,omitempty"`
}

// ListResourcesResult is the normalized result of resources/list.
type ListResourcesResult struct {
	Resources []Resource `json:"resources,omitempty"`
}

// ListPromptsResult is the normalized result of prompts/list.
type ListPromptsResult struct {
	Prompts []Prompt `json:"prompts,omitempty"`
}

// Tool describes a callable MCP tool.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Metadata    map[string]string
}

// Resource describes an addressable MCP resource.
type Resource struct {
	URI         string
	Name        string
	Description string
	Metadata    map[string]string
}

// Prompt describes an MCP prompt template.
type Prompt struct {
	Name        string
	Description string
	Template    string
	Metadata    map[string]string
}

// CallRequest is the normalized invocation payload for an MCP capability.
type CallRequest struct {
	Name      string
	Arguments map[string]any
	Metadata  map[string]string
}

// CallResult is the normalized response from an MCP capability.
type CallResult struct {
	Content  any
	Metadata map[string]string
	Error    string
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// CapabilityProvider resolves capabilities without coupling the kernel to any transport.
type CapabilityProvider interface {
	Capabilities(ctx context.Context) ([]Capability, error)
	Tools(ctx context.Context) ([]Tool, error)
	Resources(ctx context.Context) ([]Resource, error)
	Prompts(ctx context.Context) ([]Prompt, error)
	Call(ctx context.Context, request CallRequest) (CallResult, error)
}

const (
	ToolNamePrefix = "mcp"

	MetadataServerNameKey = "mcp.server"
	MetadataToolNameKey   = "mcp.tool"
	MetadataNameKey       = "mcp.name"
)

// BuildToolName returns the fully-qualified MCP tool name.
// The format matches the TS implementation: mcp__<server>__<tool>.
func BuildToolName(serverName, toolName string) string {
	return "mcp__" + serverName + "__" + toolName
}

// ParseToolName splits a fully-qualified MCP name into server and tool parts.
// It follows the TS string utility behavior: `mcp__my__server__tool` becomes
// server=`my` and tool=`server__tool`.
func ParseToolName(name string) (serverName, toolName string, ok bool) {
	parts := strings.Split(name, "__")
	if len(parts) < 2 || parts[0] != ToolNamePrefix || parts[1] == "" {
		return "", "", false
	}
	serverName = parts[1]
	if len(parts) > 2 {
		toolName = strings.Join(parts[2:], "__")
	}
	return serverName, toolName, true
}
