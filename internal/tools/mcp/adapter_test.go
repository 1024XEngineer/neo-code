package mcp

import (
	"context"
	"errors"
	"testing"
)

func TestAdapterFactoryBuildAdapters(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &stubServerClient{
		tools: []ToolDescriptor{
			{
				Name:        "search",
				Description: "search docs",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	}
	if err := registry.RegisterServer("docs", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}
	if err := registry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh tools: %v", err)
	}

	factory := NewAdapterFactory(registry)
	adapters, err := factory.BuildAdapters(context.Background())
	if err != nil {
		t.Fatalf("BuildAdapters() error = %v", err)
	}
	if len(adapters) != 1 {
		t.Fatalf("expected one adapter, got %d", len(adapters))
	}
	if adapters[0].FullName() != "mcp.docs.search" {
		t.Fatalf("unexpected adapter full name: %q", adapters[0].FullName())
	}
}

func TestAdapterCall(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &stubServerClient{
		tools: []ToolDescriptor{
			{Name: "search", InputSchema: map[string]any{"type": "object"}},
		},
		callResult: CallResult{
			Content: "result body",
			Metadata: map[string]any{
				"latency_ms": 20,
			},
		},
	}
	if err := registry.RegisterServer("docs", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}
	if err := registry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh tools: %v", err)
	}

	adapter, err := NewAdapter(registry, "docs", ToolDescriptor{
		Name:        "search",
		Description: "search docs",
		InputSchema: map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	result, err := adapter.Call(context.Background(), []byte(`{"q":"mcp"}`))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Content != "result body" {
		t.Fatalf("expected result content, got %q", result.Content)
	}
}

func TestAdapterCallError(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &stubServerClient{
		tools: []ToolDescriptor{
			{Name: "search", InputSchema: map[string]any{"type": "object"}},
		},
		callErr: errors.New("transport timeout"),
	}
	if err := registry.RegisterServer("docs", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}
	if err := registry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh tools: %v", err)
	}

	adapter, err := NewAdapter(registry, "docs", ToolDescriptor{
		Name: "search",
	})
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if _, err := adapter.Call(context.Background(), []byte(`{"q":"mcp"}`)); err == nil {
		t.Fatalf("expected call error")
	}
}

func TestAdapterAccessorsAndSchemaClone(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	adapter, err := NewAdapter(registry, "Docs", ToolDescriptor{
		Name:        "search",
		Description: "",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q": map[string]any{"type": "string"},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if adapter.ServerID() != "docs" {
		t.Fatalf("expected normalized server id docs, got %q", adapter.ServerID())
	}
	if adapter.ToolName() != "search" {
		t.Fatalf("expected tool name search, got %q", adapter.ToolName())
	}
	if adapter.Description() == "" {
		t.Fatalf("expected non-empty fallback description")
	}

	schema1 := adapter.Schema()
	schema2 := adapter.Schema()
	props1, _ := schema1["properties"].(map[string]any)
	props1["q"] = map[string]any{"type": "number"}
	props2, _ := schema2["properties"].(map[string]any)
	query2, _ := props2["q"].(map[string]any)
	if query2["type"] != "string" {
		t.Fatalf("expected schema clone not mutated, got %v", query2["type"])
	}
}

func TestAdapterBuildAndCreateErrors(t *testing.T) {
	t.Parallel()

	factory := NewAdapterFactory(nil)
	if _, err := factory.BuildAdapters(context.Background()); err == nil {
		t.Fatalf("expected nil registry error")
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	registry := NewRegistry()
	factory = NewAdapterFactory(registry)
	if _, err := factory.BuildAdapters(canceledCtx); err == nil {
		t.Fatalf("expected canceled context error")
	}

	if _, err := NewAdapter(nil, "docs", ToolDescriptor{Name: "search"}); err == nil {
		t.Fatalf("expected nil registry error")
	}
	if _, err := NewAdapter(registry, " ", ToolDescriptor{Name: "search"}); err == nil {
		t.Fatalf("expected empty server id error")
	}
	if _, err := NewAdapter(registry, "docs", ToolDescriptor{Name: " "}); err == nil {
		t.Fatalf("expected empty tool name error")
	}
}

func TestAdapterCallBoundary(t *testing.T) {
	t.Parallel()

	var nilAdapter *Adapter
	if _, err := nilAdapter.Call(context.Background(), nil); err == nil {
		t.Fatalf("expected nil adapter error")
	}

	registry := NewRegistry()
	adapter, err := NewAdapter(registry, "docs", ToolDescriptor{Name: "search"})
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Call(canceledCtx, nil); err == nil {
		t.Fatalf("expected context canceled error")
	}
}
