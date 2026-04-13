package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

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
		apiKey:  os.Getenv("ANTHROPIC_API_KEY"),
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
	return "anthropic"
}

func (p *Provider) ListModels(ctx context.Context) ([]model.ModelInfo, error) {
	if ctx == nil {
		return nil, errors.New("anthropic provider: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]model.ModelInfo(nil), p.models...), nil
}

func (p *Provider) NewSession(ctx context.Context, opts model.SessionOptions) (model.Session, error) {
	if ctx == nil {
		return nil, errors.New("anthropic provider: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	apiKey := p.apiKey
	if apiKey == "" {
		return nil, errors.New("anthropic provider: ANTHROPIC_API_KEY is not set")
	}

	modelName := opts.Model
	if modelName == "" {
		modelName = defaultModelName
	}

	return newSession(sessionConfig{
		id:           newSessionID(),
		provider:     p.Name(),
		model:        modelName,
		apiKey:       apiKey,
		baseURL:      p.baseURL,
		client:       p.client,
		systemPrompt: opts.SystemPrompt,
		temperature:  opts.Temperature,
		maxTokens:    defaultInt(opts.MaxOutputTokens, defaultMaxTokens),
		tools:        append([]core.ToolSpec(nil), opts.Tools...),
	}), nil
}

func defaultModelSet() []model.ModelInfo {
	return []model.ModelInfo{
		{
			Name:        "claude-sonnet-4-20250514",
			DisplayName: "Claude Sonnet 4",
			Capabilities: model.Capabilities{
				CanStream:           true,
				CanToolCall:         true,
				CanParallelToolUse:  true,
				CanStructuredOutput: true,
				CanImageInput:       true,
				CanThinking:         true,
				CanSystemPrompt:     true,
			},
			Metadata: map[string]string{
				"source": "static-known-set",
			},
		},
		{
			Name:        "claude-haiku-4-20250514",
			DisplayName: "Claude Haiku 4",
			Capabilities: model.Capabilities{
				CanStream:           true,
				CanToolCall:         true,
				CanParallelToolUse:  true,
				CanStructuredOutput: true,
				CanImageInput:       true,
				CanThinking:         true,
				CanSystemPrompt:     true,
			},
			Metadata: map[string]string{
				"source": "static-known-set",
			},
		},
	}
}

func defaultInt(value *int, fallback int) int {
	if value == nil || *value <= 0 {
		return fallback
	}
	return *value
}

func newSessionID() string {
	return fmt.Sprintf("anthropic-%d", time.Now().UnixNano())
}
