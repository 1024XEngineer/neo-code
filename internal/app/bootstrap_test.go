package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dust/neo-code/internal/config"
	"github.com/dust/neo-code/internal/tools"
)

func TestNewProgram(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	program, err := NewProgram(context.Background())
	if err != nil {
		t.Fatalf("NewProgram() error = %v", err)
	}
	if program == nil {
		t.Fatalf("expected tea program")
	}

	configPath := filepath.Join(home, ".neocode", "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be created at %q: %v", configPath, err)
	}
}

func TestBuildToolRegistryUsesWebFetchConfig(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("1234567890"))
	}))
	defer server.Close()

	cfg := config.Default().Clone()
	cfg.Workdir = t.TempDir()
	cfg.Tools.WebFetch.MaxResponseBytes = 4

	registry := buildToolRegistry(cfg)
	tool, err := registry.Get("webfetch")
	if err != nil {
		t.Fatalf("registry.Get(webfetch) error = %v", err)
	}

	args, err := json.Marshal(map[string]string{"url": server.URL})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	result, execErr := tool.Execute(context.Background(), tools.ToolCallInput{
		Name:      "webfetch",
		Arguments: args,
	})
	if execErr != nil {
		t.Fatalf("webfetch execute error = %v", execErr)
	}
	if truncated, ok := result.Metadata["truncated"].(bool); !ok || !truncated {
		t.Fatalf("expected truncated metadata, got %+v", result.Metadata)
	}
	if result.Content == "" {
		t.Fatalf("expected formatted webfetch content")
	}
}
