package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	"neo-code/internal/memo"
	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	agentruntime "neo-code/internal/runtime"
	agentsession "neo-code/internal/session"
	"neo-code/internal/skills"
	"neo-code/internal/tools"
	"neo-code/internal/tools/mcp"
	"neo-code/internal/tui"
)

func TestNewProgram(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	program, cleanup, err := NewProgram(context.Background(), BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewProgram() error = %v", err)
	}
	if program == nil {
		t.Fatalf("expected tea program")
	}
	if cleanup != nil {
		defer cleanup()
	}

	configPath := filepath.Join(home, ".neocode", "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be created at %q: %v", configPath, err)
	}
}

func TestNewProgramNormalizesInvalidCurrentModelOnStartup(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".neocode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	raw := []byte("selected_provider: openai\ncurrent_model: unsupported-current\nshell: powershell\n")
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	program, cleanup, err := NewProgram(context.Background(), BootstrapOptions{})
	if err != nil {
		t.Fatalf("NewProgram() error = %v", err)
	}
	if program == nil {
		t.Fatalf("expected tea program")
	}
	if cleanup != nil {
		defer cleanup()
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "current_model: "+config.OpenAIDefaultModel) {
		t.Fatalf("expected startup normalization to rewrite current_model, got:\n%s", string(data))
	}
}

func TestBuildRuntimeRejectsUnsupportedSelectedProviderDriverOnStartup(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".neocode")
	providerDir := filepath.Join(configDir, "providers", "company-gateway")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("mkdir provider dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	rawConfig := []byte("selected_provider: company-gateway\ncurrent_model: claude-3-7-sonnet\nshell: powershell\n")
	if err := os.WriteFile(configPath, rawConfig, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rawProvider := []byte(
		"name: company-gateway\n" +
			"driver: anthropic\n" +
			"api_key_env: COMPANY_GATEWAY_API_KEY\n" +
			"anthropic:\n" +
			"  base_url: https://api.anthropic.example/v1\n" +
			"  api_version: 2023-06-01\n",
	)
	if err := os.WriteFile(filepath.Join(providerDir, "provider.yaml"), rawProvider, 0o644); err != nil {
		t.Fatalf("write provider config: %v", err)
	}

	_, err := BuildRuntime(context.Background(), BootstrapOptions{})
	if !errors.Is(err, configstate.ErrDriverUnsupported) {
		t.Fatalf("expected ErrDriverUnsupported, got %v", err)
	}

	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if !strings.Contains(string(data), "selected_provider: company-gateway") {
		t.Fatalf("expected selected_provider to remain unchanged, got:\n%s", string(data))
	}
}

func TestBuildToolRegistryUsesWebFetchConfig(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("1234567890"))
	}))
	defer server.Close()

	cfg := config.StaticDefaults().Clone()
	cfg.Workdir = t.TempDir()
	cfg.Tools.WebFetch.MaxResponseBytes = 4

	registry, cleanup, err := buildToolRegistry(cfg)
	if err != nil {
		t.Fatalf("buildToolRegistry() error = %v", err)
	}
	if cleanup != nil {
		defer cleanup()
	}
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

