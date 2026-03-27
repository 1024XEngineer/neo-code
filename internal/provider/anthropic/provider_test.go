package anthropic

import (
	"context"
	"strings"
	"testing"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/provider"
)

func TestProviderScaffold(t *testing.T) {
	t.Parallel()

	cfg := config.ProviderConfig{Name: config.ProviderAnthropic, Type: config.ProviderAnthropic}
	p := New(cfg)
	if p.Name() != config.ProviderAnthropic {
		t.Fatalf("expected provider name %q, got %q", config.ProviderAnthropic, p.Name())
	}

	_, err := p.Chat(context.Background(), provider.ChatRequest{}, make(chan provider.StreamEvent))
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected scaffold error, got %v", err)
	}
}
