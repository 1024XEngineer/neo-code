package gemini

import (
	"context"
	"strings"
	"testing"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/provider"
)

func TestProviderScaffold(t *testing.T) {
	t.Parallel()

	cfg := config.ProviderConfig{Name: config.ProviderGemini, Type: config.ProviderGemini}
	p := New(cfg)
	if p.Name() != config.ProviderGemini {
		t.Fatalf("expected provider name %q, got %q", config.ProviderGemini, p.Name())
	}

	_, err := p.Chat(context.Background(), provider.ChatRequest{}, make(chan provider.StreamEvent))
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected scaffold error, got %v", err)
	}
}