func TestBuildMCPRegistryFromConfig(t *testing.T) {
	stubClient := &stubMCPServerClient{
		tools: []mcp.ToolDescriptor{
			{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
		},
	}

	cfg := config.StaticDefaults().Clone()
	cfg.Workdir = t.TempDir()
	cfg.Tools.MCP.Servers = []config.MCPServerConfig{
		{
			ID:      "docs",
			Enabled: true,
			Source:  "stdio",
			Stdio: config.MCPStdioConfig{
				Command: "mock",
			},
		},
	}

	originalRegister := registerMCPStdioServer
	t.Cleanup(func() { registerMCPStdioServer = originalRegister })
	registerMCPStdioServer = func(registry *mcp.Registry, cfg config.Config, server config.MCPServerConfig) error {
		if err := registry.RegisterServer(server.ID, "stdio", server.Version, stubClient); err != nil {
			return err
		}
		return registry.RefreshServerTools(context.Background(), server.ID)
	}

	registry, err := buildMCPRegistry(cfg)
	if err != nil {
		t.Fatalf("buildMCPRegistry() error = %v", err)
	}
	if registry == nil {
		t.Fatalf("expected non-nil mcp registry")
	}
	snapshots := registry.Snapshot()
	if len(snapshots) != 1 || snapshots[0].ServerID != "docs" {
		t.Fatalf("unexpected snapshots: %+v", snapshots)
	}
}

func TestBuildMCPRegistryUnsupportedSource(t *testing.T) {
	t.Parallel()

	cfg := config.StaticDefaults().Clone()
	cfg.Workdir = t.TempDir()
	cfg.Tools.MCP.Servers = []config.MCPServerConfig{
		{
			ID:      "docs",
			Enabled: true,
			Source:  "sse",
			Stdio: config.MCPStdioConfig{
				Command: "mock",
			},
		},
	}

	registry, err := buildMCPRegistry(cfg)
	if err == nil {
		t.Fatalf("expected unsupported source error")
	}
	if registry != nil {
		t.Fatalf("expected nil registry when source unsupported")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unsupported mcp source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultRegisterMCPStdioServerSuccess(t *testing.T) {
	t.Parallel()

	registry := mcp.NewRegistry()
	cfg := config.StaticDefaults().Clone()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cfg.Workdir = wd
	cfg.ToolTimeoutSec = 9

	server := config.MCPServerConfig{
		ID:      "docs",
		Enabled: true,
		Source:  "stdio",
		Version: "v1",
		Stdio: config.MCPStdioConfig{
			Command:         os.Args[0],
			Args:            []string{"-test.run=TestHelperProcessAppMCPStdioServer", "--"},
			Workdir:         "",
			StartTimeoutSec: 3,
			CallTimeoutSec:  3,
		},
		Env: []config.MCPEnvVarConfig{
			{Name: "MODE", Value: "test"},
			{Name: "GO_WANT_APP_MCP_STDIO_HELPER", Value: "1"},
		},
	}
	t.Cleanup(func() { _ = registry.UnregisterServer("docs") })

	if err := defaultRegisterMCPStdioServer(registry, cfg, server); err != nil {
		t.Fatalf("defaultRegisterMCPStdioServer() error = %v", err)
	}

	snapshots := registry.Snapshot()
	if len(snapshots) != 1 || snapshots[0].ServerID != "docs" {
		t.Fatalf("unexpected snapshots: %+v", snapshots)
	}
	if len(snapshots[0].Tools) != 1 || snapshots[0].Tools[0].Name != "search" {
		t.Fatalf("unexpected tools snapshot: %+v", snapshots[0].Tools)
	}
}

func TestDefaultRegisterMCPStdioServerRefreshFailure(t *testing.T) {
	t.Parallel()

	registry := mcp.NewRegistry()
	cfg := config.StaticDefaults().Clone()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cfg.Workdir = wd

	server := config.MCPServerConfig{
		ID:      "broken",
		Enabled: true,
		Source:  "stdio",
		Stdio: config.MCPStdioConfig{
			Command:         os.Args[0],
			Args:            []string{"-test.run=TestHelperProcessAppMCPStdioServer", "--"},
			StartTimeoutSec: 3,
			CallTimeoutSec:  3,
		},
		Env: []config.MCPEnvVarConfig{
			{Name: "GO_WANT_APP_MCP_STDIO_HELPER", Value: "1"},
			{Name: "GO_APP_MCP_STDIO_LIST_FAIL", Value: "1"},
		},
	}
	t.Cleanup(func() { _ = registry.UnregisterServer("broken") })

	err = defaultRegisterMCPStdioServer(registry, cfg, server)
	if err == nil {
		t.Fatalf("expected refresh failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "list tools failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshots := registry.Snapshot(); len(snapshots) != 0 {
		t.Fatalf("expected failed registration to rollback server, got %+v", snapshots)
	}
}

func TestBuildToolRegistryIncludesMCPFromConfig(t *testing.T) {
	cfg := config.StaticDefaults().Clone()
	cfg.Workdir = t.TempDir()
	cfg.Tools.MCP.Servers = []config.MCPServerConfig{
		{
			ID:      "docs",
			Enabled: true,
			Source:  "stdio",
			Stdio: config.MCPStdioConfig{
				Command: "mock",
			},
		},
	}

	originalRegister := registerMCPStdioServer
	t.Cleanup(func() { registerMCPStdioServer = originalRegister })
	registerMCPStdioServer = func(registry *mcp.Registry, cfg config.Config, server config.MCPServerConfig) error {
		client := &stubMCPServerClient{
			tools: []mcp.ToolDescriptor{
				{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
			},
		}
		if err := registry.RegisterServer(server.ID, "stdio", server.Version, client); err != nil {
			return err
		}
		return registry.RefreshServerTools(context.Background(), server.ID)
	}

	registry, cleanup, err := buildToolRegistry(cfg)
	if err != nil {
		t.Fatalf("buildToolRegistry() error = %v", err)
	}
	if cleanup != nil {
		defer cleanup()
	}
	specs, err := registry.ListAvailableSpecs(context.Background(), tools.SpecListInput{})
	if err != nil {
		t.Fatalf("ListAvailableSpecs() error = %v", err)
	}
	found := false
	for _, spec := range specs {
		if spec.Name == "mcp.docs.search" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected mcp.docs.search in specs, got %+v", specs)
	}
}

func TestBuildToolRegistryAppliesMCPExposureConfig(t *testing.T) {
	cfg := config.StaticDefaults().Clone()
	cfg.Workdir = t.TempDir()
	cfg.Tools.MCP.Servers = []config.MCPServerConfig{
		{
			ID:      "docs",
			Enabled: true,
			Source:  "stdio",
			Stdio: config.MCPStdioConfig{
				Command: "mock",
			},
		},
		{
			ID:      "admin",
			Enabled: true,
			Source:  "stdio",
			Stdio: config.MCPStdioConfig{
				Command: "mock",
			},
		},
	}
	cfg.Tools.MCP.Exposure = config.MCPExposureConfig{
		Allowlist: []string{"docs"},
		Agents: []config.MCPAgentExposureConfig{
			{Agent: "planner", Allowlist: []string{"docs.search"}},
		},
	}

	originalRegister := registerMCPStdioServer
	t.Cleanup(func() { registerMCPStdioServer = originalRegister })
	registerMCPStdioServer = func(registry *mcp.Registry, cfg config.Config, server config.MCPServerConfig) error {
		client := &stubMCPServerClient{
			tools: []mcp.ToolDescriptor{
				{Name: "search", Description: "search docs", InputSchema: map[string]any{"type": "object"}},
			},
		}
		if err := registry.RegisterServer(server.ID, "stdio", server.Version, client); err != nil {
			return err
		}
		return registry.RefreshServerTools(context.Background(), server.ID)
	}

	registry, cleanup, err := buildToolRegistry(cfg)
	if err != nil {
		t.Fatalf("buildToolRegistry() error = %v", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	specs, err := registry.ListAvailableSpecs(context.Background(), tools.SpecListInput{Agent: "planner"})
	if err != nil {
		t.Fatalf("ListAvailableSpecs() error = %v", err)
	}

	foundDocs := false
	foundAdmin := false
	for _, spec := range specs {
		if spec.Name == "mcp.docs.search" {
			foundDocs = true
		}
		if spec.Name == "mcp.admin.search" {
			foundAdmin = true
		}
	}
	if !foundDocs || foundAdmin {
		t.Fatalf("expected only docs MCP tool to be exposed, got %+v", specs)
	}
}

func TestBuildToolRegistryReturnsMCPSourceError(t *testing.T) {
	t.Parallel()

	cfg := config.StaticDefaults().Clone()
	cfg.Workdir = t.TempDir()
	cfg.Tools.MCP.Servers = []config.MCPServerConfig{
		{
			ID:      "docs",
			Enabled: true,
			Source:  "sse",
		},
	}

	_, cleanup, err := buildToolRegistry(cfg)
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unsupported mcp source") {
		t.Fatalf("expected unsupported mcp source error, got %v", err)
	}
}

func TestResolveMCPServerEnvAndWorkdir(t *testing.T) {
	t.Setenv("MCP_TOKEN", "secret")
	env, err := resolveMCPServerEnv(config.MCPServerConfig{
		Env: []config.MCPEnvVarConfig{
			{Name: "TOKEN", ValueEnv: "MCP_TOKEN"},
			{Name: "MODE", Value: "test"},
		},
	})
	if err != nil {
		t.Fatalf("resolveMCPServerEnv() error = %v", err)
	}
	joined := strings.Join(env, ",")
	if !strings.Contains(joined, "TOKEN=secret") || !strings.Contains(joined, "MODE=test") {
		t.Fatalf("unexpected env result: %+v", env)
	}

	base := t.TempDir()
	relative := resolveMCPServerWorkdir(base, "tools/mcp")
	if !strings.HasSuffix(filepath.ToSlash(relative), "tools/mcp") {
		t.Fatalf("unexpected relative workdir: %q", relative)
	}
	absoluteTarget := filepath.Join(t.TempDir(), "absolute")
	absolute := resolveMCPServerWorkdir(base, absoluteTarget)
	if absolute != filepath.Clean(absoluteTarget) {
		t.Fatalf("unexpected absolute workdir: %q", absolute)
	}
}

func TestResolveMCPServerEnvValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		server config.MCPServerConfig
	}{
		{
			name: "empty name",
			server: config.MCPServerConfig{
				Env: []config.MCPEnvVarConfig{{Name: " ", Value: "x"}},
			},
		},
		{
			name: "both value and value_env",
			server: config.MCPServerConfig{
				Env: []config.MCPEnvVarConfig{{Name: "A", Value: "x", ValueEnv: "B"}},
			},
		},
		{
			name: "missing value and value_env",
			server: config.MCPServerConfig{
				Env: []config.MCPEnvVarConfig{{Name: "A"}},
			},
		},
		{
			name: "value_env unresolved",
			server: config.MCPServerConfig{
				Env: []config.MCPEnvVarConfig{{Name: "A", ValueEnv: "MISSING_ENV_FOR_TEST"}},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := resolveMCPServerEnv(tt.server); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestBuildMCPRegistryNoEnabledServerReturnsNil(t *testing.T) {
	t.Parallel()

	cfg := config.StaticDefaults().Clone()
	cfg.Workdir = t.TempDir()
	cfg.Tools.MCP.Servers = []config.MCPServerConfig{
		{ID: "docs", Enabled: false, Source: "stdio"},
	}

	registry, err := buildMCPRegistry(cfg)
	if err != nil {
		t.Fatalf("buildMCPRegistry() error = %v", err)
	}
	if registry != nil {
		t.Fatalf("expected nil registry when no enabled server")
	}
}

func TestBuildMCPRegistryRegisterError(t *testing.T) {
	cfg := config.StaticDefaults().Clone()
	cfg.Workdir = t.TempDir()
	cfg.Tools.MCP.Servers = []config.MCPServerConfig{
		{ID: "docs", Enabled: true, Source: "stdio"},
	}

	originalRegister := registerMCPStdioServer
	t.Cleanup(func() { registerMCPStdioServer = originalRegister })
	registerMCPStdioServer = func(registry *mcp.Registry, cfg config.Config, server config.MCPServerConfig) error {
		return errors.New("register failed")
	}

	_, err := buildMCPRegistry(cfg)
	if err == nil || !strings.Contains(err.Error(), "register failed") {
		t.Fatalf("expected wrapped register error, got %v", err)
	}
}

func TestBuildMCPRegistryRollbackRegisteredServersOnFailure(t *testing.T) {
	cfg := config.StaticDefaults().Clone()
	cfg.Workdir = t.TempDir()
	cfg.Tools.MCP.Servers = []config.MCPServerConfig{
		{ID: "docs", Enabled: true, Source: "stdio"},
		{ID: "search", Enabled: true, Source: "stdio"},
	}

	closedByServer := map[string]*bool{
		"docs":   new(bool),
		"search": new(bool),
	}

	originalRegister := registerMCPStdioServer
	t.Cleanup(func() { registerMCPStdioServer = originalRegister })
	registerMCPStdioServer = func(registry *mcp.Registry, cfg config.Config, server config.MCPServerConfig) error {
		client := &closeableStubMCPServerClient{closed: closedByServer[strings.TrimSpace(server.ID)]}
		if err := registry.RegisterServer(server.ID, "stdio", server.Version, client); err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(server.ID), "search") {
			return errors.New("search register failed")
		}
		return nil
	}

	registry, err := buildMCPRegistry(cfg)
	if err == nil || !strings.Contains(err.Error(), "search register failed") {
		t.Fatalf("expected wrapped register error, got %v", err)
	}
	if registry != nil {
		t.Fatalf("expected nil registry on build failure")
	}
	if !*closedByServer["docs"] || !*closedByServer["search"] {
		t.Fatalf("expected rollback to close all registered servers, got %+v", closedByServer)
	}
}

func TestRollbackMCPServersBoundaries(t *testing.T) {
	t.Parallel()

	rollbackMCPServers(nil, []string{"docs"})
	rollbackMCPServers(mcp.NewRegistry(), nil)
}

func TestInitialMCPRefreshTimeoutAndDurationConversion(t *testing.T) {
	t.Parallel()

	cfg := config.StaticDefaults().Clone()
	cfg.ToolTimeoutSec = 1
	timeout := initialMCPRefreshTimeout(cfg)
	if timeout < 5*time.Second {
		t.Fatalf("expected minimum timeout >= 5s, got %v", timeout)
	}
	if durationFromSeconds(0) != 0 {
		t.Fatalf("expected zero duration for non-positive input")
	}
	if durationFromSeconds(2) != 2*time.Second {
		t.Fatalf("expected 2s duration")
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

	_, execErr := manager.Execute(context.Background(), tools.ToolCallInput{
		Name:      "bash",
		Arguments: []byte(`{"command":"echo hi"}`),
		Workdir:   workdir,
	})
	if execErr == nil {
		t.Fatalf("expected bash to require approval by default policy")
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

func TestBuildToolManagerAllowsWebfetchWhitelist(t *testing.T) {
	t.Parallel()

	registry := tools.NewRegistry()
	registry.Register(stubToolForBootstrap{name: "webfetch", content: "ok"})
	manager, err := buildToolManager(registry)
	if err != nil {
		t.Fatalf("buildToolManager() error = %v", err)
	}

	result, execErr := manager.Execute(context.Background(), tools.ToolCallInput{
		Name:      "webfetch",
		Arguments: []byte(`{"url":"https://github.com/1024XEngineer/neo-code"}`),
		Workdir:   t.TempDir(),
	})
	if execErr != nil {
		t.Fatalf("expected whitelist webfetch allow, got %v", execErr)
	}
	if result.Content != "ok" {
		t.Fatalf("expected ok result, got %+v", result)
	}
}

func TestBuildRuntimeUsesWorkdirOverride(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	override := filepath.Join(t.TempDir(), "中文工作区")
	if err := os.MkdirAll(override, 0o755); err != nil {
		t.Fatalf("mkdir override workdir: %v", err)
	}

	bundle, err := BuildRuntime(context.Background(), BootstrapOptions{Workdir: override})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	if bundle.Config.Workdir != filepath.Clean(override) {
		t.Fatalf("expected workdir %q, got %q", filepath.Clean(override), bundle.Config.Workdir)
	}
	if bundle.ConfigManager == nil || bundle.Runtime == nil || bundle.ProviderSelection == nil {
		t.Fatalf("expected runtime bundle dependencies, got %+v", bundle)
	}
}

func TestBuildRuntimeSucceedsWhenSkillsRootMissing(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	bundle, err := BuildRuntime(context.Background(), BootstrapOptions{})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}
	if bundle.Runtime == nil {
		t.Fatalf("expected runtime bundle to be created")
	}

	runtimeWithSkills, ok := bundle.Runtime.(interface {
		ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error
	})
	if !ok {
		t.Fatalf("expected runtime to expose ActivateSessionSkill")
	}

	store := agentsession.NewStore(bundle.ConfigManager.BaseDir(), bundle.Config.Workdir)
	session := agentsession.New("missing root session")
	if err := store.Save(context.Background(), &session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	err = runtimeWithSkills.ActivateSessionSkill(context.Background(), session.ID, "missing")
	if !errors.Is(err, skills.ErrSkillNotFound) {
		t.Fatalf("expected ErrSkillNotFound with empty catalog, got %v", err)
	}
}

func TestBuildRuntimeInjectsSkillsRegistryWhenRootExists(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	skillsRoot := filepath.Join(home, ".neocode", "skills", "go-review")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("mkdir skills root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsRoot, "SKILL.md"), []byte(strings.Join([]string{
		"---",
		"id: go-review",
		"name: go-review",
		"---",
		"",
		"# Go Review",
		"",
		"Review code carefully.",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	bundle, err := BuildRuntime(context.Background(), BootstrapOptions{})
	if err != nil {
		t.Fatalf("BuildRuntime() error = %v", err)
	}

	store := agentsession.NewStore(bundle.ConfigManager.BaseDir(), bundle.Config.Workdir)
	session := agentsession.New("skill session")
	if err := store.Save(context.Background(), &session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	runtimeWithSkills, ok := bundle.Runtime.(interface {
		ActivateSessionSkill(ctx context.Context, sessionID string, skillID string) error
	})
	if !ok {
		t.Fatalf("expected runtime to expose ActivateSessionSkill")
	}
	if err := runtimeWithSkills.ActivateSessionSkill(context.Background(), session.ID, "go-review"); err != nil {
		t.Fatalf("ActivateSessionSkill() error = %v", err)
	}

	loaded, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if got := loaded.ActiveSkillIDs(); len(got) != 1 || got[0] != "go-review" {
		t.Fatalf("expected activated skill persisted through injected registry, got %+v", got)
	}
}

func TestBuildSkillsRegistryKeepsInstanceWhenRefreshFails(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	registry := buildSkillsRegistry(canceledCtx, baseDir)
	if registry == nil {
		t.Fatalf("expected non-nil registry even when refresh fails")
	}
	_, _, err := registry.Get(context.Background(), "missing")
	if !errors.Is(err, skills.ErrSkillNotFound) {
		t.Fatalf("expected empty catalog behavior, got %v", err)
	}
}

func TestBuildRuntimeRejectsInvalidWorkdirOverride(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	invalid := filepath.Join(t.TempDir(), "missing", "中文")
	_, err := BuildRuntime(context.Background(), BootstrapOptions{Workdir: invalid})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "resolve workdir") {
		t.Fatalf("expected resolve workdir error, got %v", err)
	}
}

func TestBuildRuntimeRejectsInvalidConfigFile(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".neocode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("workdir: legacy\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := BuildRuntime(context.Background(), BootstrapOptions{})
	if err == nil || !strings.Contains(err.Error(), "workdir not found") {
		t.Fatalf("expected legacy config error, got %v", err)
	}
}

func TestBuildRuntimeRejectsUnsupportedMCPSource(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".neocode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.yaml")
	raw := []byte(strings.Join([]string{
		"selected_provider: openai",
		"current_model: " + config.OpenAIDefaultModel,
		"shell: powershell",
		"tools:",
		"  mcp:",
		"    servers:",
		"      - id: docs",
		"        enabled: true",
		"        source: sse",
	}, "\n") + "\n")
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := BuildRuntime(context.Background(), BootstrapOptions{})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not supported") {
		t.Fatalf("expected unsupported mcp source validation error, got %v", err)
	}
}

func TestBuildRuntimeCleansResourcesWhenToolManagerBuildFails(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".neocode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.yaml")
	raw := []byte(strings.Join([]string{
		"selected_provider: openai",
		"current_model: " + config.OpenAIDefaultModel,
		"shell: powershell",
		"tools:",
		"  mcp:",
		"    servers:",
		"      - id: docs",
		"        enabled: true",
		"        source: stdio",
		"        stdio:",
		"          command: mock",
	}, "\n") + "\n")
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	closed := false
	originalRegister := registerMCPStdioServer
	t.Cleanup(func() { registerMCPStdioServer = originalRegister })
	registerMCPStdioServer = func(registry *mcp.Registry, cfg config.Config, server config.MCPServerConfig) error {
		client := &closeableStubMCPServerClient{closed: &closed}
		return registry.RegisterServer(server.ID, "stdio", server.Version, client)
	}

	originalBuildToolManager := buildToolManagerFunc
	t.Cleanup(func() { buildToolManagerFunc = originalBuildToolManager })
	buildToolManagerFunc = func(registry *tools.Registry) (tools.Manager, error) {
		return nil, errors.New("build tool manager failed")
	}

	_, err := BuildRuntime(context.Background(), BootstrapOptions{})
	if err == nil || !strings.Contains(err.Error(), "build tool manager failed") {
		t.Fatalf("expected tool manager build error, got %v", err)
	}
	if !closed {
		t.Fatalf("expected MCP resources to be closed on BuildRuntime failure")
	}
}

func TestNewProgramCleansResourcesWhenTUIBuildFails(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".neocode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.yaml")
	raw := []byte(strings.Join([]string{
		"selected_provider: openai",
		"current_model: " + config.OpenAIDefaultModel,
		"shell: powershell",
		"tools:",
		"  mcp:",
		"    servers:",
		"      - id: docs",
		"        enabled: true",
		"        source: stdio",
		"        stdio:",
		"          command: mock",
	}, "\n") + "\n")
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	closed := false
	originalRegister := registerMCPStdioServer
	t.Cleanup(func() { registerMCPStdioServer = originalRegister })
	registerMCPStdioServer = func(registry *mcp.Registry, cfg config.Config, server config.MCPServerConfig) error {
		client := &closeableStubMCPServerClient{closed: &closed}
		return registry.RegisterServer(server.ID, "stdio", server.Version, client)
	}

	originalNewTUIWithMemo := newTUIWithMemo
	t.Cleanup(func() { newTUIWithMemo = originalNewTUIWithMemo })
	newTUIWithMemo = func(
		cfg *config.Config,
		configManager *config.Manager,
		runtime agentruntime.Runtime,
		providerSvc tui.ProviderController,
		memoSvc *memo.Service,
	) (tui.App, error) {
		return tui.App{}, errors.New("tui init failed")
	}

	_, cleanup, err := NewProgram(context.Background(), BootstrapOptions{})
	if cleanup != nil {
		t.Fatalf("expected nil cleanup on NewProgram failure")
	}
	if err == nil || !strings.Contains(err.Error(), "tui init failed") {
		t.Fatalf("expected tui init error, got %v", err)
	}
	if !closed {
		t.Fatalf("expected MCP resources to be closed when NewProgram fails")
	}
}

func TestNewProgramRejectsInvalidWorkdirOverride(t *testing.T) {
	disableBuiltinProviderAPIKeys(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	_, cleanup, err := NewProgram(context.Background(), BootstrapOptions{Workdir: filepath.Join(t.TempDir(), "missing", "中文")})
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "resolve workdir") {
		t.Fatalf("expected invalid workdir error, got %v", err)
	}
}

func TestResolveBootstrapWorkdirRejectsEmptyAndFile(t *testing.T) {
	if _, err := resolveBootstrapWorkdir("   "); err == nil || !strings.Contains(err.Error(), "workdir is empty") {
		t.Fatalf("expected empty workdir error, got %v", err)
	}

	filePath := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := resolveBootstrapWorkdir(filePath); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("expected file path error, got %v", err)
	}
}

func TestEnsureConsoleUTF8SetsOutputThenInput(t *testing.T) {
	originalOutput := setConsoleOutputCodePage
	originalInput := setConsoleInputCodePage
	t.Cleanup(func() {
		setConsoleOutputCodePage = originalOutput
		setConsoleInputCodePage = originalInput
	})

	calls := make([]string, 0, 2)
	setConsoleOutputCodePage = func(codePage uint32) error {
		if codePage != utf8CodePage {
			t.Fatalf("expected utf8 code page %d, got %d", utf8CodePage, codePage)
		}
		calls = append(calls, "output")
		return nil
	}
	setConsoleInputCodePage = func(codePage uint32) error {
		if codePage != utf8CodePage {
			t.Fatalf("expected utf8 code page %d, got %d", utf8CodePage, codePage)
		}
		calls = append(calls, "input")
		return nil
	}

	EnsureConsoleUTF8()

	if len(calls) != 2 || calls[0] != "output" || calls[1] != "input" {
		t.Fatalf("expected output->input order, got %+v", calls)
	}
}

func TestEnsureConsoleUTF8SkipsInputWhenOutputFails(t *testing.T) {
	originalOutput := setConsoleOutputCodePage
	originalInput := setConsoleInputCodePage
	t.Cleanup(func() {
		setConsoleOutputCodePage = originalOutput
		setConsoleInputCodePage = originalInput
	})

	outputErr := errors.New("output failed")
	setConsoleOutputCodePage = func(codePage uint32) error {
		return outputErr
	}
	inputCalled := false
	setConsoleInputCodePage = func(codePage uint32) error {
		inputCalled = true
		return nil
	}

	EnsureConsoleUTF8()

	if inputCalled {
		t.Fatalf("expected input code page setup to be skipped when output setup fails")
	}
}

func TestRuntimeMemoExtractorFuncSchedule(t *testing.T) {
	t.Parallel()

	called := false
	extractor := runtimeMemoExtractorFunc(func(sessionID string, messages []providertypes.Message) {
		called = true
		if sessionID != "session-1" {
			t.Fatalf("unexpected session id %q", sessionID)
		}
		if len(messages) != 1 || messages[0].Content != "hi" {
			t.Fatalf("unexpected messages %+v", messages)
		}
	})

	extractor.Schedule("session-1", []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "hi"},
	})

	if !called {
		t.Fatalf("expected schedule callback to be called")
	}
}

func TestTextGenAdapterGenerate(t *testing.T) {
	t.Parallel()

	called := false
	adapter := textGenAdapter(func(ctx context.Context, prompt string, msgs []providertypes.Message) (string, error) {
		called = true
		if prompt != "prompt" {
			t.Fatalf("unexpected prompt %q", prompt)
		}
		if len(msgs) != 1 || msgs[0].Content != "msg" {
			t.Fatalf("unexpected messages %+v", msgs)
		}
		return "ok", nil
	})

	result, err := adapter.Generate(context.Background(), "prompt", []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "msg"},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result != "ok" {
		t.Fatalf("unexpected result %q", result)
	}
	if !called {
		t.Fatalf("expected adapter callback to be called")
	}
}

func TestNewMemoExtractorAdapterSkipsWhenSchedulerNil(t *testing.T) {
	t.Parallel()

	cfg := config.StaticDefaults().Clone()
	cfg.SelectedProvider = config.OpenAIName
	manager := config.NewManager(config.NewLoader("", &cfg))
	extractor := newMemoExtractorAdapter(nil, manager, nil)

	extractor.Schedule("session-1", []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "remember this"},
	})
}

func TestNewMemoExtractorAdapterSkipsWhenResolveProviderFails(t *testing.T) {
	t.Parallel()

	cfg := config.StaticDefaults().Clone()
	cfg.SelectedProvider = ""
	manager := config.NewManager(config.NewLoader("", &cfg))
	scheduler := &stubMemoExtractorScheduler{}
	extractor := newMemoExtractorAdapter(nil, manager, scheduler)

	extractor.Schedule("session-1", []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "remember this"},
	})

	if scheduler.called {
		t.Fatalf("expected scheduler not to be called when provider resolution fails")
	}
}

func TestNewMemoExtractorAdapterSchedulesExtractorAndGenerates(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "token")
	cfg := config.StaticDefaults().Clone()
	cfg.SelectedProvider = config.OpenAIName
	manager := config.NewManager(config.NewLoader("", &cfg))
	providerStub := &stubMemoProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			if req.Model != cfg.CurrentModel {
				t.Fatalf("unexpected model %q", req.Model)
			}
			if strings.TrimSpace(req.SystemPrompt) == "" {
				t.Fatalf("expected non-empty system prompt")
			}
			events <- providertypes.NewTextDeltaStreamEvent(`[{"type":"user","title":"偏好","content":"记住使用 Go","keywords":["go"]}]`)
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}
	factory := &stubMemoProviderFactory{provider: providerStub}
	scheduler := &stubMemoExtractorScheduler{}
	extractor := newMemoExtractorAdapter(factory, manager, scheduler)

	inputMessages := []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "remember 我偏好 Go"},
	}
	extractor.Schedule("session-1", inputMessages)

	if !scheduler.called || scheduler.extractor == nil {
		t.Fatalf("expected scheduler to receive extractor")
	}
	if scheduler.sessionID != "session-1" {
		t.Fatalf("unexpected session id %q", scheduler.sessionID)
	}
	entries, err := scheduler.extractor.Extract(context.Background(), inputMessages)
	if err != nil {
		t.Fatalf("extractor.Extract() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one extracted entry, got %+v", entries)
	}
	if !factory.called {
		t.Fatalf("expected provider factory Build to be called")
	}
}

