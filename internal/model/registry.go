package model

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrNilProvider           = errors.New("model registry: nil provider")
	ErrEmptyProviderName     = errors.New("model registry: empty provider name")
	ErrProviderAlreadyExists = errors.New("model registry: provider already registered")
	ErrProviderNotFound      = errors.New("model registry: provider not found")
)

// Registry stores model providers by name and creates sessions from them.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

var defaultRegistry = NewRegistry()

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// DefaultRegistry returns the package-level registry used by helper functions.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// RegisterProvider registers a provider in the package-level registry.
func RegisterProvider(provider Provider) error {
	return defaultRegistry.Register(provider)
}

// LookupProvider finds a provider in the package-level registry by name.
func LookupProvider(name string) (Provider, error) {
	return defaultRegistry.Lookup(name)
}

// ListProviders returns all providers from the package-level registry.
func ListProviders() []Provider {
	return defaultRegistry.List()
}

// ListProviderNames returns the sorted provider names from the package-level registry.
func ListProviderNames() []string {
	return defaultRegistry.Names()
}

// NewSession creates a session from the package-level registry.
func NewSession(ctx context.Context, providerName string, opts SessionOptions) (Session, error) {
	return defaultRegistry.NewSession(ctx, providerName, opts)
}

// Register adds a provider to the registry.
func (r *Registry) Register(provider Provider) error {
	if r == nil {
		return errors.New("model registry: nil registry")
	}
	if provider == nil {
		return ErrNilProvider
	}

	name := strings.TrimSpace(provider.Name())
	if name == "" {
		return ErrEmptyProviderName
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("model registry: provider %q already registered: %w", name, ErrProviderAlreadyExists)
	}

	r.providers[name] = provider
	return nil
}

// Lookup returns the registered provider for name.
func (r *Registry) Lookup(name string) (Provider, error) {
	if r == nil {
		return nil, errors.New("model registry: nil registry")
	}

	key := strings.TrimSpace(name)
	if key == "" {
		return nil, ErrEmptyProviderName
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, ok := r.providers[key]
	if !ok {
		return nil, fmt.Errorf("model registry: provider %q not found: %w", key, ErrProviderNotFound)
	}
	return provider, nil
}

// List returns the registered providers in name order.
func (r *Registry) List() []Provider {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)

	providers := make([]Provider, 0, len(names))
	for _, name := range names {
		providers = append(providers, r.providers[name])
	}
	return providers
}

// Names returns the registered provider names in sorted order.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NewSession looks up a provider and creates a session from it.
func (r *Registry) NewSession(ctx context.Context, providerName string, opts SessionOptions) (Session, error) {
	provider, err := r.Lookup(providerName)
	if err != nil {
		return nil, err
	}
	return provider.NewSession(ctx, opts)
}
