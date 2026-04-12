package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	providerpkg "neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

// ---- 辅助函数与默认值测试 ----

func TestNormalizeWorkdirEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, value string)
	}{
		{
			name:  "empty string returns empty",
			input: "",
			check: func(t *testing.T, value string) {
				if value != "" {
					t.Fatalf("expected empty, got %q", value)
				}
			},
		},
		{
			name:  "whitespace only returns empty",
			input: "   ",
			check: func(t *testing.T, value string) {
				if value != "" {
					t.Fatalf("expected empty for whitespace-only input, got %q", value)
				}
			},
		},
		{
			name:  "dot resolves to cwd",
			input: ".",
			check: func(t *testing.T, value string) {
				if !filepath.IsAbs(value) {
					t.Fatalf("expected absolute path for dot, got %q", value)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeWorkdir(tt.input)
			tt.check(t, got)
		})
	}
}

func TestContainsProviderName(t *testing.T) {
	t.Parallel()

	providers := []ProviderConfig{
		{Name: "OpenAI"},
		{Name: "Gemini"},
	}
	if !containsProviderName(providers, "openai") {
		t.Fatal("expected openai to be found (case insensitive)")
	}
	if !containsProviderName(providers, "OPENAI") {
		t.Fatal("expected OPENAI to be found")
	}
	if containsProviderName(providers, "Anthropic") {
		t.Fatal("expected Anthropic to not be found")
	}
	if containsProviderName(nil, "openai") {
		t.Fatal("expected nil providers to return false")
	}
	if containsProviderName(providers, "") {
		t.Fatal("expected empty name to return false")
	}
	if containsProviderName(providers, "  ") {
		t.Fatal("expected whitespace name to return false")
	}
}

func TestDefaultWebFetchSupportedContentTypesReturnsCopy(t *testing.T) {
	t.Parallel()

	types1 := DefaultWebFetchSupportedContentTypes()
	types2 := DefaultWebFetchSupportedContentTypes()
	if &types1[0] == &types2[0] {
		t.Fatal("expected independent slices")
	}
	if len(types1) == 0 {
		t.Fatal("expected non-empty default content types")
	}
}

func TestStaticReturnsCompleteSkeleton(t *testing.T) {
	t.Parallel()

	cfg := StaticDefaults()
	if cfg == nil {
		t.Fatal("expected non-nil static defaults")
	}
	if cfg.Workdir == "" {
		t.Fatal("expected workdir to be set")
	}
	if cfg.Shell == "" {
		t.Fatal("expected shell to be set")
	}
	if cfg.MaxLoops == 0 {
		t.Fatal("expected max_loops to be set")
	}
	if cfg.ToolTimeoutSec == 0 {
		t.Fatal("expected tool_timeout_sec to be set")
	}
	if cfg.Tools.WebFetch.MaxResponseBytes == 0 {
		t.Fatal("expected webfetch max_response_bytes to be set")
	}
}

// ---- Clone 独立性测试 ----

func TestWebFetchConfigCloneIndependence(t *testing.T) {
	t.Parallel()

	original := WebFetchConfig{
		MaxResponseBytes:      4096,
		SupportedContentTypes: []string{"text/html", "application/json"},
	}
	cloned := original.Clone()

	cloned.MaxResponseBytes = 1024
	cloned.SupportedContentTypes[0] = "text/plain"

	if original.MaxResponseBytes == cloned.MaxResponseBytes {
		t.Fatal("expected MaxResponseBytes clone to be independent")
	}
	if original.SupportedContentTypes[0] == cloned.SupportedContentTypes[0] {
		t.Fatal("expected SupportedContentTypes clone to be independent")
	}
}