func TestNewMemoExtractorAdapterBuildsProviderSafeMemoWindow(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "token")
	cfg := config.StaticDefaults().Clone()
	cfg.SelectedProvider = config.OpenAIName
	manager := config.NewManager(config.NewLoader("", &cfg))

	providerStub := &stubMemoProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			if len(req.Messages) != 2 {
				t.Fatalf("unexpected memo window: %+v", req.Messages)
			}
			for _, message := range req.Messages {
				if len(message.ToolCalls) > 0 {
					t.Fatalf("unexpected incomplete tool call span in memo window: %+v", req.Messages)
				}
			}
			events <- providertypes.NewTextDeltaStreamEvent(`[]`)
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}
	factory := &stubMemoProviderFactory{provider: providerStub}
	scheduler := &stubMemoExtractorScheduler{}
	extractor := newMemoExtractorAdapter(factory, manager, scheduler)

	inputMessages := []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "first"},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call_1", Name: "filesystem_read_file", Arguments: `{}`},
			},
		},
		{Role: providertypes.RoleUser, Content: "second"},
	}
	extractor.Schedule("session-1", inputMessages)
	if !scheduler.called || scheduler.extractor == nil {
		t.Fatalf("expected scheduler to receive extractor")
	}

	entries, err := scheduler.extractor.Extract(context.Background(), inputMessages)
	if err != nil {
		t.Fatalf("extractor.Extract() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty extracted entries, got %+v", entries)
	}
	if !factory.called {
		t.Fatalf("expected provider factory Build to be called")
	}
}

