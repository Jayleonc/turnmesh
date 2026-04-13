package model

import (
	"context"
	"errors"
	"testing"

	"github.com/Jayleonc/turnmesh/internal/core"
)

func TestRegistryRegisterLookupListAndNewSession(t *testing.T) {
	reg := NewRegistry()

	alpha := &stubProvider{
		name: "alpha",
		models: []ModelInfo{
			{Name: "alpha-model"},
		},
	}
	beta := &stubProvider{name: "beta"}

	if err := reg.Register(beta); err != nil {
		t.Fatalf("Register(beta) error = %v", err)
	}
	if err := reg.Register(alpha); err != nil {
		t.Fatalf("Register(alpha) error = %v", err)
	}

	if err := reg.Register(&stubProvider{name: "alpha"}); !errors.Is(err, ErrProviderAlreadyExists) {
		t.Fatalf("Register(duplicate) error = %v, want ErrProviderAlreadyExists", err)
	}

	got, err := reg.Lookup("alpha")
	if err != nil {
		t.Fatalf("Lookup(alpha) error = %v", err)
	}
	if got != alpha {
		t.Fatalf("Lookup(alpha) = %#v, want %#v", got, alpha)
	}

	if _, err := reg.Lookup("missing"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("Lookup(missing) error = %v, want ErrProviderNotFound", err)
	}

	if got := reg.Names(); len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("Names() = %#v, want [alpha beta]", got)
	}

	providers := reg.List()
	if len(providers) != 2 {
		t.Fatalf("List() len = %d, want 2", len(providers))
	}
	if providers[0] != alpha || providers[1] != beta {
		t.Fatalf("List() = %#v, want [alpha beta]", providers)
	}

	sess, err := reg.NewSession(context.Background(), "alpha", SessionOptions{
		Model:        "alpha-model",
		SystemPrompt: "stay focused",
		Metadata:     map[string]string{"origin": "test"},
		Tools:        []core.ToolSpec{{Name: "shell"}},
	})
	if err != nil {
		t.Fatalf("NewSession(alpha) error = %v", err)
	}

	gotSession, ok := sess.(*stubSession)
	if !ok {
		t.Fatalf("NewSession(alpha) type = %T, want *stubSession", sess)
	}
	if gotSession.provider != "alpha" {
		t.Fatalf("session provider = %q, want alpha", gotSession.provider)
	}
	if gotSession.model != "alpha-model" {
		t.Fatalf("session model = %q, want alpha-model", gotSession.model)
	}
	if gotSession.systemPrompt != "stay focused" {
		t.Fatalf("session systemPrompt = %q, want stay focused", gotSession.systemPrompt)
	}
	if gotSession.metadata["origin"] != "test" {
		t.Fatalf("session metadata = %#v, want origin=test", gotSession.metadata)
	}
	if len(gotSession.tools) != 1 || gotSession.tools[0].Name != "shell" {
		t.Fatalf("session tools = %#v, want shell", gotSession.tools)
	}
}

func TestPackageLevelRegistryHelpers(t *testing.T) {
	previous := defaultRegistry
	defaultRegistry = NewRegistry()
	t.Cleanup(func() {
		defaultRegistry = previous
	})

	provider := &stubProvider{name: "gamma"}

	if err := RegisterProvider(provider); err != nil {
		t.Fatalf("RegisterProvider(gamma) error = %v", err)
	}
	if got := DefaultRegistry(); got == nil {
		t.Fatal("DefaultRegistry() returned nil")
	}

	found, err := LookupProvider("gamma")
	if err != nil {
		t.Fatalf("LookupProvider(gamma) error = %v", err)
	}
	if found != provider {
		t.Fatalf("LookupProvider(gamma) = %#v, want %#v", found, provider)
	}

	names := ListProviderNames()
	if len(names) == 0 || names[len(names)-1] != "gamma" {
		t.Fatalf("ListProviderNames() = %#v, want gamma to be present", names)
	}
}

func TestRegistryRejectsNilAndEmptyProvider(t *testing.T) {
	reg := NewRegistry()

	if err := reg.Register(nil); !errors.Is(err, ErrNilProvider) {
		t.Fatalf("Register(nil) error = %v, want ErrNilProvider", err)
	}
	if err := reg.Register(&stubProvider{}); !errors.Is(err, ErrEmptyProviderName) {
		t.Fatalf("Register(empty) error = %v, want ErrEmptyProviderName", err)
	}
	if _, err := reg.Lookup(""); !errors.Is(err, ErrEmptyProviderName) {
		t.Fatalf("Lookup(empty) error = %v, want ErrEmptyProviderName", err)
	}
}

type stubProvider struct {
	name       string
	models     []ModelInfo
	session    Session
	newSession func(context.Context, SessionOptions) (Session, error)
}

func (p *stubProvider) Name() string {
	return p.name
}

func (p *stubProvider) ListModels(context.Context) ([]ModelInfo, error) {
	return append([]ModelInfo(nil), p.models...), nil
}

func (p *stubProvider) NewSession(ctx context.Context, opts SessionOptions) (Session, error) {
	if p.newSession != nil {
		return p.newSession(ctx, opts)
	}
	if p.session != nil {
		return p.session, nil
	}
	return &stubSession{
		provider:     p.name,
		model:        opts.Model,
		systemPrompt: opts.SystemPrompt,
		metadata:     opts.Metadata,
		tools:        append([]core.ToolSpec(nil), opts.Tools...),
	}, nil
}

type stubSession struct {
	provider     string
	model        string
	systemPrompt string
	metadata     map[string]string
	tools        []core.ToolSpec
}

func (s *stubSession) ID() string {
	return "stub-session"
}

func (s *stubSession) Provider() string {
	return s.provider
}

func (s *stubSession) Model() string {
	return s.model
}

func (s *stubSession) Capabilities() Capabilities {
	return Capabilities{}
}

func (s *stubSession) StreamTurn(context.Context, core.TurnInput) (<-chan core.TurnEvent, error) {
	ch := make(chan core.TurnEvent)
	close(ch)
	return ch, nil
}

func (s *stubSession) Close() error {
	return nil
}