func TestContextConfigCloneIndependence(t *testing.T) {
	t.Parallel()

	original := ContextConfig{
		Compact: CompactConfig{
			ManualStrategy:           CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
		},
		AutoCompact: AutoCompactConfig{
			Enabled:             true,
			InputTokenThreshold: 50000,
		},
	}
	cloned := original.Clone()

	cloned.Compact.ManualStrategy = CompactManualStrategyFullReplace
	cloned.Compact.ManualKeepRecentMessages = 5
	cloned.AutoCompact.Enabled = false
	cloned.AutoCompact.InputTokenThreshold = 100000

	if original.Compact.ManualStrategy == cloned.Compact.ManualStrategy {
		t.Fatal("expected Compact Clone to be independent")
	}
	if original.Compact.ManualKeepRecentMessages == cloned.Compact.ManualKeepRecentMessages {
		t.Fatal("expected ManualKeepRecentMessages Clone to be independent")
	}
	if original.AutoCompact.Enabled == cloned.AutoCompact.Enabled {
		t.Fatal("expected AutoCompact Enabled clone to be independent")
	}
	if original.AutoCompact.InputTokenThreshold == cloned.AutoCompact.InputTokenThreshold {
		t.Fatal("expected AutoCompact InputTokenThreshold clone to be independent")
	}
}

func TestCompactConfigCloneValueSemantics(t *testing.T) {
	t.Parallel()

	original := CompactConfig{
		ManualStrategy:           CompactManualStrategyFullReplace,
		ManualKeepRecentMessages: 5,
		MaxSummaryChars:          800,
		MicroCompactDisabled:     true,
	}
	cloned := original.Clone()
	if original != cloned {
		t.Fatalf("expected equal configs, got %+v vs %+v", original, cloned)
	}
}

func TestAutoCompactConfigCloneValueSemantics(t *testing.T) {
	t.Parallel()

	original := AutoCompactConfig{Enabled: true, InputTokenThreshold: 75000}
	cloned := original.Clone()
	if original != cloned {
		t.Fatalf("expected equal configs, got %+v vs %+v", original, cloned)
	}
}

// ---- nil 接收者 ApplyDefaults 测试 ----

func TestApplyDefaultsNilReceivers(t *testing.T) {
	t.Parallel()

	var toolsCfg *ToolsConfig
	toolsCfg.ApplyDefaults(ToolsConfig{})
	// 不应 panic

	var ctxCfg *ContextConfig
	ctxCfg.ApplyDefaults(ContextConfig{})
	// 不应 panic

	var mcpCfg *MCPConfig
	mcpCfg.ApplyDefaults(MCPConfig{})
	// 不应 panic

	var wfCfg *WebFetchConfig
	wfCfg.ApplyDefaults(WebFetchConfig{})
	// 不应 panic

	var ccfg *CompactConfig
	ccfg.ApplyDefaults(CompactConfig{})
	// 不应 panic

	var serverCfg *MCPServerConfig
	serverCfg.ApplyDefaults()
	// 不应 panic
}

func TestApplyStaticDefaultsNilReceiver(t *testing.T) {
	t.Parallel()

	var cfg *Config
	cfg.applyStaticDefaults(*StaticDefaults())
	// 不应 panic，cfg 应仍为 nil
	if cfg != nil {
		t.Fatal("expected nil config to remain nil")
	}
}

// ---- 校验边界测试 ----

func TestValidateSnapshotRejectsEmptyWorkdir(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Providers:        []ProviderConfig{testDefaultProviderConfig()},
		SelectedProvider: testProviderName,
		CurrentModel:     testModel,
		Workdir:          "",
		Shell:            "powershell",
	}
	err := cfg.ValidateSnapshot()
	if err == nil || !strings.Contains(err.Error(), "workdir is empty") {
		t.Fatalf("expected empty workdir error, got %v", err)
	}
}

func TestValidateSnapshotPropagatesCompactError(t *testing.T) {
	t.Parallel()

	workdir := filepath.Clean(t.TempDir())
	cfg := Config{
		Providers:        []ProviderConfig{testDefaultProviderConfig()},
		SelectedProvider: testProviderName,
		CurrentModel:     testModel,
		Workdir:          workdir,
		Shell:            "powershell",
		Tools: ToolsConfig{
			WebFetch: WebFetchConfig{
				MaxResponseBytes:      DefaultWebFetchMaxResponseBytes,
				SupportedContentTypes: []string{"text/html"},
			},
		},
		Context: ContextConfig{
			Compact: CompactConfig{
				ManualStrategy:           "invalid_strategy",
				ManualKeepRecentMessages: 10,
				MaxSummaryChars:          1200,
			},
		},
	}
	err := cfg.ValidateSnapshot()
	if err == nil || !strings.Contains(err.Error(), "compact") {
		t.Fatalf("expected compact error, got %v", err)
	}
}

