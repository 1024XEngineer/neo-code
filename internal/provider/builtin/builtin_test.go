package builtin

import (
	"testing"

	"neo-code/internal/provider"
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

func TestRegister(t *testing.T) {
	t.Parallel()

	t.Run("nil registry", func(t *testing.T) {
		t.Parallel()
		err := Register(nil)
		if err == nil {
			t.Fatal("expected error for nil registry")
		}
		if err.Error() != "builtin provider registry is nil" {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid registry", func(t *testing.T) {
		t.Parallel()
		registry := provider.NewRegistry()
		err := Register(registry)
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		if !registry.Supports(openai.DriverName) {
			t.Fatalf("expected registry to include %q driver", openai.DriverName)
		}
	})
}
