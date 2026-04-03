package builtin

import (
	"testing"

	"neo-code/internal/provider/openai"
)

func TestNewRegistry(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if registry == nil {
		t.Fatal("expected non-nil registry")
	}
	if !registry.Supports(openai.DriverName) {
		t.Fatalf("expected registry to include %q driver", openai.DriverName)
	}
}
