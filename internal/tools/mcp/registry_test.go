package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type stubServerClient struct {
	mu            sync.Mutex
	tools         []ToolDescriptor
	callResult    CallResult
	listErr       error
	callErr       error
	healthErr     error
	lastToolName  string
	lastArguments []byte
}

type closableStubServerClient struct {
	stubServerClient
	closed bool
}

func (s *closableStubServerClient) Close() error {
	s.closed = true
	return nil
}

func (s *stubServerClient) ListTools(ctx context.Context) ([]ToolDescriptor, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return cloneToolDescriptors(s.tools), nil
}

func (s *stubServerClient) CallTool(ctx context.Context, toolName string, arguments []byte) (CallResult, error) {
	s.mu.Lock()
	s.lastToolName = toolName
	s.lastArguments = append([]byte(nil), arguments...)
	s.mu.Unlock()
	if s.callErr != nil {
		return CallResult{}, s.callErr
	}
	result := s.callResult
	result.Metadata = cloneSchema(result.Metadata)
	return result, nil
}

func (s *stubServerClient) HealthCheck(ctx context.Context) error {
	return s.healthErr
}

func TestRegistryRegisterRefreshSnapshotCall(t *testing.T) {
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
		callResult: CallResult{
			Content: "ok",
			Metadata: map[string]any{
				"latency_ms": 18,
			},
		},
	}

	if err := registry.RegisterServer("Docs", "stdio", "v1", client); err != nil {
		t.Fatalf("RegisterServer() error = %v", err)
	}
	if err := registry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("RefreshServerTools() error = %v", err)
	}

	snapshots := registry.Snapshot()
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.ServerID != "docs" || snapshot.Status != ServerStatusReady {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if len(snapshot.Tools) != 1 || snapshot.Tools[0].Name != "search" {
		t.Fatalf("unexpected tools in snapshot: %+v", snapshot.Tools)
	}

	result, err := registry.Call(context.Background(), "docs", "search", []byte(`{"q":"mcp"}`))
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("expected call content ok, got %q", result.Content)
	}
}

func TestRegistryStatusTransitions(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &stubServerClient{
		tools: []ToolDescriptor{
			{Name: "search", InputSchema: map[string]any{"type": "object"}},
		},
	}
	if err := registry.RegisterServer("server-1", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}

	client.healthErr = errors.New("offline")
	if err := registry.HealthCheck(context.Background(), "server-1"); err == nil {
		t.Fatalf("expected health check failure")
	}
	if snapshots := registry.Snapshot(); snapshots[0].Status != ServerStatusOffline {
		t.Fatalf("expected offline status, got %+v", snapshots[0].Status)
	}

	client.healthErr = nil
	if err := registry.HealthCheck(context.Background(), "server-1"); err != nil {
		t.Fatalf("unexpected health check error: %v", err)
	}
	if snapshots := registry.Snapshot(); snapshots[0].Status != ServerStatusReady {
		t.Fatalf("expected ready status, got %+v", snapshots[0].Status)
	}
}

func TestRegistryConcurrentSnapshotAndRefresh(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &stubServerClient{
		tools: []ToolDescriptor{
			{Name: "search", InputSchema: map[string]any{"type": "object"}},
		},
	}
	if err := registry.RegisterServer("server-1", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = registry.RefreshServerTools(context.Background(), "server-1")
		}()
	}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = registry.Snapshot()
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("concurrent registry operations timed out")
	}
}

func TestRegistryConcurrentRefreshAndCall(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &stubServerClient{
		tools: []ToolDescriptor{
			{Name: "search", InputSchema: map[string]any{"type": "object"}},
		},
		callResult: CallResult{Content: "ok"},
	}
	if err := registry.RegisterServer("server-1", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}
	if err := registry.RefreshServerTools(context.Background(), "server-1"); err != nil {
		t.Fatalf("refresh tools: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 32)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 16; j++ {
				if err := registry.RefreshServerTools(context.Background(), "server-1"); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 16; j++ {
				result, err := registry.Call(context.Background(), "server-1", "search", []byte(`{"q":"neo"}`))
				if err != nil {
					errCh <- err
					return
				}
				if result.Content != "ok" {
					errCh <- errors.New("unexpected call result")
					return
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("concurrent refresh and call timed out")
	}
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent registry operation failed: %v", err)
	}
}

func TestRegistrySnapshotSchemaIsDeepCloned(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &stubServerClient{
		tools: []ToolDescriptor{
			{
				Name: "search",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type": "string",
						},
					},
				},
			},
		},
	}
	if err := registry.RegisterServer("server-1", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}
	if err := registry.RefreshServerTools(context.Background(), "server-1"); err != nil {
		t.Fatalf("refresh tools: %v", err)
	}

	first := registry.Snapshot()
	properties, ok := first[0].Tools[0].InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map")
	}
	properties["query"] = map[string]any{"type": "number"}

	second := registry.Snapshot()
	secondProperties, ok := second[0].Tools[0].InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map in second snapshot")
	}
	query, ok := secondProperties["query"].(map[string]any)
	if !ok {
		t.Fatalf("expected query schema map")
	}
	if query["type"] != "string" {
		t.Fatalf("expected deep cloned schema type string, got %v", query["type"])
	}
}

