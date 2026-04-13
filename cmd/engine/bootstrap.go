package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/Jayleonc/turnmesh/internal/agent"
	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/executor"
	"github.com/Jayleonc/turnmesh/internal/feedback"
	"github.com/Jayleonc/turnmesh/internal/mcp"
	"github.com/Jayleonc/turnmesh/internal/memory"
	"github.com/Jayleonc/turnmesh/internal/model"
	"github.com/Jayleonc/turnmesh/internal/model/anthropic"
	"github.com/Jayleonc/turnmesh/internal/model/openai"
	"github.com/Jayleonc/turnmesh/internal/orchestrator"
)

// MCPServer describes one MCP capability provider to register in the runtime.
type MCPServer struct {
	Name     string
	Provider mcp.CapabilityProvider
}

// Config defines the runtime assembly inputs.
type Config struct {
	Sink           feedback.Sink
	Provider       string
	SessionOptions model.SessionOptions
	Providers      *model.Registry
	Tools          *executor.RegistryStore
	Batch          executor.BatchRuntime
	Memory         *memory.Runtime
	Agents         agent.Runtime
	MCPServers     []MCPServer
}

// Runtime is the assembled kernel runtime for the engine entrypoint.
type Runtime struct {
	Engine    *orchestrator.Engine
	Providers *model.Registry
	Tools     *executor.RegistryStore
	Batch     executor.BatchRuntime
	Memory    *memory.Runtime
	Agents    agent.Runtime
	Session   model.Session
}

// Close releases any runtime-scoped session resources.
func (r *Runtime) Close() error {
	if r == nil || r.Session == nil {
		return nil
	}
	return r.Session.Close()
}

// BuildRuntime assembles the default engine runtime without polluting the core loop.
func BuildRuntime(ctx context.Context, cfg Config) (*Runtime, error) {
	if ctx == nil {
		return nil, errors.New("engine bootstrap: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	providers := cfg.Providers
	if providers == nil {
		providers = model.NewRegistry()
		if err := registerDefaultProviders(providers); err != nil {
			return nil, err
		}
	}

	tools := cfg.Tools
	if tools == nil {
		tools = executor.NewRegistryStore()
		if err := registerDefaultTools(tools); err != nil {
			return nil, err
		}
	}
	if err := registerMCPTools(ctx, tools, cfg.MCPServers); err != nil {
		return nil, err
	}

	batch := cfg.Batch
	if batch == nil {
		batch = executor.NewBatchRuntime(tools)
	}

	mem := cfg.Memory
	if mem == nil {
		mem = memory.NewRuntime(memory.NewInMemoryStore(), nil)
	}
	coordinator := newMemoryCoordinator(mem)

	opts := cfg.SessionOptions
	if len(opts.Tools) == 0 {
		opts.Tools = coreToolCatalog(tools.List())
	}

	var session model.Session
	if cfg.Provider != "" {
		var err error
		session, err = providers.NewSession(ctx, cfg.Provider, opts)
		if err != nil {
			return nil, err
		}
	}

	engine := orchestrator.New(orchestrator.Config{
		Preparer:  coordinator,
		Finalizer: coordinator,
		Session:   session,
		Tools:     executor.NewToolDispatcher(tools),
		ToolBatch: batch,
		Sink:      cfg.Sink,
	})

	agents := cfg.Agents
	if agents == nil {
		agents = agent.NewAgentRuntime(&kernelAgentRunner{
			sink:      cfg.Sink,
			provider:  cfg.Provider,
			providers: providers,
			tools:     tools,
			memory:    mem,
		})
	}

	return &Runtime{
		Engine:    engine,
		Providers: providers,
		Tools:     tools,
		Batch:     batch,
		Memory:    mem,
		Agents:    agents,
		Session:   session,
	}, nil
}

func registerDefaultProviders(registry *model.Registry) error {
	for _, provider := range []model.Provider{
		openai.NewProvider(),
		anthropic.NewProvider(),
	} {
		if err := registry.Register(provider); err != nil {
			return err
		}
	}
	return nil
}

func registerDefaultTools(registry *executor.RegistryStore) error {
	return registry.Register(executor.NewCommandTool(executor.ToolSpec{
		Name:        "shell",
		Description: "run a local shell command",
	}, executor.NewLocalCommandExecutor()))
}

func registerMCPTools(ctx context.Context, registry *executor.RegistryStore, servers []MCPServer) error {
	for _, server := range servers {
		if server.Name == "" {
			return errors.New("engine bootstrap: mcp server name is required")
		}
		adapter, err := mcp.NewToolAdapter(server.Name, server.Provider)
		if err != nil {
			return err
		}
		toolAdapter := adapter

		specs, err := toolAdapter.ListTools(ctx)
		if err != nil {
			return fmt.Errorf("engine bootstrap: list mcp tools for %q: %w", server.Name, err)
		}

		for _, coreSpec := range specs {
			spec := executorToolSpec(coreSpec)
			fqName := coreSpec.Name
			if err := registry.Register(executor.NewHandlerTool(spec, func(ctx context.Context, request executor.ToolRequest) (executor.ToolOutcome, error) {
				result, err := toolAdapter.Invoke(ctx, core.ToolInvocation{
					Tool:       fqName,
					Input:      cloneRawMessage(request.Input),
					Arguments:  cloneRawMessage(request.Arguments),
					Caller:     request.Caller,
					ApprovalID: request.ApprovalID,
					Metadata:   cloneStringMap(request.Metadata),
				})
				return executor.ToolOutcome{
					Output:     result.Output,
					Structured: cloneRawMessage(result.Structured),
					Metadata:   cloneStringMap(result.Metadata),
					Duration:   result.Duration,
					Status:     result.Status,
					Error:      result.Error,
				}, err
			})); err != nil {
				return err
			}
		}
	}

	return nil
}

func coreToolCatalog(specs []executor.ToolSpec) []core.ToolSpec {
	if len(specs) == 0 {
		return nil
	}

	tools := make([]core.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, core.ToolSpec{
			Name:            spec.Name,
			Description:     spec.Description,
			InputSchema:     cloneRawMessage(spec.InputSchema),
			OutputSchema:    cloneRawMessage(spec.OutputSchema),
			ReadOnly:        spec.ReadOnly,
			ConcurrencySafe: spec.ConcurrencySafe,
			Timeout:         spec.Timeout,
			Metadata:        cloneStringMap(spec.Metadata),
		})
	}
	return tools
}

func executorToolSpec(spec core.ToolSpec) executor.ToolSpec {
	return executor.ToolSpec{
		Name:            spec.Name,
		Description:     spec.Description,
		InputSchema:     cloneRawMessage(spec.InputSchema),
		OutputSchema:    cloneRawMessage(spec.OutputSchema),
		ReadOnly:        spec.ReadOnly,
		ConcurrencySafe: spec.ConcurrencySafe,
		Timeout:         spec.Timeout,
		Metadata:        cloneStringMap(spec.Metadata),
	}
}

func cloneRawMessage(raw []byte) []byte {
	if len(raw) == 0 {
		return nil
	}
	return append([]byte(nil), raw...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
