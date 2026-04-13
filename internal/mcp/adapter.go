package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Jayleonc/turnmesh/internal/core"
)

var ErrInvalidMcpToolName = errors.New("invalid mcp tool name")

// ToolAdapter converts MCP capability-provider results into core tool objects.
// It stays transport- and orchestrator-agnostic so it can be reused by any
// higher-level runtime.
type ToolAdapter struct {
	serverName string
	provider   CapabilityProvider
}

// NewToolAdapter builds a reusable adapter for one MCP server namespace.
func NewToolAdapter(serverName string, provider CapabilityProvider) (*ToolAdapter, error) {
	if serverName == "" {
		return nil, errors.New("mcp tool adapter requires a server name")
	}
	if provider == nil {
		return nil, errors.New("mcp tool adapter requires a capability provider")
	}
	return &ToolAdapter{
		serverName: serverName,
		provider:   provider,
	}, nil
}

// ListTools resolves MCP tools and rewrites them into core.ToolSpec entries.
func (a *ToolAdapter) ListTools(ctx context.Context) ([]core.ToolSpec, error) {
	if a == nil || a.provider == nil {
		return nil, errors.New("mcp tool adapter is not configured")
	}

	tools, err := a.provider.Tools(ctx)
	if err != nil {
		return nil, err
	}

	specs := make([]core.ToolSpec, 0, len(tools))
	for _, tool := range tools {
		spec, err := toolToSpec(a.serverName, tool)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// Invoke converts a core.ToolInvocation into an MCP tools/call request.
func (a *ToolAdapter) Invoke(ctx context.Context, invocation core.ToolInvocation) (core.ToolResult, error) {
	start := time.Now()
	toolName, callRequest, fqName, err := a.invocationToCallRequest(invocation)
	if err != nil {
		return failedToolResult(invocation, fqName, time.Since(start), err), err
	}

	result, callErr := a.provider.Call(ctx, callRequest)
	if callErr != nil {
		return failedToolResult(invocation, fqName, time.Since(start), callErr), callErr
	}

	return toolResultFromCallResult(invocation, fqName, result, time.Since(start), toolName), nil
}

func (a *ToolAdapter) invocationToCallRequest(invocation core.ToolInvocation) (toolName string, request CallRequest, fqName string, err error) {
	if a == nil || a.provider == nil {
		return "", CallRequest{}, "", errors.New("mcp tool adapter is not configured")
	}
	if invocation.Tool == "" {
		return "", CallRequest{}, "", errors.New("tool invocation is missing a tool name")
	}

	toolName = invocation.Tool
	fqName = BuildToolName(a.serverName, toolName)
	if serverName, parsedToolName, ok := ParseToolName(invocation.Tool); ok {
		if serverName != a.serverName {
			return "", CallRequest{}, BuildToolName(serverName, parsedToolName), fmt.Errorf(
				"%w: tool %q belongs to server %q, not %q",
				ErrInvalidMcpToolName,
				invocation.Tool,
				serverName,
				a.serverName,
			)
		}
		if parsedToolName == "" {
			return "", CallRequest{}, BuildToolName(serverName, parsedToolName), fmt.Errorf(
				"%w: tool %q is missing a tool component",
				ErrInvalidMcpToolName,
				invocation.Tool,
			)
		}
		toolName = parsedToolName
		fqName = BuildToolName(serverName, parsedToolName)
	}

	arguments, err := invocationArguments(invocation)
	if err != nil {
		return "", CallRequest{}, fqName, err
	}

	request = CallRequest{
		Name:      toolName,
		Arguments: arguments,
		Metadata:  mergeMetadata(invocation.Metadata, map[string]string{MetadataServerNameKey: a.serverName, MetadataToolNameKey: toolName, MetadataNameKey: fqName}),
	}
	return toolName, request, fqName, nil
}

func toolToSpec(serverName string, tool Tool) (core.ToolSpec, error) {
	name := qualifyToolName(serverName, tool.Name)
	inputSchema, err := marshalRawSchema(tool.InputSchema)
	if err != nil {
		return core.ToolSpec{}, fmt.Errorf("marshal tool %q schema: %w", tool.Name, err)
	}

	return core.ToolSpec{
		Name:        name,
		Description: tool.Description,
		InputSchema: inputSchema,
		Metadata: mergeMetadata(tool.Metadata, map[string]string{
			MetadataServerNameKey: serverName,
			MetadataToolNameKey:   rawToolName(tool.Name),
			MetadataNameKey:       name,
		}),
	}, nil
}

func toolResultFromCallResult(invocation core.ToolInvocation, fqName string, result CallResult, duration time.Duration, toolName string) core.ToolResult {
	status := core.ToolStatusSucceeded
	var toolErr *core.Error
	if result.Error != "" {
		status = core.ToolStatusFailed
		toolErr = core.NewError(core.ErrorCodeInternal, result.Error)
	}

	output, structured, err := encodeToolContent(result.Content)
	if err != nil {
		status = core.ToolStatusFailed
		toolErr = core.NewError(core.ErrorCodeInternal, err.Error())
	}

	metadata := mergeMetadata(result.Metadata, invocation.Metadata, map[string]string{
		MetadataServerNameKey: invocationServerName(fqName),
		MetadataToolNameKey:   toolName,
		MetadataNameKey:       fqName,
	})

	if toolErr != nil {
		metadata["mcp.error"] = toolErr.Message
	}

	return core.ToolResult{
		InvocationID: invocation.ID,
		Tool:         fqName,
		Status:       status,
		Output:       output,
		Structured:   structured,
		Error:        toolErr,
		Duration:     duration,
		Metadata:     metadata,
	}
}

func failedToolResult(invocation core.ToolInvocation, fqName string, duration time.Duration, err error) core.ToolResult {
	if fqName == "" {
		fqName = invocation.Tool
	}
	metadata := mergeMetadata(invocation.Metadata, map[string]string{
		MetadataNameKey: fqName,
	})
	if serverName, toolName, ok := ParseToolName(fqName); ok {
		metadata[MetadataServerNameKey] = serverName
		metadata[MetadataToolNameKey] = toolName
	}
	return core.ToolResult{
		InvocationID: invocation.ID,
		Tool:         fqName,
		Status:       core.ToolStatusFailed,
		Error:        core.NewError(core.ErrorCodeInternal, err.Error()),
		Duration:     duration,
		Metadata:     metadata,
	}
}

func invocationArguments(invocation core.ToolInvocation) (map[string]any, error) {
	raw := invocation.Arguments
	if len(raw) == 0 {
		raw = invocation.Input
	}
	if len(raw) == 0 {
		return nil, nil
	}

	var arguments map[string]any
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, fmt.Errorf("tool invocation arguments must be a JSON object: %w", err)
	}
	return arguments, nil
}

func encodeToolContent(content any) (string, json.RawMessage, error) {
	switch value := content.(type) {
	case nil:
		return "", nil, nil
	case string:
		return value, nil, nil
	case []byte:
		return string(value), nil, nil
	case json.RawMessage:
		if len(value) == 0 {
			return "", nil, nil
		}
		return "", json.RawMessage(value), nil
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return "", nil, err
		}
		return "", json.RawMessage(data), nil
	}
}

func marshalRawSchema(schema map[string]any) (json.RawMessage, error) {
	if schema == nil {
		return nil, nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func mergeMetadata(maps ...map[string]string) map[string]string {
	size := 0
	for _, m := range maps {
		size += len(m)
	}
	if size == 0 {
		return nil
	}
	out := make(map[string]string, size)
	for _, m := range maps {
		for key, value := range m {
			out[key] = value
		}
	}
	return out
}

func qualifyToolName(serverName, toolName string) string {
	if _, parsedToolName, ok := ParseToolName(toolName); ok && parsedToolName != "" {
		return BuildToolName(serverName, parsedToolName)
	}
	return BuildToolName(serverName, toolName)
}

func rawToolName(name string) string {
	if _, toolName, ok := ParseToolName(name); ok && toolName != "" {
		return toolName
	}
	return name
}

func invocationServerName(fqName string) string {
	serverName, _, ok := ParseToolName(fqName)
	if !ok {
		return ""
	}
	return serverName
}
