package openaichat

import (
	"context"
	"net/http"
	"testing"
)

func TestProviderListModelsUsesStaticSet(t *testing.T) {
	p := NewProvider(WithAPIKey("sk-test"))

	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) == 0 {
		t.Fatal("ListModels() returned no models")
	}
	if got := models[0].Metadata["source"]; got != "static-known-set" {
		t.Fatalf("first model source = %q, want static-known-set", got)
	}
}

func TestNewProviderReadsEnvAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")

	p := NewProvider()
	if p.apiKey != "env-key" {
		t.Fatalf("apiKey = %q, want env-key", p.apiKey)
	}
}

func TestProviderAllowsCustomHTTPClient(t *testing.T) {
	client := &http.Client{}
	p := NewProvider(WithAPIKey("sk-test"), WithHTTPClient(client))

	if p.client != client {
		t.Fatal("custom HTTP client not applied")
	}
}
