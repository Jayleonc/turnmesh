package openai

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/Jayleonc/turnmesh/internal/core"
	"github.com/Jayleonc/turnmesh/internal/model"
)

type Option func(*Provider)

type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
	models  []model.ModelInfo
}

func NewProvider(opts ...Option) *Provider {
	p := &Provider{
		apiKey:  os.Getenv("OPENAI_API_KEY"),
		baseURL: defaultAPIBaseURL,
		client:  http.DefaultClient,
		models:  defaultModelSet(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	if p.baseURL == "" {
		p.baseURL = defaultAPIBaseURL
	}
	if p.client == nil {
		p.client = http.DefaultClient
	}
	if len(p.models) == 0 {
		p.models = defaultModelSet()
	}
	return p
}

func WithAPIKey(apiKey string) Option {
	return func(p *Provider) {
		p.apiKey = apiKey
	}
}

func WithBaseURL(baseURL string) Option {
	return func(p *Provider) {
		p.baseURL = strings.TrimRight(baseURL, "/")
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(p *Provider) {
		p.client = client
	}
}

func WithModels(models []model.ModelInfo) Option {
	return func(p *Provider) {
		p.models = append([]model.ModelInfo(nil), models...)
	}
}

func (p *Provider) Name() string {
	return "openai"
}

func (p *Provider) ListModels(ctx context.Context) ([]model.ModelInfo, error) {
	if ctx == nil {
		return nil, errors.New("openai provider: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]model.ModelInfo(nil), p.models...), nil
}

func (p *Provider) NewSession(ctx context.Context, opts model.SessionOptions) (model.Session, error) {
	if ctx == nil {
		return nil, errors.New("openai provider: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	apiKey := p.apiKey
	if apiKey == "" {
		return nil, errors.New("openai provider: OPENAI_API_KEY is not set")
	}

	modelName := opts.Model
	if modelName == "" {
		modelName = defaultModelName
	}

	caps := capabilitiesForModel(modelName)
	id := newSessionID()

	return &Session{
		id:              id,
		provider:        p.Name(),
		model:           modelName,
		caps:            caps,
		apiKey:          apiKey,
		baseURL:         p.baseURL,
		client:          p.client,
		systemPrompt:    opts.SystemPrompt,
		temperature:     opts.Temperature,
		maxOutputTokens: opts.MaxOutputTokens,
		seed:            opts.Seed,
		tools:           append([]core.ToolSpec(nil), opts.Tools...),
	}, nil
}

func defaultModelSet() []model.ModelInfo {
	return []model.ModelInfo{
		{
			Name:        "gpt-5",
			DisplayName: "GPT-5",
			Capabilities: model.Capabilities{
				CanStream:           true,
				CanToolCall:         true,
				CanParallelToolUse:  true,
				CanStructuredOutput: true,
				CanImageInput:       true,
				CanSystemPrompt:     true,
			},
			Metadata: map[string]string{
				"source": "static-known-set",
				"basis":  "OpenAI Responses API docs and public model naming",
			},
		},
		{
			Name:        "gpt-4o-mini",
			DisplayName: "GPT-4o mini",
			Capabilities: model.Capabilities{
				CanStream:           true,
				CanToolCall:         true,
				CanParallelToolUse:  true,
				CanStructuredOutput: true,
				CanImageInput:       true,
				CanSystemPrompt:     true,
			},
			Metadata: map[string]string{
				"source": "static-known-set",
				"basis":  "OpenAI Responses API docs and public model naming",
			},
		},
		{
			Name:        "gpt-4.1",
			DisplayName: "GPT-4.1",
			Capabilities: model.Capabilities{
				CanStream:           true,
				CanToolCall:         true,
				CanParallelToolUse:  true,
				CanStructuredOutput: true,
				CanImageInput:       true,
				CanSystemPrompt:     true,
			},
			Metadata: map[string]string{
				"source": "static-known-set",
				"basis":  "OpenAI Responses API docs and public model naming",
			},
		},
	}
}

func capabilitiesForModel(string) model.Capabilities {
	return model.Capabilities{
		CanStream:           true,
		CanToolCall:         true,
		CanParallelToolUse:  true,
		CanStructuredOutput: true,
		CanImageInput:       true,
		CanSystemPrompt:     true,
	}
}