func TestRegistryRegisterAndUnregisterBoundaries(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &stubServerClient{}

	if err := registry.RegisterServer("docs", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}
	if err := registry.RegisterServer("docs", "stdio", "v1", client); err == nil {
		t.Fatalf("expected duplicate register error")
	}
	if !registry.UnregisterServer("docs") {
		t.Fatalf("expected unregister success")
	}
	if registry.UnregisterServer("docs") {
		t.Fatalf("expected unregister miss to be false")
	}
}

func TestRegistryUnregisterServerClosesClient(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &closableStubServerClient{}
	if err := registry.RegisterServer("docs", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}
	if !registry.UnregisterServer("docs") {
		t.Fatalf("expected unregister success")
	}
	if !client.closed {
		t.Fatalf("expected client to be closed on unregister")
	}
}

func TestCloseServerClientBoundaries(t *testing.T) {
	t.Parallel()

	closeServerClient(nil)
	closeServerClient(&stubServerClient{})

	client := &closableStubServerClient{}
	closeServerClient(client)
	if !client.closed {
		t.Fatalf("expected closeable client to be closed")
	}
}

func TestRegistrySetServerStatusValidation(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &stubServerClient{}
	if err := registry.RegisterServer("docs", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}
	if err := registry.SetServerStatus("docs", ServerStatus("unknown")); err == nil {
		t.Fatalf("expected invalid status error")
	}
	if err := registry.SetServerStatus("missing", ServerStatusReady); err == nil {
		t.Fatalf("expected missing server error")
	}
}

func TestRegistryNilAndValidationBoundaries(t *testing.T) {
	t.Parallel()

	var nilRegistry *Registry
	if err := nilRegistry.RegisterServer("docs", "stdio", "v1", &stubServerClient{}); err == nil {
		t.Fatalf("expected nil registry error")
	}
	if nilRegistry.UnregisterServer("docs") {
		t.Fatalf("nil registry should return false on unregister")
	}
	if err := nilRegistry.SetServerStatus("docs", ServerStatusReady); err == nil {
		t.Fatalf("expected nil registry error for set status")
	}
	if err := nilRegistry.RefreshServerTools(context.Background(), "docs"); err == nil {
		t.Fatalf("expected nil registry error for refresh")
	}
	if err := nilRegistry.HealthCheck(context.Background(), "docs"); err == nil {
		t.Fatalf("expected nil registry error for health check")
	}
	if _, err := nilRegistry.Call(context.Background(), "docs", "search", nil); err == nil {
		t.Fatalf("expected nil registry error for call")
	}
	if snapshots := nilRegistry.Snapshot(); snapshots != nil {
		t.Fatalf("expected nil snapshots from nil registry")
	}
}

func TestRegistryRefreshHealthCallValidation(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	client := &stubServerClient{}
	if err := registry.RegisterServer("docs", "stdio", "v1", client); err != nil {
		t.Fatalf("register server: %v", err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := registry.RefreshServerTools(canceledCtx, "docs"); err == nil {
		t.Fatalf("expected canceled refresh error")
	}
	if err := registry.HealthCheck(canceledCtx, "docs"); err == nil {
		t.Fatalf("expected canceled health check error")
	}
	if _, err := registry.Call(canceledCtx, "docs", "search", nil); err == nil {
		t.Fatalf("expected canceled call error")
	}

	if err := registry.RefreshServerTools(context.Background(), " "); err == nil {
		t.Fatalf("expected empty server id error")
	}
	if err := registry.HealthCheck(context.Background(), " "); err == nil {
		t.Fatalf("expected empty server id error")
	}
	if _, err := registry.Call(context.Background(), " ", "search", nil); err == nil {
		t.Fatalf("expected empty server id error")
	}
	if _, err := registry.Call(context.Background(), "docs", " ", nil); err == nil {
		t.Fatalf("expected empty tool name error")
	}
}

func TestRegistryCloneAnyCoversSlicesAndMaps(t *testing.T) {
	t.Parallel()

	source := map[string]any{
		"items": []any{
			map[string]any{"name": "a"},
			[]any{"nested"},
		},
	}
	cloned := cloneSchema(source)

	items, ok := cloned["items"].([]any)
	if !ok {
		t.Fatalf("expected []any clone")
	}
	nestedMap, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map clone")
	}
	nestedMap["name"] = "changed"

	originalItems := source["items"].([]any)
	originalMap := originalItems[0].(map[string]any)
	if originalMap["name"] != "a" {
		t.Fatalf("expected deep cloned map, got %v", originalMap["name"])
	}
}
