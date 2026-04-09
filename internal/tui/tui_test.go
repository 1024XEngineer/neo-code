package tui

import (
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/runtime"
	tuibootstrap "neo-code/internal/tui/bootstrap"
)

func TestAppTypeAlias(t *testing.T) {
	var _ App = App{}
}

func TestProviderControllerTypeAlias(t *testing.T) {
	var _ ProviderController = ProviderController(nil)
}

func TestWorkspaceSwitcherTypeAlias(t *testing.T) {
	var _ WorkspaceSwitcher = WorkspaceSwitcher(nil)
}

func TestNewForwardsToCore(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		_, err := New(nil, &config.Manager{}, &runtime.Service{}, nil, nil)
		if err == nil {
			t.Error("expected error for nil runtime")
		}
	})
}

func TestNewWithBootstrapForwardsToCore(t *testing.T) {
	t.Run("empty options", func(t *testing.T) {
		_, err := NewWithBootstrap(tuibootstrap.Options{})
		if err == nil {
			t.Error("expected error for empty options")
		}
	})
}