func TestNewMemoExtractorAdapterKeepsScheduledConfigSnapshot(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "openai-token")
	t.Setenv(config.QiniuDefaultAPIKeyEnv, "qiniu-token")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cfg := config.StaticDefaults().Clone()
	cfg.SelectedProvider = config.OpenAIName
	cfg.CurrentModel = "snapshot-model"
	manager := config.NewManager(config.NewLoader("", &cfg))
	providerStub := &stubMemoProvider{
		generate: func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error {
			if req.Model != "snapshot-model" {
				t.Fatalf("expected scheduled model snapshot, got %q", req.Model)
			}
			events <- providertypes.NewTextDeltaStreamEvent(
				`[{"type":"user","title":"偏好","content":"snapshot","keywords":["snapshot"]}]`,
			)
			events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
			return nil
		},
	}
	factory := &stubMemoProviderFactory{provider: providerStub}
	scheduler := &stubMemoExtractorScheduler{}
	extractor := newMemoExtractorAdapter(factory, manager, scheduler)

	messages := []providertypes.Message{{Role: providertypes.RoleUser, Content: "remember snapshot"}}
	extractor.Schedule("session-1", messages)
	if !scheduler.called || scheduler.extractor == nil {
		t.Fatalf("expected scheduler to receive extractor")
	}

	if err := manager.Update(context.Background(), func(next *config.Config) error {
		next.SelectedProvider = config.QiniuName
		next.CurrentModel = "drifted-model"
		return nil
	}); err != nil {
		t.Fatalf("manager.Update() error = %v", err)
	}

	entries, err := scheduler.extractor.Extract(context.Background(), messages)
	if err != nil {
		t.Fatalf("extractor.Extract() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one extracted entry, got %+v", entries)
	}
	if !factory.called {
		t.Fatalf("expected provider factory Build to be called")
	}
	if factory.cfg.Name != config.OpenAIName {
		t.Fatalf("expected scheduled provider snapshot %q, got %q", config.OpenAIName, factory.cfg.Name)
	}
}

