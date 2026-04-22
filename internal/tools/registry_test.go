package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"neo-code/internal/security"
	"neo-code/internal/tools/mcp"
)

type stubTool struct {
	name        string
	description string
	schema      map[string]any
	policy      MicroCompactPolicy
	result      ToolResult
	err         error
}

func (s stubTool) Name() string        { return s.name }
func (s stubTool) Description() string { return s.description }
func (s stubTool) Schema() map[string]any {
	return s.schema
}
func (s stubTool) MicroCompactPolicy() MicroCompactPolicy {
	return s.policy
}
func (s stubTool) Execute(ctx context.Context, call ToolCallInput) (ToolResult, error) {
	return s.result, s.err
}

func TestRegistryGetSpecsSorted(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{name: "z_tool", description: "last", schema: map[string]any{"type": "object"}})
	registry.Register(stubTool{name: "a_tool", description: "first", schema: map[string]any{"type": "object"}})

	specs := registry.GetSpecs()
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].Name != "a_tool" || specs[1].Name != "z_tool" {
		t.Fatalf("expected sorted specs, got %+v", specs)
	}
}

func TestRegistryExecute(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{
		name:        "ok_tool",
		description: "success tool",
		schema:      map[string]any{"type": "object"},
		result: ToolResult{
			Name:    "ok_tool",
			Content: "done",
		},
	})
	registry.Register(stubTool{
		name:        "error_tool",
		description: "error tool",
		schema:      map[string]any{"type": "object"},
		result: ToolResult{
			Name: "error_tool",
		},
		err: errors.New("boom"),
	})
	registry.Register(stubTool{
		name:        "content_error_tool",
		description: "tool preserves own error content",
		schema:      map[string]any{"type": "object"},
		result: ToolResult{
			Name:    "content_error_tool",
			Content: "explicit failure",
		},
		err: errors.New("boom"),
	})

	tests := []struct {
		name          string
		input         ToolCallInput
		expectErr     string
		expectContent string
		expectIsError bool
	}{
		{
			name: "dispatch success",
			input: ToolCallInput{
				ID:   "call-1",
				Name: "ok_tool",
			},
			expectContent: "done",
		},
		{
			name: "unknown tool",
			input: ToolCallInput{
				ID:   "call-2",
				Name: "missing_tool",
			},
			expectErr:     "tool: not found",
			expectContent: "tool error",
			expectIsError: true,
		},
		{
			name: "tool error falls back to returned error text",
			input: ToolCallInput{
				ID:   "call-3",
				Name: "error_tool",
			},
			expectErr:     "boom",
			expectContent: "tool error",
			expectIsError: true,
		},
		{
			name: "tool error preserves explicit content",
			input: ToolCallInput{
				ID:   "call-4",
				Name: "content_error_tool",
			},
			expectErr:     "boom",
			expectContent: "explicit failure",
			expectIsError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := registry.Execute(context.Background(), tt.input)
			if tt.expectErr != "" {
				if err == nil || err.Error() != tt.expectErr {
					t.Fatalf("expected error %q, got %v", tt.expectErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.ToolCallID != tt.input.ID {
				t.Fatalf("expected tool call id %q, got %q", tt.input.ID, result.ToolCallID)
			}
			if tt.expectContent != "" && !strings.Contains(result.Content, tt.expectContent) {
				t.Fatalf("expected content containing %q, got %q", tt.expectContent, result.Content)
			}
			if result.IsError != tt.expectIsError {
				t.Fatalf("expected IsError=%v, got %v", tt.expectIsError, result.IsError)
			}
		})
	}
}

func TestRegistryHelpers(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(nil)
	registry.Register(stubTool{name: "a_tool", description: "first", schema: map[string]any{"type": "object"}})

	if !registry.Supports("a_tool") {
		t.Fatalf("expected registry to support a_tool")
	}
	if registry.Supports("missing") {
		t.Fatalf("did not expect registry to support missing tool")
	}
	if registry.MicroCompactPolicy("a_tool") != MicroCompactPolicyCompact {
		t.Fatalf("expected compact policy default for a_tool")
	}
	if registry.MicroCompactPolicy("missing") != MicroCompactPolicyCompact {
		t.Fatalf("expected compact policy default for unknown tool")
	}

	schemas := registry.ListSchemas()
	if len(schemas) != 1 || schemas[0].Name != "a_tool" {
		t.Fatalf("unexpected schemas: %+v", schemas)
	}

	specs, err := registry.ListAvailableSpecs(context.Background(), SpecListInput{SessionID: "s-1"})
	if err != nil {
		t.Fatalf("ListAvailableSpecs() error = %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "a_tool" {
		t.Fatalf("unexpected specs: %+v", specs)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = registry.ListAvailableSpecs(canceled, SpecListInput{})
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestRegistryMicroCompactPolicyPreserveHistory(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{
		name:        "custom_tool",
		description: "preserve history",
		schema:      map[string]any{"type": "object"},
		policy:      MicroCompactPolicyPreserveHistory,
	})

	if got := registry.MicroCompactPolicy("custom_tool"); got != MicroCompactPolicyPreserveHistory {
		t.Fatalf("expected preserve history policy, got %q", got)
	}
}

func TestRegistryMicroCompactPolicyNormalizesNameAndNilRegistry(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{
		name:        "Custom_Tool",
		description: "preserve history",
		schema:      map[string]any{"type": "object"},
		policy:      MicroCompactPolicyPreserveHistory,
	})

	if got := registry.MicroCompactPolicy("  custom_tool  "); got != MicroCompactPolicyPreserveHistory {
		t.Fatalf("expected normalized preserve history policy, got %q", got)
	}

	var nilRegistry *Registry
	if got := nilRegistry.MicroCompactPolicy("whatever"); got != MicroCompactPolicyCompact {
		t.Fatalf("expected nil registry default compact policy, got %q", got)
	}
}

func TestRegistryRememberSessionDecisionUnsupported(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	err := registry.RememberSessionDecision("session-1", security.Action{}, SessionPermissionScopeAlways)
	if err == nil {
		t.Fatalf("expected unsupported error")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}

type stubMCPClient struct {
	tools      []mcp.ToolDescriptor
	callResult mcp.CallResult
	callErr    error
}

type stubExposureFilter struct {
	filtered  []mcp.ServerSnapshot
	decisions []mcp.ExposureDecision
	err       error
	inputs    []mcp.ExposureFilterInput
}

func (s *stubExposureFilter) Filter(
	ctx context.Context,
	snapshots []mcp.ServerSnapshot,
	input mcp.ExposureFilterInput,
) ([]mcp.ServerSnapshot, []mcp.ExposureDecision, error) {
	s.inputs = append(s.inputs, input)
	if s.err != nil {
		return nil, nil, s.err
	}
	if s.filtered != nil || s.decisions != nil {
		return s.filtered, s.decisions, nil
	}
	return snapshots, nil, nil
}

func (s *stubMCPClient) ListTools(ctx context.Context) ([]mcp.ToolDescriptor, error) {
	return s.tools, nil
}

func (s *stubMCPClient) CallTool(ctx context.Context, toolName string, arguments []byte) (mcp.CallResult, error) {
	if s.callErr != nil {
		return mcp.CallResult{}, s.callErr
	}
	return s.callResult, nil
}

func (s *stubMCPClient) HealthCheck(ctx context.Context) error {
	return nil
}

func TestRegistryListAvailableSpecsIncludesMCP(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{name: "a_tool", description: "built-in", schema: map[string]any{"type": "object"}})

	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	specs, err := registry.ListAvailableSpecs(context.Background(), SpecListInput{})
	if err != nil {
		t.Fatalf("ListAvailableSpecs() error = %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs (built-in + mcp), got %d", len(specs))
	}
	foundMCP := false
	for _, spec := range specs {
		if spec.Name == "mcp.docs.search" {
			foundMCP = true
			break
		}
	}
	if !foundMCP {
		t.Fatalf("expected mcp.docs.search in specs, got %+v", specs)
	}
}

func TestRegistryExecuteDispatchesToMCPAdapter(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
		callResult: mcp.CallResult{
			Content: "mcp ok",
			Metadata: map[string]any{
				"latency_ms":             12,
				"verification_passed":    true,
				"workspace_write":        true,
				"mcp_server_id":          "override",
				"verification_performed": true,
			},
		},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	result, err := registry.Execute(context.Background(), ToolCallInput{
		ID:        "mcp-call-1",
		Name:      "mcp.docs.search",
		Arguments: []byte(`{"query":"neocode"}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.ToolCallID != "mcp-call-1" {
		t.Fatalf("expected tool call id mcp-call-1, got %q", result.ToolCallID)
	}
	if result.Name != "mcp.docs.search" {
		t.Fatalf("expected mcp tool name, got %q", result.Name)
	}
	if !strings.Contains(result.Content, "mcp ok") {
		t.Fatalf("expected mcp content, got %q", result.Content)
	}
	if result.Metadata["mcp_server_id"] != "docs" || result.Metadata["mcp_tool_name"] != "search" {
		t.Fatalf("unexpected mcp metadata: %+v", result.Metadata)
	}
	if result.Metadata["latency_ms"] != 12 {
		t.Fatalf("expected safe metadata passthrough, got %+v", result.Metadata)
	}
	if _, exists := result.Metadata["workspace_write"]; exists {
		t.Fatalf("expected workspace_write metadata to be filtered, got %+v", result.Metadata)
	}
	if _, exists := result.Metadata["verification_passed"]; exists {
		t.Fatalf("expected verification metadata to be filtered, got %+v", result.Metadata)
	}
}

func TestRegistryExecuteRejectsPolicyDeniedMCPTool(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
		callResult: mcp.CallResult{Content: "should not run"},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)
	registry.SetMCPExposureFilter(mcp.NewExposureFilter(mcp.ExposureFilterConfig{
		Denylist: []string{"docs.search"},
	}))

	result, err := registry.Execute(context.Background(), ToolCallInput{
		ID:   "mcp-call-policy-deny",
		Name: "mcp.docs.search",
	})
	if err == nil || !strings.Contains(err.Error(), "tool: not found") {
		t.Fatalf("expected tool not found for denied tool, got %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError true")
	}
}

func TestRegistryExecuteMCPResolveErrorPropagates(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := registry.Execute(ctx, ToolCallInput{
		ID:   "mcp-call-canceled",
		Name: "mcp.docs.search",
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result")
	}
	if !strings.Contains(result.Content, context.Canceled.Error()) {
		t.Fatalf("expected canceled content, got %q", result.Content)
	}
}

func TestRegistryExecuteMCPCallErrorDoesNotReturnOK(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
		callErr: errors.New("mcp transport timeout"),
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	result, err := registry.Execute(context.Background(), ToolCallInput{
		ID:        "mcp-call-error",
		Name:      "mcp.docs.search",
		Arguments: []byte(`{"query":"neocode"}`),
	})
	if err == nil {
		t.Fatalf("expected mcp call error")
	}
	if !result.IsError {
		t.Fatalf("expected IsError true")
	}
	if strings.TrimSpace(result.Content) == "" || strings.EqualFold(strings.TrimSpace(result.Content), "ok") {
		t.Fatalf("expected non-ok error content, got %q", result.Content)
	}
}

func TestRegistryExecuteMCPIsErrorResultDoesNotFallbackToOK(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
		callResult: mcp.CallResult{
			Content: "",
			IsError: true,
		},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	result, err := registry.Execute(context.Background(), ToolCallInput{
		ID:   "mcp-call-iserror",
		Name: "mcp.docs.search",
	})
	if err == nil {
		t.Fatalf("expected mcp isError to return error")
	}
	if !result.IsError {
		t.Fatalf("expected IsError true")
	}
	if strings.EqualFold(strings.TrimSpace(result.Content), "ok") || strings.TrimSpace(result.Content) == "" {
		t.Fatalf("expected non-ok error content, got %q", result.Content)
	}
}

func TestRegistryExecuteMCPIsErrorWithContentKeepsContent(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
		callResult: mcp.CallResult{
			Content: "explicit mcp error",
			IsError: true,
		},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	result, err := registry.Execute(context.Background(), ToolCallInput{
		ID:   "mcp-call-iserror-content",
		Name: "mcp.docs.search",
	})
	if err == nil {
		t.Fatalf("expected mcp isError to return error")
	}
	if !result.IsError {
		t.Fatalf("expected IsError true")
	}
	if result.Content != "explicit mcp error" {
		t.Fatalf("expected explicit error content preserved, got %q", result.Content)
	}
}

func TestRegistrySupportsMCPToolAndHelpers(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	if !registry.Supports("mcp.docs.search") {
		t.Fatalf("expected supports mcp.docs.search")
	}
	if registry.Supports("mcp.docs.missing") {
		t.Fatalf("did not expect supports mcp.docs.missing")
	}
	if registry.Supports("search") {
		t.Fatalf("did not expect supports non-prefixed mcp name")
	}

	snapshots := registry.mcpFactoryBuildSnapshot()
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(snapshots))
	}
	if got := mcpToolFullName(" Docs ", " Search "); got != "mcp.docs.search" {
		t.Fatalf("unexpected mcp full name: %q", got)
	}
}

func TestRegistryListAvailableSpecsAppliesMCPExposureFilter(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{name: "a_tool", description: "built-in", schema: map[string]any{"type": "object"}})

	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RegisterServer("admin", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "secret", Description: "secret tool", InputSchema: map[string]any{"type": "object"}},
		},
	}); err != nil {
		t.Fatalf("register admin server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh docs tools: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "admin"); err != nil {
		t.Fatalf("refresh admin tools: %v", err)
	}

	registry.SetMCPRegistry(mcpRegistry)
	registry.SetMCPExposureFilter(mcp.NewExposureFilter(mcp.ExposureFilterConfig{
		Allowlist: []string{"docs"},
		Denylist:  []string{"admin.secret"},
	}))

	specs, err := registry.ListAvailableSpecs(context.Background(), SpecListInput{Agent: "coder", SessionID: "s-1"})
	if err != nil {
		t.Fatalf("ListAvailableSpecs() error = %v", err)
	}

	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	if strings.Join(names, ",") != "a_tool,mcp.docs.search" {
		t.Fatalf("unexpected specs: %+v", names)
	}

	audit := registry.MCPExposureAuditSnapshot()
	if len(audit) != 2 {
		t.Fatalf("expected 2 audit decisions, got %+v", audit)
	}
	if audit[0].ToolFullName != "mcp.admin.secret" || audit[0].Reason != mcp.ExposureFilterReasonPolicyDeny {
		t.Fatalf("unexpected audit[0]: %+v", audit[0])
	}
}

func TestRegistryListAvailableSpecsFailClosedOnExposureFilterError(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{name: "builtin", description: "built-in", schema: map[string]any{"type": "object"}})

	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}

	registry.SetMCPRegistry(mcpRegistry)
	registry.SetMCPExposureFilter(&stubExposureFilter{err: errors.New("boom")})

	specs, err := registry.ListAvailableSpecs(context.Background(), SpecListInput{})
	if err != nil {
		t.Fatalf("ListAvailableSpecs() error = %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "builtin" {
		t.Fatalf("expected built-in specs only, got %+v", specs)
	}

	audit := registry.MCPExposureAuditSnapshot()
	if len(audit) != 1 || audit[0].Reason != mcp.ExposureFilterReasonFilterError {
		t.Fatalf("expected filter_error audit, got %+v", audit)
	}
}

func TestRegistryListAvailableSpecsReturnsContextErrorFromExposureFilter(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{name: "builtin", description: "built-in", schema: map[string]any{"type": "object"}})

	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}

	registry.SetMCPRegistry(mcpRegistry)
	registry.SetMCPExposureFilter(&stubExposureFilter{err: context.Canceled})

	_, err := registry.ListAvailableSpecs(context.Background(), SpecListInput{})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestRegistryListAvailableSpecsPassesAgentAndQueryToExposureFilter(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}

	filter := &stubExposureFilter{}
	registry.SetMCPRegistry(mcpRegistry)
	registry.SetMCPExposureFilter(filter)

	_, err := registry.ListAvailableSpecs(context.Background(), SpecListInput{
		SessionID: "session-1",
		Agent:     "planner",
		Query:     "find docs",
	})
	if err != nil {
		t.Fatalf("ListAvailableSpecs() error = %v", err)
	}
	if len(filter.inputs) != 1 {
		t.Fatalf("expected one filter input, got %+v", filter.inputs)
	}
	if filter.inputs[0].SessionID != "session-1" || filter.inputs[0].Agent != "planner" || filter.inputs[0].Query != "find docs" {
		t.Fatalf("unexpected filter input: %+v", filter.inputs[0])
	}
}

func TestParseMCPToolFullName(t *testing.T) {
	t.Parallel()

	serverID, toolName, ok := parseMCPToolFullName("  MCP.Docs.Search  ")
	if !ok {
		t.Fatalf("expected parse success")
	}
	if serverID != "docs" || toolName != "search" {
		t.Fatalf("unexpected parse result: server=%q tool=%q", serverID, toolName)
	}

	if _, _, ok := parseMCPToolFullName("mcp.docs"); ok {
		t.Fatalf("expected parse failure for incomplete name")
	}
}

func TestRegistryMCPFaultDoesNotAffectBuiltInTools(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(stubTool{
		name:        "builtin_tool",
		description: "built in",
		schema:      map[string]any{"type": "object"},
		result: ToolResult{
			Name:    "builtin_tool",
			Content: "builtin ok",
		},
	})

	mcpRegistry := mcp.NewRegistry()
	if err := mcpRegistry.RegisterServer("docs", "stdio", "v1", &stubMCPClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
		callErr: errors.New("mcp transport timeout"),
	}); err != nil {
		t.Fatalf("register mcp server: %v", err)
	}
	if err := mcpRegistry.RefreshServerTools(context.Background(), "docs"); err != nil {
		t.Fatalf("refresh mcp tools: %v", err)
	}
	if err := mcpRegistry.SetServerStatus("docs", mcp.ServerStatusOffline); err != nil {
		t.Fatalf("set server offline: %v", err)
	}
	registry.SetMCPRegistry(mcpRegistry)

	specs, err := registry.ListAvailableSpecs(context.Background(), SpecListInput{})
	if err != nil {
		t.Fatalf("ListAvailableSpecs() error = %v", err)
	}
	if len(specs) != 1 || specs[0].Name != "builtin_tool" {
		t.Fatalf("expected built-in tool only, got %+v", specs)
	}

	result, err := registry.Execute(context.Background(), ToolCallInput{
		ID:   "builtin-call",
		Name: "builtin_tool",
	})
	if err != nil {
		t.Fatalf("builtin Execute() error = %v", err)
	}
	if result.Content != "builtin ok" {
		t.Fatalf("expected builtin execution success, got %+v", result)
	}
}
