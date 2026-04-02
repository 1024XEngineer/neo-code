package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/tools"
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

func TestBuildToolManagerWrapsRegistry(t *testing.T) {
	t.Parallel()

	registry := tools.NewRegistry()
	registry.Register(stubToolForBootstrap{name: "bash", content: "ok"})
	workdir := t.TempDir()
	manager, err := buildToolManager(registry)
	if err != nil {
		t.Fatalf("buildToolManager() error = %v", err)
	}
	if manager == nil {
		t.Fatalf("expected tool manager")
	}

	specs, err := manager.ListAvailableSpecs(context.Background(), tools.SpecListInput{})
	if err != nil {
		t.Fatalf("ListAvailableSpecs() error = %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %+v", specs)
	}

	result, execErr := manager.Execute(context.Background(), tools.ToolCallInput{
		Name:      "bash",
		Arguments: []byte(`{"command":"echo hi"}`),
		Workdir:   workdir,
	})
	if execErr != nil {
		t.Fatalf("Execute() error = %v", execErr)
	}
	if result.Content != "ok" {
		t.Fatalf("expected ok result, got %+v", result)
	}

	_, execErr = manager.Execute(context.Background(), tools.ToolCallInput{
		Name:      "bash",
		Arguments: []byte(`{"command":"echo hi","workdir":"../outside"}`),
		Workdir:   workdir,
	})
	if execErr == nil {
		t.Fatalf("expected sandbox rejection for outside workdir")
	}
}

type stubToolForBootstrap struct {
	name    string
	content string
}

func (s stubToolForBootstrap) Name() string           { return s.name }
func (s stubToolForBootstrap) Description() string    { return "stub" }
func (s stubToolForBootstrap) Schema() map[string]any { return map[string]any{"type": "object"} }
func (s stubToolForBootstrap) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	return tools.ToolResult{Name: s.name, Content: s.content}, nil
}