func TestNewMemoExtractorAdapterPropagatesFactoryBuildError(t *testing.T) {
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "token")
	cfg := config.StaticDefaults().Clone()
	cfg.SelectedProvider = config.OpenAIName
	manager := config.NewManager(config.NewLoader("", &cfg))
	factory := &stubMemoProviderFactory{buildErr: errors.New("build failed")}
	scheduler := &stubMemoExtractorScheduler{}
	extractor := newMemoExtractorAdapter(factory, manager, scheduler)

	inputMessages := []providertypes.Message{
		{Role: providertypes.RoleUser, Content: "remember coding style"},
	}
	extractor.Schedule("session-1", inputMessages)
	if !scheduler.called || scheduler.extractor == nil {
		t.Fatalf("expected scheduler to receive extractor")
	}

	_, err := scheduler.extractor.Extract(context.Background(), inputMessages)
	if err == nil || !strings.Contains(err.Error(), "build failed") {
		t.Fatalf("expected build failure, got %v", err)
	}
}

type stubToolForBootstrap struct {
	name    string
	content string
}

func (s stubToolForBootstrap) Name() string           { return s.name }
func (s stubToolForBootstrap) Description() string    { return "stub" }
func (s stubToolForBootstrap) Schema() map[string]any { return map[string]any{"type": "object"} }
func (s stubToolForBootstrap) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}
func (s stubToolForBootstrap) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	return tools.ToolResult{Name: s.name, Content: s.content}, nil
}

