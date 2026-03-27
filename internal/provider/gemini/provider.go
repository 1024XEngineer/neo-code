package gemini

import (
	"context"
	"errors"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/provider"
)

type Provider struct {
	cfg config.ProviderConfig
}

func New(cfg config.ProviderConfig) *Provider {
	return &Provider{cfg: cfg}
}

func (p *Provider) Name() string {
	return p.cfg.Name
}

func (p *Provider) Chat(ctx context.Context, req provider.ChatRequest, events chan<- provider.StreamEvent) (provider.ChatResponse, error) {
	return provider.ChatResponse{}, errors.New("gemini provider is scaffolded but not implemented in this MVP")
}