func TestResolveSelectedProviderEmptyString(t *testing.T) {
	t.Parallel()

	cfg := Config{
		SelectedProvider: "",
		Providers:        []ProviderConfig{testDefaultProviderConfig()},
	}
	_, err := ResolveSelectedProvider(cfg)
	if err == nil || !strings.Contains(err.Error(), "selected provider is empty") {
		t.Fatalf("expected empty selected provider error, got %v", err)
	}
}

func TestResolveSelectedProviderNotFound(t *testing.T) {
	t.Parallel()

	cfg := Config{
		SelectedProvider: "nonexistent-provider",
		Providers:        []ProviderConfig{testDefaultProviderConfig()},
	}
	_, err := ResolveSelectedProvider(cfg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestProviderConfigIdentity(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{
		Name:      "test-openai",
		Driver:    "openaicompat",
		BaseURL:   "https://api.openai.com/v1",
		Model:     "gpt-4o",
		APIKeyEnv: "TEST_KEY",
		APIStyle:  "chat_completions",
	}

	identity, err := cfg.Identity()
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	if identity.Driver != "openaicompat" {
		t.Fatalf("expected driver openaicompat, got %q", identity.Driver)
	}
	if identity.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("expected base URL, got %q", identity.BaseURL)
	}
	if identity.APIStyle != "chat_completions" {
		t.Fatalf("expected api_style chat_completions, got %q", identity.APIStyle)
	}
}

func TestProviderConfigResolveAPIKeyEmptyEnvName(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{Name: "test", APIKeyEnv: ""}
	_, err := cfg.ResolveAPIKey()
	if err == nil || !strings.Contains(err.Error(), "api_key_env is empty") {
		t.Fatalf("expected empty api_key_env error, got %v", err)
	}
}

// ---- 内容类型规范化测试 ----

func TestNormalizeContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{input: "text/html", expected: "text/html"},
		{input: " Text/HTML ", expected: "text/html"},
		{input: "text/html; charset=utf-8", expected: "text/html"},
		{input: "APPLICATION/JSON", expected: "application/json"},
		{input: "   ", expected: ""},
		{input: "", expected: ""},
		{input: "application/x-www-form-urlencoded; charset=utf-8", expected: "application/x-www-form-urlencoded"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := normalizeContentType(tt.input)
			if got != tt.expected {
				t.Fatalf("normalizeContentType(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeContentTypesDeduplicatesAndCleans(t *testing.T) {
	t.Parallel()

	defaults := []string{"text/html", "text/plain"}
	result := normalizeContentTypes([]string{"TEXT/HTML", " text/plain ", "text/html", "", "  "}, defaults)
	if len(result) != 2 {
		t.Fatalf("expected 2 unique content types, got %d: %+v", len(result), result)
	}
}

func TestNormalizeContentTypesFallsBackToDefaultsWhenEmpty(t *testing.T) {
	t.Parallel()

	defaults := []string{"text/html", "text/plain"}
	// 当所有输入值都为空或纯空白时，它们全部被过滤掉，结果为空切片
	// （normalizeContentTypes 只在 source 长度为 0 时才回退到 defaults，而这里 source 长度为 2）
	result := normalizeContentTypes([]string{"", "   "}, defaults)
	if len(result) != 0 {
		t.Fatalf("expected 0 types when all inputs are empty, got %d: %+v", len(result), result)
	}
}

// ---- Manager 边界测试 ----

func TestManagerUpdateNilMutateFunc(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewManager(NewLoader(tempDir, testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	err := manager.Update(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "mutate func is nil") {
		t.Fatalf("expected nil mutate error, got %v", err)
	}
}

func TestManagerUpdateValidationFailurePreservesCurrentState(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewManager(NewLoader(tempDir, testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	before := manager.Get()
	// 使用无效的 compact strategy 触发验证失败（applyStaticDefaults 不会修复这个字段）
	err := manager.Update(context.Background(), func(cfg *Config) error {
		cfg.Context.Compact.ManualStrategy = "totally_invalid_strategy"
		return nil
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	after := manager.Get()
	if before.SelectedProvider != after.SelectedProvider {
		t.Fatalf("expected selected provider to be preserved after failed update")
	}
}

// ---- Loader 边界测试 ----

func TestParseConfigWithEmptyData(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfigWithContextDefaults([]byte{}, StaticDefaults().Context)
	if err != nil {
		t.Fatalf("parseConfigWithContextDefaults(empty) error = %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config for empty data")
	}
}

func TestParseConfigWithWhitespaceOnlyData(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfigWithContextDefaults([]byte("  \n\t  "), StaticDefaults().Context)
	if err != nil {
		t.Fatalf("parseConfigWithContextDefaults(whitespace) error = %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config for whitespace-only data")
	}
}

func TestMarshalPersistedConfigEndsWithNewline(t *testing.T) {
	t.Parallel()

	snapshot := testDefaultConfig().Clone()
	data, err := marshalPersistedConfig(snapshot)
	if err != nil {
		t.Fatalf("marshalPersistedConfig() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty marshaled data")
	}
	if data[len(data)-1] != '\n' {
		t.Fatal("expected marshaled data to end with newline")
	}
}

func TestAssembleProvidersAcceptsEmptyNameProvider(t *testing.T) {
	t.Parallel()

	custom := []ProviderConfig{{
		Name:      "",
		Driver:    "custom-driver",
		BaseURL:   "https://example.com/v1",
		APIKeyEnv: "CUSTOM_KEY",
		Source:    ProviderSourceCustom,
	}}

	assembled, err := assembleProviders(testDefaultConfig().Providers, custom)
	if err != nil {
		t.Fatalf("assembleProviders() with empty name error = %v", err)
	}
	if len(assembled) < 2 {
		t.Fatalf("expected at least 2 providers, got %d", len(assembled))
	}
}

func TestAssembleProvidersDuplicateBetweenBuiltinAndCustom(t *testing.T) {
	t.Parallel()

	custom := []ProviderConfig{{
		Name:      testProviderName, // 与内置同名
		Driver:    "custom-driver",
		BaseURL:   "https://example.com/v1",
		APIKeyEnv: "CUSTOM_KEY",
		Source:    ProviderSourceCustom,
	}}

	_, err := assembleProviders(testDefaultConfig().Providers, custom)
	if err == nil || !strings.Contains(err.Error(), "duplicate provider name") {
		t.Fatalf("expected duplicate error between builtin/custom, got %v", err)
	}
}

// ---- runtime 转换测试 ----

func TestToRuntimeConfigMapsAllFields(t *testing.T) {
	t.Parallel()

	resolved := ResolvedProviderConfig{
		ProviderConfig: ProviderConfig{
			Name:           "test-provider",
			Driver:         "gemini",
			BaseURL:        "https://generativelanguage.googleapis.com/v1beta/openai",
			Model:          "gemini-2.5-flash",
			APIKeyEnv:      "TEST_ENV_KEY",
			APIStyle:       "responses",
			DeploymentMode: "vertex",
			APIVersion:     "v1beta",
		},
		APIKey: "resolved-secret-key",
	}

	got := resolved.ToRuntimeConfig()
	if got.Name != "test-provider" {
		t.Fatalf("expected Name=test-provider, got %q", got.Name)
	}
	if got.Driver != "gemini" {
		t.Fatalf("expected Driver=gemini, got %q", got.Driver)
	}
	if got.DefaultModel != "gemini-2.5-flash" {
		t.Fatalf("expected DefaultModel=gemini-2.5-flash, got %q", got.DefaultModel)
	}
	if got.APIKey != "resolved-secret-key" {
		t.Fatalf("expected APIKey=resolved-secret-key, got %q", got.APIKey)
	}
	if got.DeploymentMode != "vertex" {
		t.Fatalf("expected DeploymentMode=vertex, got %q", got.DeploymentMode)
	}
	if got.APIVersion != "v1beta" {
		t.Fatalf("expected APIVersion=v1beta, got %q", got.APIVersion)
	}
}

// ---- MCP 配置测试 ----

func TestMCPServerConfigApplyDefaultsNormalizesSource(t *testing.T) {
	t.Parallel()

	cfg := MCPServerConfig{
		ID:      "test-server",
		Enabled: true,
		Source:  " STDIO ",
		Stdio:   MCPStdioConfig{Command: "cmd"},
	}
	cfg.ApplyDefaults()
	if cfg.Source != "stdio" {
		t.Fatalf("expected normalized source 'stdio', got %q", cfg.Source)
	}
}

func TestMCPServerConfigApplyDefaultsSetsStdioWhenEmpty(t *testing.T) {
	t.Parallel()

	cfg := MCPServerConfig{
		ID:      "test-server",
		Enabled: true,
		Source:  "",
		Stdio:   MCPStdioConfig{Command: "cmd"},
	}
	cfg.ApplyDefaults()
	if cfg.Source != "stdio" {
		t.Fatalf("expected default source 'stdio', got %q", cfg.Source)
	}
}

func TestMCPConfigValidateSkipsDisabledServerWithoutCommand(t *testing.T) {
	t.Parallel()

	cfg := MCPConfig{
		Servers: []MCPServerConfig{
			{ID: "disabled-no-cmd", Enabled: false, Source: "stdio"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected disabled server without command to pass validation, got %v", err)
	}
}

func TestMCPConfigValidateRejectsServerWithEmptyEnvBothValues(t *testing.T) {
	t.Parallel()

	cfg := MCPConfig{
		Servers: []MCPServerConfig{
			{
				ID:      "test",
				Enabled: true,
				Source:  "stdio",
				Stdio:   MCPStdioConfig{Command: "cmd"},
				Env: []MCPEnvVarConfig{
					{Name: "VAR", Value: "", ValueEnv: ""},
					{Name: "VAR2", Value: "a", ValueEnv: "b"},
				},
			},
		},
	}
	err := cfg.Validate()
	// 至少应该有一个错误（两个 env 都有问题）
	if err == nil {
		t.Fatal("expected validation error for invalid env bindings")
	}
}

func TestMCPConfigValidateRejectsServerWithMissingEnvName(t *testing.T) {
	t.Parallel()

	cfg := MCPConfig{
		Servers: []MCPServerConfig{
			{
				ID:      "test",
				Enabled: true,
				Source:  "stdio",
				Stdio:   MCPStdioConfig{Command: "cmd"},
				Env: []MCPEnvVarConfig{
					{Name: "  ", Value: "a"},
				},
			},
		},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "name is empty") {
		t.Fatalf("expected empty env name error, got %v", err)
	}
}

// ---- WebFetch 配置测试 ----

func TestWebFetchConfigValidateRejectsEmptyContentType(t *testing.T) {
	t.Parallel()

	cfg := WebFetchConfig{
		MaxResponseBytes:      1024,
		SupportedContentTypes: []string{"text/html", "", "application/json"},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "[1] is empty") {
		t.Fatalf("expected empty content type error, got %v", err)
	}
}

// ---- ContextConfig 验证传播测试 ----

func TestContextConfigValidatePropagatesCompactError(t *testing.T) {
	t.Parallel()

	cfg := ContextConfig{
		Compact: CompactConfig{
			ManualKeepRecentMessages: 0,
			MaxSummaryChars:          0,
		},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "compact") {
		t.Fatalf("expected compact validation error, got %v", err)
	}
}

// ---- ProviderIdentityFromConfig 默认 APIStyle 回退 ----

func TestProviderIdentityFromConfigDefaultsAPIStyleForOpenAICompat(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{
		Name:      "test-openai",
		Driver:    "openaicompat",
		BaseURL:   "https://api.openai.com/v1",
		APIKeyEnv: "TEST_KEY",
		// APIStyle 故意为空
	}

	identity, err := providerIdentityFromConfig(cfg)
	if err != nil {
		t.Fatalf("providerIdentityFromConfig() error = %v", err)
	}
	if identity.APIStyle != providerpkg.OpenAICompatibleAPIStyleChatCompletions {
		t.Fatalf("expected default api_style %q, got %q", providerpkg.OpenAICompatibleAPIStyleChatCompletions, identity.APIStyle)
	}
}

// ---- Qiniu 内置 provider 完整性 ----

func TestQiniuProviderConfig(t *testing.T) {
	t.Parallel()

	provider := QiniuProvider()
	if provider.Name != QiniuName {
		t.Fatalf("expected name %q, got %q", QiniuName, provider.Name)
	}
	if provider.Driver != "openaicompat" {
		t.Fatalf("expected driver openaicompat, got %q", provider.Driver)
	}
	if provider.BaseURL != QiniuDefaultBaseURL {
		t.Fatalf("expected base URL %q, got %q", QiniuDefaultBaseURL, provider.BaseURL)
	}
	if provider.Model != QiniuDefaultModel {
		t.Fatalf("expected default model %q, got %q", QiniuDefaultModel, provider.Model)
	}
	if provider.APIKeyEnv != QiniuDefaultAPIKeyEnv {
		t.Fatalf("expected API key env %q, got %q", QiniuDefaultAPIKeyEnv, provider.APIKeyEnv)
	}
	if provider.Source != ProviderSourceBuiltin {
		t.Fatalf("expected builtin source, got %q", provider.Source)
	}
}

// ---- normalizeConfigKey / normalizeProviderDriver 基本覆盖 ----

func TestNormalizeConfigKey(t *testing.T) {
	t.Parallel()

	if normalizeConfigKey(" OpenAI ") != "openai" {
		t.Fatal("expected normalized lowercase trimmed key")
	}
	if normalizeConfigKey("") != "" {
		t.Fatal("expected empty key to remain empty")
	}
}

func TestNormalizeProviderDriver(t *testing.T) {
	t.Parallel()

	if normalizeProviderDriver(" OPENAICOMPAT ") != "openaicompat" {
		t.Fatal("expected normalized driver")
	}
	if normalizeProviderDriver("") != "" {
		t.Fatal("expected empty driver to remain empty")
	}
}

// ---- MCPServerConfig Clone 独立性（无 env 场景）----

func TestMCPServerConfigCloneWithoutEnv(t *testing.T) {
	t.Parallel()

	original := MCPServerConfig{
		ID:      "test",
		Enabled: true,
		Source:  "stdio",
		Stdio:   MCPStdioConfig{Command: "cmd", Args: []string{"--help"}},
		Env:     nil,
	}
	cloned := original.Clone()
	cloned.Stdio.Args[0] = "--version"
	if original.Stdio.Args[0] == cloned.Stdio.Args[0] {
		t.Fatal("expected args clone to be independent")
	}
}

func TestMCPServerConfigCloneWithEmptyEnvSlice(t *testing.T) {
	t.Parallel()

	original := MCPServerConfig{
		ID:    "test",
		Stdio: MCPStdioConfig{Command: "cmd"},
		Env:   []MCPEnvVarConfig{},
	}
	cloned := original.Clone()
	if len(cloned.Env) != 0 {
		t.Fatal("expected cloned env to be empty slice, not nil")
	}
}

// ---- ProviderConfig Resolve 错误链 ----

func TestProviderConfigResolveWrapsAPIKeyError(t *testing.T) {
	restoreEnv(t, "UNRESOLVABLE_API_KEY_FOR_TEST")
	_ = os.Unsetenv("UNRESOLVABLE_API_KEY_FOR_TEST")

	cfg := ProviderConfig{
		Name:      "test",
		Driver:    "custom",
		BaseURL:   "https://example.com",
		APIKeyEnv: "UNRESOLVABLE_API_KEY_FOR_TEST",
	}
	_, err := cfg.Resolve()
	if err == nil || !strings.Contains(err.Error(), "UNRESOLVABLE_API_KEY_FOR_TEST") {
		t.Fatalf("expected unresolved API key error, got %v", err)
	}
}

// ---- ToolsConfig Clone + ApplyDefaults 完整性 ----

func TestToolsConfigCloneIndependence(t *testing.T) {
	t.Parallel()

	original := ToolsConfig{
		WebFetch: WebFetchConfig{
			MaxResponseBytes:      2048,
			SupportedContentTypes: []string{"text/html"},
		},
		MCP: MCPConfig{
			Servers: []MCPServerConfig{{ID: "s1"}},
		},
	}
	cloned := original.Clone()
	cloned.WebFetch.MaxResponseBytes = 999
	cloned.MCP.Servers[0].ID = "s2"

	if original.WebFetch.MaxResponseBytes == cloned.WebFetch.MaxResponseBytes {
		t.Fatal("expected WebFetch clone independence")
	}
	if original.MCP.Servers[0].ID == cloned.MCP.Servers[0].ID {
		t.Fatal("expected MCP clone independence")
	}
}

// ---- cloneProviderConfig 模型独立性 ----

func TestCloneProviderConfigModelDescriptorsIndependence(t *testing.T) {
	t.Parallel()

	original := ProviderConfig{
		Name:   "test",
		Models: []providertypes.ModelDescriptor{{ID: "model-a"}, {ID: "model-b"}},
	}
	cloned := cloneProviderConfig(original)
	cloned.Models[0].ID = "mutated"
	if original.Models[0].ID == cloned.Models[0].ID {
		t.Fatal("expected Models descriptor clone to be independent")
	}
}

// ---- ProviderByName 大小写不敏感查找 ----

func TestProviderByNameCaseInsensitive(t *testing.T) {
	t.Parallel()

	cfg := &Config{Providers: []ProviderConfig{testDefaultProviderConfig()}}
	found, err := cfg.ProviderByName("OPENAI")
	if err != nil {
		t.Fatalf("ProviderByName(OPENAI) error = %v", err)
	}
	if found.Name != testProviderName {
		t.Fatalf("expected provider %q, got %q", testProviderName, found.Name)
	}
}

// ---- ResolveSelectedProvider 包装 ProviderByName 错误 ----

func TestResolveSelectedProviderWrapsNotFoundError(t *testing.T) {
	t.Parallel()

	cfg := Config{
		SelectedProvider: "nonexistent",
		Providers:        []ProviderConfig{testDefaultProviderConfig()},
	}
	_, err := ResolveSelectedProvider(cfg)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected wrapped not found error, got %v", err)
	}
}

// ---- ContextConfig ApplyDefaults 传播到子配置 ----

func TestContextConfigApplyDefaultsNilReceiver(t *testing.T) {
	t.Parallel()

	var ctxCfg *ContextConfig
	ctxCfg.ApplyDefaults(ContextConfig{
		Compact:     CompactConfig{ManualStrategy: CompactManualStrategyFullReplace},
		AutoCompact: AutoCompactConfig{InputTokenThreshold: 50000},
	})
	// 不应 panic
}

// ---- CompactConfig ApplyDefaults 全部为零值时填充 ----

func TestCompactConfigApplyDefaultsAllZeroValues(t *testing.T) {
	t.Parallel()

	cfg := CompactConfig{}
	defaults := CompactConfig{
		ManualStrategy:           CompactManualStrategyFullReplace,
		ManualKeepRecentMessages: 15,
		MaxSummaryChars:          2000,
	}
	cfg.ApplyDefaults(defaults)
	if cfg.ManualStrategy != CompactManualStrategyFullReplace {
		t.Fatalf("expected strategy %q, got %q", CompactManualStrategyFullReplace, cfg.ManualStrategy)
	}
	if cfg.ManualKeepRecentMessages != 15 {
		t.Fatalf("expected messages=15, got %d", cfg.ManualKeepRecentMessages)
	}
	if cfg.MaxSummaryChars != 2000 {
		t.Fatalf("expected chars=2000, got %d", cfg.MaxSummaryChars)
	}
}

// ---- WebFetchConfig ApplyDefaults 边界 ----

func TestWebFetchConfigApplyDefaultsNilReceiver(t *testing.T) {
	t.Parallel()

	var wfCfg *WebFetchConfig
	wfCfg.ApplyDefaults(WebFetchConfig{
		MaxResponseBytes:      512,
		SupportedContentTypes: []string{"text/plain"},
	})
	// 不应 panic
}

func TestWebFetchConfigApplyDefaultsPreservesExplicitMaxResponseBytes(t *testing.T) {
	t.Parallel()

	cfg := WebFetchConfig{MaxResponseBytes: 9999}
	cfg.ApplyDefaults(WebFetchConfig{MaxResponseBytes: 100})
	if cfg.MaxResponseBytes != 9999 {
		t.Fatalf("expected explicit MaxResponseBytes=9999 to be preserved, got %d", cfg.MaxResponseBytes)
	}
}

// ---- Loader 取消上下文测试 ----

func TestLoaderLoadWithCanceledContext(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := loader.Load(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error for Load, got %v", err)
	}
}

func TestLoaderSaveWithCanceledContext(t *testing.T) {
	t.Parallel()

	loader := NewLoader(t.TempDir(), testDefaultConfig())
	cfg := testDefaultConfig()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := loader.Save(ctx, cfg)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error for Save, got %v", err)
	}
}

// ---- Manager Update 恢复 providers 列表 ----

func TestManagerUpdateRestoresProvidersWhenCleared(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewManager(NewLoader(tempDir, testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	err := manager.Update(context.Background(), func(cfg *Config) error {
		cfg.Providers = nil // 清空 provider 列表
		return nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	reloaded := manager.Get()
	if len(reloaded.Providers) == 0 {
		t.Fatal("expected providers to be restored from defaults after clearing")
	}
}

// ---- MCPServerConfig Clone 环境变量独立性 ----

func TestMCPServerConfigCloneEnvIndependence(t *testing.T) {
	t.Parallel()

	original := MCPServerConfig{
		ID:    "test",
		Stdio: MCPStdioConfig{Command: "cmd"},
		Env: []MCPEnvVarConfig{
			{Name: "TOKEN", Value: "secret"},
		},
	}
	cloned := original.Clone()
	cloned.Env[0].Value = "modified"
	if original.Env[0].Value == cloned.Env[0].Value {
		t.Fatal("expected Env clone to be independent")
	}
}

// ---- cloneProviders 空切片 ----

func TestCloneProvidersEmptySlice(t *testing.T) {
	t.Parallel()

	result := cloneProviders([]ProviderConfig{})
	if result != nil {
		t.Fatalf("expected nil for empty input, got %+v", result)
	}
}

// ---- ProviderByName 空名称 ----

func TestProviderByNameEmptyNameOnNonEmptyList(t *testing.T) {
	t.Parallel()

	cfg := &Config{Providers: []ProviderConfig{testDefaultProviderConfig()}}
	_, err := cfg.ProviderByName("")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found for empty name, got %v", err)
	}
}

// ---- ResolveAPIKey 空环境变量名 ----

func TestResolveAPIKeyEmptyEnvName(t *testing.T) {
	t.Parallel()

	cfg := ProviderConfig{Name: "test"}
	_, err := cfg.ResolveAPIKey()
	if err == nil || !strings.Contains(err.Error(), "api_key_env is empty") {
		t.Fatalf("expected empty api_key_env error, got %v", err)
	}
}

// ---- Config.Clone nil 接收者返回默认值 ----

func TestConfigCloneNilReceiverReturnsDefaults(t *testing.T) {
	t.Parallel()

	var cfg *Config
	cloned := cfg.Clone()
	// Clone(nil) 走 StaticDefaults() 分支，providers 为空，cloneProviders 对空切片返回 nil
	if cloned.Workdir == "" {
		t.Fatal("expected cloned nil config to have default workdir")
	}
	if cloned.Shell == "" {
		t.Fatal("expected cloned nil config to have default shell")
	}
	if cloned.MaxLoops == 0 {
		t.Fatal("expected cloned nil config to have default max_loops")
	}
}