func disableBuiltinProviderAPIKeys(t *testing.T) {
	t.Helper()
	t.Setenv(config.OpenAIDefaultAPIKeyEnv, "")
	t.Setenv(config.GeminiDefaultAPIKeyEnv, "")
	t.Setenv(config.OpenLLDefaultAPIKeyEnv, "")
	t.Setenv(config.QiniuDefaultAPIKeyEnv, "")
}

type stubMCPServerClient struct {
	tools   []mcp.ToolDescriptor
	listErr error
}

func (s *stubMCPServerClient) ListTools(ctx context.Context) ([]mcp.ToolDescriptor, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]mcp.ToolDescriptor(nil), s.tools...), nil
}

func (s *stubMCPServerClient) CallTool(ctx context.Context, toolName string, arguments []byte) (mcp.CallResult, error) {
	return mcp.CallResult{Content: "ok"}, nil
}

func (s *stubMCPServerClient) HealthCheck(ctx context.Context) error {
	return nil
}

type closeableStubMCPServerClient struct {
	closed *bool
}

func (s *closeableStubMCPServerClient) ListTools(ctx context.Context) ([]mcp.ToolDescriptor, error) {
	return nil, nil
}

func (s *closeableStubMCPServerClient) CallTool(ctx context.Context, toolName string, arguments []byte) (mcp.CallResult, error) {
	return mcp.CallResult{}, nil
}

