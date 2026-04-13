package executor

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// RegistryStore is the default in-memory tool registry/runtime.
type RegistryStore struct {
	mu    sync.RWMutex
	tools map[string]ToolRuntime
}

// NewRegistryStore creates an empty in-memory tool registry.
func NewRegistryStore() *RegistryStore {
	return &RegistryStore{
		tools: make(map[string]ToolRuntime),
	}
}

// Register adds a tool to the registry.
func (r *RegistryStore) Register(tool ToolRuntime) error {
	if tool == nil {
		return fmt.Errorf("executor: nil tool")
	}

	spec := tool.Spec()
	if spec.Name == "" {
		return fmt.Errorf("executor: tool name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.tools == nil {
		r.tools = make(map[string]ToolRuntime)
	}
	if _, exists := r.tools[spec.Name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateTool, spec.Name)
	}

	r.tools[spec.Name] = tool
	return nil
}

// Lookup returns a registered tool by name.
func (r *RegistryStore) Lookup(name string) (ToolRuntime, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[name]
	return tool, ok
}

// List returns the registered tool specs in name order.
func (r *RegistryStore) List() []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	specs := make([]ToolSpec, 0, len(r.tools))
	for _, tool := range r.tools {
		specs = append(specs, tool.Spec())
	}

	sort.Slice(specs, func(i, j int) bool {
		return specs[i].Name < specs[j].Name
	})

	return specs
}

// Execute routes a generic tool request to the named tool.
func (r *RegistryStore) Execute(ctx context.Context, name string, request ToolRequest) (ToolOutcome, error) {
	tool, ok := r.Lookup(name)
	if !ok {
		return ToolOutcome{}, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}

	return tool.Execute(ctx, request)
}