func (s *closeableStubMCPServerClient) HealthCheck(ctx context.Context) error {
	return nil
}

func (s *closeableStubMCPServerClient) Close() error {
	if s.closed != nil {
		*s.closed = true
	}
	return nil
}

type stubMemoExtractorScheduler struct {
	called    bool
	sessionID string
	messages  []providertypes.Message
	extractor memo.Extractor
}

func (s *stubMemoExtractorScheduler) ScheduleWithExtractor(
	sessionID string,
	messages []providertypes.Message,
	extractor memo.Extractor,
) {
	s.called = true
	s.sessionID = sessionID
	s.messages = append([]providertypes.Message(nil), messages...)
	s.extractor = extractor
}

type stubMemoProviderFactory struct {
	called   bool
	cfg      provider.RuntimeConfig
	provider provider.Provider
	buildErr error
}

func (s *stubMemoProviderFactory) Build(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
	s.called = true
	s.cfg = cfg
	if s.buildErr != nil {
		return nil, s.buildErr
	}
	if s.provider != nil {
		return s.provider, nil
	}
	return &stubMemoProvider{}, nil
}

type stubMemoProvider struct {
	generate func(ctx context.Context, req providertypes.GenerateRequest, events chan<- providertypes.StreamEvent) error
}

func (s *stubMemoProvider) Generate(
	ctx context.Context,
	req providertypes.GenerateRequest,
	events chan<- providertypes.StreamEvent,
) error {
	if s.generate != nil {
		return s.generate(ctx, req, events)
	}
	events <- providertypes.NewMessageDoneStreamEvent("stop", nil)
	return nil
}

func TestHelperProcessAppMCPStdioServer(t *testing.T) {
	if os.Getenv("GO_WANT_APP_MCP_STDIO_HELPER") != "1" {
		return
	}

	listFail := os.Getenv("GO_APP_MCP_STDIO_LIST_FAIL") == "1"
	initialized := false
	reader := bufio.NewReader(os.Stdin)

	for {
		payload, err := readFramedForAppTest(reader)
		if err != nil {
			if errors.Is(err, os.ErrClosed) || strings.Contains(strings.ToLower(err.Error()), "eof") {
				os.Exit(0)
			}
			os.Exit(2)
		}

		var request map[string]any
		if err := json.Unmarshal(payload, &request); err != nil {
			os.Exit(3)
		}

		method, _ := request["method"].(string)
		requestID, _ := request["id"].(string)
		var response any

		switch method {
		case "initialize":
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"serverInfo": map[string]any{
						"name":    "app-helper",
						"version": "1.0.0",
					},
				},
			}
		case "notifications/initialized":
			initialized = true
			continue
		case "tools/list":
			if listFail {
				response = map[string]any{
					"jsonrpc": "2.0",
					"id":      requestID,
					"error": map[string]any{
						"code":    -32001,
						"message": "list tools failed",
					},
				}
				break
			}
			if !initialized {
				response = map[string]any{
					"jsonrpc": "2.0",
					"id":      requestID,
					"error": map[string]any{
						"code":    -32002,
						"message": "server not initialized",
					},
				}
				break
			}
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name":        "search",
							"description": "search docs",
							"inputSchema": map[string]any{
								"type":       "object",
								"properties": map[string]any{"query": map[string]any{"type": "string"}},
							},
						},
					},
				},
			}
		default:
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      requestID,
				"error": map[string]any{
					"code":    -32601,
					"message": "method not found",
				},
			}
		}

		rawResponse, err := json.Marshal(response)
		if err != nil {
			os.Exit(4)
		}
		if err := writeFramedForAppTest(os.Stdout, rawResponse); err != nil {
			os.Exit(5)
		}
	}
}

func readFramedForAppTest(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if contentLength >= 0 {
				break
			}
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "content-length:") {
			rawLength := strings.TrimSpace(trimmed[len("content-length:"):])
			length, convErr := strconv.Atoi(rawLength)
			if convErr != nil {
				return nil, convErr
			}
			contentLength = length
			continue
		}
	}
	if contentLength < 0 {
		return nil, errors.New("missing content-length")
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeFramedForAppTest(writer io.Writer, payload []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))
	if _, err := io.WriteString(writer, header); err != nil {
		return err
	}
	if _, err := writer.Write(bytes.TrimSpace(payload)); err != nil {
		return err
	}
	return nil
}
