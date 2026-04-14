package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	providerpkg "neo-code/internal/provider"
)

const (
	testProviderName = "openai"
	testBaseURL      = "https://api.openai.com/v1"
	testModel        = "gpt-4.1"
	testAPIKeyEnv    = "OPENAI_API_KEY"
)

func testDefaultProviderConfig() ProviderConfig {
	return ProviderConfig{
		Name:      testProviderName,
		Driver:    providerpkg.DriverOpenAICompat,
		BaseURL:   testBaseURL,
		Model:     testModel,
		APIKeyEnv: testAPIKeyEnv,
		APIStyle:  providerpkg.OpenAICompatibleAPIStyleChatCompletions,
		Source:    ProviderSourceBuiltin,
	}
}

func testDefaultConfig() *Config {
	cfg := StaticDefaults()
	defaultProvider := testDefaultProviderConfig()
	cfg.Providers = []ProviderConfig{defaultProvider}
	cfg.SelectedProvider = defaultProvider.Name
	cfg.CurrentModel = defaultProvider.Model
	return cfg
}

func applyStaticDefaultsForTest(cfg *Config) {
	if len(cfg.Providers) == 0 {
		cfg.Providers = cloneProviders(testDefaultConfig().Providers)
	}
	cfg.applyStaticDefaults(*testDefaultConfig())
}

func selectedProviderConfigForTest(cfg *Config) (ProviderConfig, error) {
	if cfg == nil {
		return ProviderConfig{}, os.ErrInvalid
	}
	if strings.TrimSpace(cfg.SelectedProvider) == "" {
		return ProviderConfig{}, os.ErrNotExist
	}
	return cfg.ProviderByName(cfg.SelectedProvider)
}

func TestParseConfigFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		data   string
		err    string
		assert func(t *testing.T, cfg *Config)
	}{
		{
			name: "current format parses runtime settings only",
			data: `
selected_provider: openai
current_model: gpt-5.4
shell: powershell
tools:
  webfetch:
    max_response_bytes: 4096
    supported_content_types:
      - text/html
      - text/plain
`,
			assert: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.CurrentModel != "gpt-5.4" {
					t.Fatalf("expected current model gpt-5.4, got %q", cfg.CurrentModel)
				}
				provider, err := selectedProviderConfigForTest(cfg)
				if err != nil {
					t.Fatalf("selected provider: %v", err)
				}
				if provider.BaseURL != testBaseURL {
					t.Fatalf("expected builtin base url %q, got %q", testBaseURL, provider.BaseURL)
				}
				if provider.Model != testModel {
					t.Fatalf("expected builtin default model %q, got %q", testModel, provider.Model)
				}
				if cfg.Tools.WebFetch.MaxResponseBytes != 4096 {
					t.Fatalf("expected custom max_response_bytes 4096, got %d", cfg.Tools.WebFetch.MaxResponseBytes)
				}
				if len(cfg.Tools.WebFetch.SupportedContentTypes) != 2 {
					t.Fatalf("expected 2 supported content types, got %+v", cfg.Tools.WebFetch.SupportedContentTypes)
				}
			},
		},
		{
			name: "legacy default_workdir key is rejected",
			data: `
selected_provider: openai
current_model: gpt-4.1
default_workdir: ./from-default
shell: powershell
`,
			err: "field default_workdir not found",
		},
		{
			name: "legacy workdir key is rejected",
			data: `
selected_provider: openai
current_model: gpt-4.1
workdir: ./from-legacy
shell: powershell
`,
			err: "field workdir not found",
		},
		{
			name: "legacy persisted providers list is rejected",
			data: `
selected_provider: openai
current_model: gpt-5.4
shell: powershell
providers:
  - name: openai
    type: openai
    base_url: https://example.com/v1
    model: gpt-5.4
    api_key_env: OPENAI_API_KEY
`,
			err: `field providers not found`,
		},
		{
			name: "legacy unknown fields are rejected",
			data: `
selected_provider: openai
current_model: gpt-4o
workspace_root: ./definitely-legacy-root
shell: bash
max_loop: 5
providers:
  openai:
    type: openai
    base_url: https://legacy.example.com/v1
    api_key_env: OPENAI_API_KEY
    models:
      - gpt-4o
`,
			err: `field workspace_root not found`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := parseConfig([]byte(tt.data))
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("expected parseConfig() error containing %q, got %v", tt.err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseConfig() error = %v", err)
			}
			applyStaticDefaultsForTest(cfg)
			tt.assert(t, cfg)
		})
	}
}

func TestProviderConfigResolveAPIKey(t *testing.T) {
	tests := []struct {
		name      string
		envKey    string
		envValue  string
		expectErr string
	}{
		{
			name:     "success",
			envKey:   "OPENAI_API_KEY",
			envValue: "secret-value",
		},
		{
			name:      "missing",
			envKey:    "OPENAI_API_KEY",
			expectErr: "environment variable OPENAI_API_KEY is empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			restoreEnv(t, tt.envKey)
			if tt.envValue == "" {
				_ = os.Unsetenv(tt.envKey)
			} else {
				t.Setenv(tt.envKey, tt.envValue)
			}

			provider := ProviderConfig{
				Name:      testProviderName,
				Driver:    "openaicompat",
				BaseURL:   testBaseURL,
				Model:     testModel,
				APIKeyEnv: tt.envKey,
			}

			value, err := provider.ResolveAPIKey()
			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("ResolveAPIKey() error = %v", err)
			}
			if value != tt.envValue {
				t.Fatalf("expected %q, got %q", tt.envValue, value)
			}
		})
	}
}

func TestConfigMethodErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("selected provider on nil config", func(t *testing.T) {
		var cfg *Config
		_, err := selectedProviderConfigForTest(cfg)
		if !errors.Is(err, os.ErrInvalid) {
			t.Fatalf("expected nil config error, got %v", err)
		}
	})

	t.Run("provider lookup not found", func(t *testing.T) {
		cfg := StaticDefaults()
		_, err := cfg.ProviderByName("missing-provider")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("expected missing provider error, got %v", err)
		}
	})

	t.Run("resolve wraps missing env", func(t *testing.T) {
		restoreEnv(t, "MISSING_PROVIDER_KEY")
		_ = os.Unsetenv("MISSING_PROVIDER_KEY")

		_, err := (ProviderConfig{
			Name:      "custom",
			Driver:    "custom",
			BaseURL:   "https://example.com",
			Model:     "custom-model",
			APIKeyEnv: "MISSING_PROVIDER_KEY",
		}).Resolve()
		if err == nil || !strings.Contains(err.Error(), "MISSING_PROVIDER_KEY") {
			t.Fatalf("expected missing env resolve error, got %v", err)
		}
	})
}

func TestManagerConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewManager(NewLoader(tempDir, testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	models := []string{"gpt-4.1", "gpt-4o", "gpt-5.4", "gpt-5.3-codex"}
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				cfg := manager.Get()
				if cfg.SelectedProvider == "" {
					t.Errorf("selected provider should never be empty")
				}
				if _, err := selectedProviderConfigForTest(&cfg); err != nil {
					t.Errorf("selected provider error = %v", err)
				}
				model := models[(idx+j)%len(models)]
				if err := manager.Update(context.Background(), func(next *Config) error {
					next.CurrentModel = model
					for k := range next.Providers {
						if next.Providers[k].Name == next.SelectedProvider {
							next.Providers[k].Model = model
						}
					}
					return nil
				}); err != nil {
					t.Errorf("Update() error = %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	finalConfig := manager.Get()
	applyStaticDefaultsForTest(&finalConfig)
	if err := finalConfig.ValidateSnapshot(); err != nil {
		t.Fatalf("final config should validate, got %v", err)
	}
}

func TestConfigApplyStaticDefaultsFillsRuntimeFields(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: []ProviderConfig{
			{
				Name: testProviderName,
			},
		},
		SelectedProvider: testProviderName,
		CurrentModel:     "",
		Workdir:          ".",
	}

	applyStaticDefaultsForTest(cfg)

	if cfg.CurrentModel != "" {
		t.Fatalf("expected current model to stay empty, got %q", cfg.CurrentModel)
	}
	if cfg.Providers[0].BaseURL != "" {
		t.Fatalf("expected provider base url to stay empty, got %q", cfg.Providers[0].BaseURL)
	}
	if cfg.Providers[0].APIKeyEnv != "" {
		t.Fatalf("expected provider api key env to stay empty, got %q", cfg.Providers[0].APIKeyEnv)
	}
	if !filepath.IsAbs(cfg.Workdir) {
		t.Fatalf("expected absolute workdir, got %q", cfg.Workdir)
	}
	if cfg.Tools.WebFetch.MaxResponseBytes != DefaultWebFetchMaxResponseBytes {
		t.Fatalf("expected default webfetch max_response_bytes %d, got %d", DefaultWebFetchMaxResponseBytes, cfg.Tools.WebFetch.MaxResponseBytes)
	}
	if len(cfg.Tools.WebFetch.SupportedContentTypes) != len(DefaultWebFetchSupportedContentTypes()) {
		t.Fatalf("expected default supported content types, got %+v", cfg.Tools.WebFetch.SupportedContentTypes)
	}
}

func TestConfigValidateFailures(t *testing.T) {
	t.Parallel()

	validConfig := testDefaultConfig().Clone()
	applyStaticDefaultsForTest(&validConfig)

	tests := []struct {
		name      string
		config    *Config
		expectErr string
	}{
		{
			name:      "nil config",
			config:    nil,
			expectErr: "config is nil",
		},
		{
			name: "no providers",
			config: &Config{
				SelectedProvider: testProviderName,
				CurrentModel:     testModel,
				Workdir:          filepath.Clean(t.TempDir()),
			},
			expectErr: "providers is empty",
		},
		{
			name: "duplicate providers",
			config: &Config{
				Providers: []ProviderConfig{
					testDefaultProviderConfig(),
					testDefaultProviderConfig(),
				},
				SelectedProvider: testProviderName,
				CurrentModel:     testModel,
				Workdir:          filepath.Clean(t.TempDir()),
			},
			expectErr: "duplicate provider name",
		},
		{
			name: "relative workdir",
			config: &Config{
				Providers: []ProviderConfig{
					testDefaultProviderConfig(),
				},
				SelectedProvider: testProviderName,
				CurrentModel:     testModel,
				Workdir:          ".",
			},
			expectErr: "workdir must be absolute",
		},
		{
			name: "non-existent workdir is accepted by ValidateSnapshot (no filesystem check)",
			config: func() *Config {
				cfg := testDefaultConfig()
				cfg.Workdir = filepath.Join(t.TempDir(), "does-not-exist")
				return cfg
			}(),
			expectErr: "", // ValidateSnapshot 只校验路径格式，不做文件系统检查
		},
		{
			name: "workdir pointing to a file is accepted by ValidateSnapshot (no filesystem check)",
			config: func() *Config {
				cfg := testDefaultConfig()
				filePath := filepath.Join(t.TempDir(), "a-file.txt")
				if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
					t.Fatalf("setup: %v", err)
				}
				cfg.Workdir = filePath
				return cfg
			}(),
			expectErr: "", // ValidateSnapshot 不验证 workdir 是否为目录
		},
		{
			name: "selected provider model empty",
			config: func() *Config {
				cfg := validConfig.Clone()
				cfg.Providers[0].Model = ""
				return &cfg
			}(),
			expectErr: "model is empty",
		},
		{
			name: "invalid webfetch max response bytes",
			config: func() *Config {
				cfg := validConfig.Clone()
				cfg.Tools.WebFetch.MaxResponseBytes = 0
				return &cfg
			}(),
			expectErr: "max_response_bytes must be greater than 0",
		},
		{
			name: "invalid webfetch supported content types",
			config: func() *Config {
				cfg := validConfig.Clone()
				cfg.Tools.WebFetch.SupportedContentTypes = []string{""}
				return &cfg
			}(),
			expectErr: "supported_content_types[0] is empty",
		},
		{
			name: "duplicate provider endpoints after normalization",
			config: func() *Config {
				cfg := validConfig.Clone()
				cfg.Providers = append(cfg.Providers, ProviderConfig{
					Name:      "openai-shadow",
					Driver:    "OPENAICOMPAT",
					BaseURL:   "https://API.OPENAI.COM/v1/",
					Model:     "shadow-model",
					APIKeyEnv: "OPENAI_SHADOW_KEY",
				})
				return &cfg
			}(),
			expectErr: "duplicate provider endpoint",
		},
		{
			name: "invalid mcp duplicate server id",
			config: func() *Config {
				cfg := validConfig.Clone()
				cfg.Tools.MCP.Servers = []MCPServerConfig{
					{ID: "docs", Enabled: true, Stdio: MCPStdioConfig{Command: "cmd-1"}},
					{ID: "docs", Enabled: true, Stdio: MCPStdioConfig{Command: "cmd-2"}},
				}
				return &cfg
			}(),
			expectErr: "duplicate servers",
		},
		{
			name: "invalid mcp source",
			config: func() *Config {
				cfg := validConfig.Clone()
				cfg.Tools.MCP.Servers = []MCPServerConfig{
					{ID: "docs", Enabled: true, Source: "sse", Stdio: MCPStdioConfig{Command: "cmd"}},
				}
				return &cfg
			}(),
			expectErr: "not supported",
		},
		{
			name: "invalid mcp env binding",
			config: func() *Config {
				cfg := validConfig.Clone()
				cfg.Tools.MCP.Servers = []MCPServerConfig{
					{
						ID:      "docs",
						Enabled: true,
						Stdio:   MCPStdioConfig{Command: "cmd"},
						Env: []MCPEnvVarConfig{
							{Name: "TOKEN", Value: "a", ValueEnv: "TOKEN_ENV"},
						},
					},
				}
				return &cfg
			}(),
			expectErr: "exactly one of value/value_env",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.ValidateSnapshot()
			if tt.expectErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestConfigValidateAllowsEmptyCurrentModelForSelectedCustomProvider(t *testing.T) {
	t.Parallel()

	workdir := filepath.Clean(t.TempDir())
	cfg := Config{
		Providers: []ProviderConfig{
			testDefaultProviderConfig(),
			{
				Name:      "company-gateway",
				Driver:    "openaicompat",
				BaseURL:   "https://llm.example.com/v1",
				APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
				Source:    ProviderSourceCustom,
			},
		},
		SelectedProvider: "company-gateway",
		CurrentModel:     "",
		Workdir:          workdir,
		Shell:            "powershell",
	}
	applyStaticDefaultsForTest(&cfg)

	if err := cfg.ValidateSnapshot(); err != nil {
		t.Fatalf("expected selected custom provider with empty current_model to validate, got %v", err)
	}
}

func TestValidateSnapshotDoesNotTreatSelectionAsRuntimeReady(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Providers: []ProviderConfig{
			testDefaultProviderConfig(),
		},
		SelectedProvider: "missing-provider",
		CurrentModel:     "",
		Workdir:          filepath.Clean(t.TempDir()),
		Shell:            "powershell",
	}
	applyStaticDefaultsForTest(&cfg)

	if err := cfg.ValidateSnapshot(); err != nil {
		t.Fatalf("expected snapshot validation to ignore unresolved selection state, got %v", err)
	}
	if _, err := ResolveSelectedProvider(cfg); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected unresolved selected provider after snapshot validation, got %v", err)
	}
}

func TestMCPConfigApplyDefaultsAndClone(t *testing.T) {
	t.Parallel()

	cfg := MCPConfig{
		Servers: []MCPServerConfig{
			{
				ID:      " Docs ",
				Enabled: true,
				Source:  "",
				Stdio: MCPStdioConfig{
					Command: "mock",
					Args:    []string{"a"},
				},
			},
		},
	}
	cfg.ApplyDefaults(defaultMCPConfig())
	if cfg.Servers[0].Source != "stdio" {
		t.Fatalf("expected default source stdio, got %q", cfg.Servers[0].Source)
	}

	cloned := cfg.Clone()
	cloned.Servers[0].Stdio.Args[0] = "b"
	if cfg.Servers[0].Stdio.Args[0] == "b" {
		t.Fatalf("expected MCP clone to be independent")
	}
}

func TestProviderConfigValidateFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		provider  ProviderConfig
		expectErr string
	}{
		{
			name:      "missing name",
			provider:  ProviderConfig{},
			expectErr: "provider name is empty",
		},
		{
			name: "missing driver",
			provider: ProviderConfig{
				Name: testProviderName,
			},
			expectErr: "driver is empty",
		},
		{
			name: "custom provider must not define model",
			provider: ProviderConfig{
				Name:      "custom-openai",
				Driver:    providerpkg.DriverOpenAICompat,
				BaseURL:   "https://example.com/v1",
				Model:     "gpt-4.1",
				APIKeyEnv: "CUSTOM_API_KEY",
				Source:    ProviderSourceCustom,
			},
			expectErr: "must not define model",
		},
		{
			name: "missing base url",
			provider: ProviderConfig{
				Name:   testProviderName,
				Driver: providerpkg.DriverOpenAICompat,
			},
			expectErr: "base_url is empty",
		},
		{
			name: "missing model",
			provider: ProviderConfig{
				Name:    testProviderName,
				Driver:  providerpkg.DriverOpenAICompat,
				BaseURL: testBaseURL,
			},
			expectErr: "model is empty",
		},
		{
			name: "missing api key env",
			provider: ProviderConfig{
				Name:    testProviderName,
				Driver:  providerpkg.DriverOpenAICompat,
				BaseURL: testBaseURL,
				Model:   testModel,
			},
			expectErr: "api_key_env is empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.provider.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestProviderConfigValidateAllowsStructurallyValidCustomDriver(t *testing.T) {
	t.Parallel()

	err := (ProviderConfig{
		Name:      "custom-openai",
		Driver:    "custom-driver",
		BaseURL:   "https://example.com/v1",
		APIKeyEnv: "CUSTOM_API_KEY",
		Source:    ProviderSourceCustom,
	}).Validate()
	if err != nil {
		t.Fatalf("expected custom driver to pass structural validation, got %v", err)
	}
}

func TestProviderLookupAndResolveSelectedProvider(t *testing.T) {
	t.Setenv(testAPIKeyEnv, "lookup-key")

	manager := NewManager(NewLoader(t.TempDir(), testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	cfg := manager.Get()
	provider, err := cfg.ProviderByName("OPENAI")
	if err != nil {
		t.Fatalf("ProviderByName() error = %v", err)
	}
	if provider.Name != testProviderName {
		t.Fatalf("expected provider %q, got %q", testProviderName, provider.Name)
	}

	current := manager.Get()
	selected, err := current.ProviderByName(current.SelectedProvider)
	if err != nil {
		t.Fatalf("selected provider error = %v", err)
	}
	resolved, err := selected.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.APIKey != "lookup-key" {
		t.Fatalf("expected resolved key %q, got %q", "lookup-key", resolved.APIKey)
	}
}

func TestLoaderLoadAndSaveRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	loader := NewLoader(tempDir, testDefaultConfig())

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, err := os.Stat(loader.ConfigPath()); err != nil {
		t.Fatalf("expected config file to exist, got %v", err)
	}

	cfg.CurrentModel = "gpt-5.4"
	cfg.Providers[0].BaseURL = "https://ignored.example/v1"
	cfg.Tools.WebFetch.MaxResponseBytes = 1024
	cfg.Tools.WebFetch.SupportedContentTypes = []string{"text/html", "application/json"}
	if err := loader.Save(context.Background(), cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(loader.ConfigPath())
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "default_workdir:") || strings.Contains(text, "\nworkdir:") || strings.HasPrefix(text, "workdir:") {
		t.Fatalf("expected persisted config to avoid any workdir keys, got:\n%s", text)
	}
	if strings.Contains(text, "\nproviders:") || strings.HasPrefix(text, "providers:") {
		t.Fatalf("expected persisted config to omit providers, got:\n%s", text)
	}
	if strings.Contains(text, "provider_overrides:") {
		t.Fatalf("expected persisted config to omit provider overrides, got:\n%s", text)
	}
	if strings.Contains(text, "models:") || strings.Contains(text, "base_url:") || strings.Contains(text, "api_key_env:") {
		t.Fatalf("expected persisted config to keep only selection state and common runtime settings, got:\n%s", text)
	}

	reloaded, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() reload error = %v", err)
	}
	if reloaded.CurrentModel != "gpt-5.4" {
		t.Fatalf("expected current model %q, got %q", "gpt-5.4", reloaded.CurrentModel)
	}
	provider, err := selectedProviderConfigForTest(reloaded)
	if err != nil {
		t.Fatalf("selected provider reload error = %v", err)
	}
	if provider.Model != testModel {
		t.Fatalf("expected provider default model to stay %q, got %q", testModel, provider.Model)
	}
	if provider.BaseURL != testBaseURL {
		t.Fatalf("expected provider base url to come from builtin definition, got %q", provider.BaseURL)
	}
	if reloaded.Tools.WebFetch.MaxResponseBytes != 1024 {
		t.Fatalf("expected max_response_bytes %d, got %d", 1024, reloaded.Tools.WebFetch.MaxResponseBytes)
	}
	if len(reloaded.Tools.WebFetch.SupportedContentTypes) != 2 {
		t.Fatalf("expected persisted supported content types, got %+v", reloaded.Tools.WebFetch.SupportedContentTypes)
	}
}

func TestLoaderUsesUpdatedBuiltinProviderWhenUserHasNoOverride(t *testing.T) {
	tempDir := t.TempDir()

	initialDefaults := testDefaultConfig()
	initialDefaults.Providers[0].BaseURL = "https://old.example/v1"
	initialDefaults.CurrentModel = initialDefaults.Providers[0].Model

	initialLoader := NewLoader(tempDir, initialDefaults)
	if _, err := initialLoader.Load(context.Background()); err != nil {
		t.Fatalf("initial Load() error = %v", err)
	}

	updatedDefaults := testDefaultConfig()
	updatedDefaults.Providers[0].BaseURL = "https://new.example/v1"
	updatedDefaults.CurrentModel = updatedDefaults.Providers[0].Model

	updatedLoader := NewLoader(tempDir, updatedDefaults)
	reloaded, err := updatedLoader.Load(context.Background())
	if err != nil {
		t.Fatalf("updated Load() error = %v", err)
	}

	provider, err := selectedProviderConfigForTest(reloaded)
	if err != nil {
		t.Fatalf("selected provider error = %v", err)
	}
	if provider.BaseURL != "https://new.example/v1" {
		t.Fatalf("expected latest builtin base url, got %q", provider.BaseURL)
	}
}

func TestAssembleProvidersPreservesCustomProvidersAlongsideBuiltinSnapshot(t *testing.T) {
	t.Parallel()

	customProviders := []ProviderConfig{{
		Name:      "openai-alt",
		Driver:    "custom",
		BaseURL:   "https://example.com/v1",
		APIKeyEnv: "CUSTOM_API_KEY",
		Source:    ProviderSourceCustom,
	}}

	assembled, err := assembleProviders(testDefaultConfig().Providers, customProviders)
	if err != nil {
		t.Fatalf("assembleProviders() error = %v", err)
	}

	if len(assembled) != 2 {
		t.Fatalf("expected builtin and custom providers to coexist, got %+v", assembled)
	}
	current := Config{Providers: assembled}
	customProvider, err := current.ProviderByName("openai-alt")
	if err != nil {
		t.Fatalf("expected custom provider to be preserved, got %+v", assembled)
	}
	if customProvider.Source != ProviderSourceCustom {
		t.Fatalf("expected custom provider source, got %+v", customProvider)
	}
	provider, err := current.ProviderByName(testProviderName)
	if err != nil {
		t.Fatalf("ProviderByName(%s) error = %v", testProviderName, err)
	}
	if provider.BaseURL != testBaseURL || provider.Model != testModel || provider.APIKeyEnv != testAPIKeyEnv {
		t.Fatalf("expected builtin provider metadata, got %+v", provider)
	}
	if provider.Source != ProviderSourceBuiltin {
		t.Fatalf("expected builtin provider source, got %+v", provider)
	}
}

func TestAssembleProvidersRejectsDuplicateCustomProviderNames(t *testing.T) {
	t.Parallel()

	customProviders := []ProviderConfig{
		{
			Name:      "company-gateway",
			Driver:    "openaicompat",
			BaseURL:   "https://example-a.com/v1",
			APIKeyEnv: "COMPANY_GATEWAY_A_API_KEY",
			Source:    ProviderSourceCustom,
		},
		{
			Name:      "company-gateway",
			Driver:    "openaicompat",
			BaseURL:   "https://example-b.com/v1",
			APIKeyEnv: "COMPANY_GATEWAY_B_API_KEY",
			Source:    ProviderSourceCustom,
		},
	}

	if _, err := assembleProviders(testDefaultConfig().Providers, customProviders); err == nil || !strings.Contains(err.Error(), "duplicate provider name") {
		t.Fatalf("expected duplicate custom provider name error, got %v", err)
	}
}

func TestAssembleProvidersRejectsIdenticalDuplicateCustomProviderNames(t *testing.T) {
	t.Parallel()

	customProviders := []ProviderConfig{
		{
			Name:      "company-gateway",
			Driver:    "openaicompat",
			BaseURL:   "https://example.com/v1",
			APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
			APIStyle:  "responses",
			Source:    ProviderSourceCustom,
		},
		{
			Name:      "company-gateway",
			Driver:    "openaicompat",
			BaseURL:   "https://example.com/v1",
			APIKeyEnv: "COMPANY_GATEWAY_API_KEY",
			APIStyle:  "responses",
			Source:    ProviderSourceCustom,
		},
	}

	if _, err := assembleProviders(testDefaultConfig().Providers, customProviders); err == nil || !strings.Contains(err.Error(), "duplicate provider name") {
		t.Fatalf("expected duplicate custom provider name error, got %v", err)
	}
}

func TestApplyDefaultsPreservesDynamicCurrentModel(t *testing.T) {
	t.Parallel()

	cfg := testDefaultConfig()
	cfg.CurrentModel = "server-discovered-model"

	applyStaticDefaultsForTest(cfg)

	if cfg.CurrentModel != "server-discovered-model" {
		t.Fatalf("expected dynamic current model to be preserved, got %q", cfg.CurrentModel)
	}
}

func TestNormalizeWorkdirAndClone(t *testing.T) {
	t.Parallel()

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, value string)
	}{
		{
			name:  "dot becomes absolute",
			input: ".",
			validate: func(t *testing.T, value string) {
				t.Helper()
				if value != workingDir {
					t.Fatalf("expected working dir %q, got %q", workingDir, value)
				}
			},
		},
		{
			name:  "relative path becomes absolute",
			input: filepath.Join("internal", "config"),
			validate: func(t *testing.T, value string) {
				t.Helper()
				if !filepath.IsAbs(value) {
					t.Fatalf("expected absolute path, got %q", value)
				}
				if !strings.HasSuffix(filepath.ToSlash(value), "internal/config") {
					t.Fatalf("expected suffix internal/config, got %q", value)
				}
			},
		},
		{
			name:  "absolute path stays clean",
			input: workingDir,
			validate: func(t *testing.T, value string) {
				t.Helper()
				if value != filepath.Clean(workingDir) {
					t.Fatalf("expected %q, got %q", filepath.Clean(workingDir), value)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.validate(t, normalizeWorkdir(tt.input))
		})
	}

	var nilConfig *Config
	clonedNil := nilConfig.Clone()
	applyStaticDefaultsForTest(&clonedNil)
	if err := clonedNil.ValidateSnapshot(); err != nil {
		t.Fatalf("cloned nil config should validate, got %v", err)
	}

	cfg := testDefaultConfig()
	cloned := cfg.Clone()
	cloned.CurrentModel = "modified"
	cloned.Providers[0].BaseURL = "https://modified.example/v1"
	cloned.Tools.WebFetch.SupportedContentTypes[0] = "application/json"
	if cfg.CurrentModel == cloned.CurrentModel {
		t.Fatalf("expected clone to be independent from source")
	}
	if cfg.Providers[0].BaseURL == cloned.Providers[0].BaseURL {
		t.Fatalf("expected providers to be cloned")
	}
	if cfg.Tools.WebFetch.SupportedContentTypes[0] == cloned.Tools.WebFetch.SupportedContentTypes[0] {
		t.Fatalf("expected webfetch supported content types to be cloned")
	}
}

func TestManagerHelperMethodsAndReloads(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewManager(NewLoader(tempDir, testDefaultConfig()))

	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := manager.Save(context.Background()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := manager.Reload(context.Background()); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if got := manager.ConfigPath(); got != filepath.Join(tempDir, configName) {
		t.Fatalf("expected config path %q, got %q", filepath.Join(tempDir, configName), got)
	}
	if got := manager.BaseDir(); got != tempDir {
		t.Fatalf("expected base dir %q, got %q", tempDir, got)
	}
}

func TestLoaderDefaultsAndProviderDefaults(t *testing.T) {
	t.Parallel()

	loader := NewLoader("", testDefaultConfig())
	if loader.BaseDir() == "" {
		t.Fatalf("expected default base dir to be set")
	}
	if !strings.HasSuffix(filepath.ToSlash(loader.BaseDir()), "/"+dirName) {
		t.Fatalf("expected loader base dir to end with %q, got %q", dirName, loader.BaseDir())
	}
	if defaultBaseDir() == "" {
		t.Fatalf("expected defaultBaseDir() to return a value")
	}

	defaultCfg := testDefaultConfig()
	if len(defaultCfg.Providers) != 1 {
		t.Fatalf("expected 1 default provider, got %d", len(defaultCfg.Providers))
	}
	if defaultCfg.Providers[0].Name != testProviderName {
		t.Fatalf("expected default provider %q, got %q", testProviderName, defaultCfg.Providers[0].Name)
	}
}

func TestConstructorsRejectMissingDependencies(t *testing.T) {
	t.Run("NewLoader panics on nil defaults", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatalf("expected NewLoader to panic when defaults are nil")
			}
		}()
		_ = NewLoader(t.TempDir(), nil)
	})

	t.Run("NewManager panics on nil loader", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatalf("expected NewManager to panic when loader is nil")
			}
		}()
		_ = NewManager(nil)
	})
}

func TestCompactConfigDefaultsAndRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	loader := NewLoader(tempDir, testDefaultConfig())

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	compactCfg := cfg.Context.Compact
	if compactCfg.ManualStrategy != CompactManualStrategyKeepRecent {
		t.Fatalf("expected manual strategy %q, got %q", CompactManualStrategyKeepRecent, compactCfg.ManualStrategy)
	}
	if compactCfg.ManualKeepRecentMessages != DefaultCompactManualKeepRecentMessages {
		t.Fatalf("expected manual_keep_recent_messages=%d, got %d", DefaultCompactManualKeepRecentMessages, compactCfg.ManualKeepRecentMessages)
	}
	if compactCfg.MaxSummaryChars != DefaultCompactMaxSummaryChars {
		t.Fatalf("expected max_summary_chars=%d, got %d", DefaultCompactMaxSummaryChars, compactCfg.MaxSummaryChars)
	}
	if compactCfg.ReadTimeMaxMessageSpans != DefaultCompactReadTimeMaxMessageSpans {
		t.Fatalf(
			"expected read_time_max_message_spans=%d, got %d",
			DefaultCompactReadTimeMaxMessageSpans,
			compactCfg.ReadTimeMaxMessageSpans,
		)
	}
	if compactCfg.MicroCompactDisabled {
		t.Fatalf("expected micro compact to be enabled by default")
	}

	cfg.Context.Compact.ManualStrategy = CompactManualStrategyFullReplace
	cfg.Context.Compact.ManualKeepRecentMessages = 2
	cfg.Context.Compact.MaxSummaryChars = 900
	cfg.Context.Compact.ReadTimeMaxMessageSpans = 30
	cfg.Context.Compact.MicroCompactDisabled = true
	if err := loader.Save(context.Background(), cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(loader.ConfigPath())
	if err != nil {
		t.Fatalf("read config after save: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "manual_keep_recent_messages: 2") {
		t.Fatalf("expected persisted config to use manual_keep_recent_messages, got:\n%s", text)
	}
	if strings.Contains(text, "manual_keep_recent_spans:") {
		t.Fatalf("expected persisted config to drop legacy manual_keep_recent_spans key, got:\n%s", text)
	}
	if !strings.Contains(text, "micro_compact_disabled: true") {
		t.Fatalf("expected persisted config to include micro_compact_disabled, got:\n%s", text)
	}
	if !strings.Contains(text, "read_time_max_message_spans: 30") {
		t.Fatalf("expected persisted config to include read_time_max_message_spans, got:\n%s", text)
	}

	reloaded, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if reloaded.Context.Compact.ManualStrategy != CompactManualStrategyFullReplace {
		t.Fatalf("expected manual strategy to persist, got %q", reloaded.Context.Compact.ManualStrategy)
	}
	if reloaded.Context.Compact.ManualKeepRecentMessages != 2 {
		t.Fatalf("expected manual_keep_recent_messages=2, got %d", reloaded.Context.Compact.ManualKeepRecentMessages)
	}
	if reloaded.Context.Compact.MaxSummaryChars != 900 {
		t.Fatalf("expected max_summary_chars=900, got %d", reloaded.Context.Compact.MaxSummaryChars)
	}
	if reloaded.Context.Compact.ReadTimeMaxMessageSpans != 30 {
		t.Fatalf("expected read_time_max_message_spans=30, got %d", reloaded.Context.Compact.ReadTimeMaxMessageSpans)
	}
	if !reloaded.Context.Compact.MicroCompactDisabled {
		t.Fatalf("expected micro_compact_disabled to persist")
	}
}

func TestCompactConfigValidateFailures(t *testing.T) {
	tests := []struct {
		name      string
		compact   CompactConfig
		expectErr string
	}{
		{
			name: "invalid manual strategy",
			compact: CompactConfig{
				ManualStrategy:           "invalid",
				ManualKeepRecentMessages: 10,
				MaxSummaryChars:          1200,
				ReadTimeMaxMessageSpans:  24,
			},
			expectErr: "manual_strategy",
		},
		{
			name: "invalid manual keep messages",
			compact: CompactConfig{
				ManualStrategy:           CompactManualStrategyKeepRecent,
				ManualKeepRecentMessages: 0,
				MaxSummaryChars:          1200,
				ReadTimeMaxMessageSpans:  24,
			},
			expectErr: "manual_keep_recent_messages",
		},
		{
			name: "invalid summary chars",
			compact: CompactConfig{
				ManualStrategy:           CompactManualStrategyKeepRecent,
				ManualKeepRecentMessages: 10,
				MaxSummaryChars:          0,
				ReadTimeMaxMessageSpans:  24,
			},
			expectErr: "max_summary_chars",
		},
		{
			name: "invalid read time max spans",
			compact: CompactConfig{
				ManualStrategy:           CompactManualStrategyKeepRecent,
				ManualKeepRecentMessages: 10,
				MaxSummaryChars:          1200,
				ReadTimeMaxMessageSpans:  0,
			},
			expectErr: "read_time_max_message_spans",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tt.compact.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestCompactConfigValidateSupportsFullReplace(t *testing.T) {
	err := (CompactConfig{
		ManualStrategy:           CompactManualStrategyFullReplace,
		ManualKeepRecentMessages: 10,
		MaxSummaryChars:          1200,
		ReadTimeMaxMessageSpans:  24,
	}).Validate()
	if err != nil {
		t.Fatalf("expected full_replace strategy to validate, got %v", err)
	}
}

func restoreEnv(t *testing.T, key string) {
	t.Helper()
	value, ok := os.LookupEnv(key)
	t.Cleanup(func() {
		if !ok {
			_ = os.Unsetenv(key)
			return
		}
		_ = os.Setenv(key, value)
	})
}

func TestAutoCompactConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := StaticDefaults()

	if cfg.Context.AutoCompact.InputTokenThreshold != DefaultAutoCompactInputTokenThreshold {
		t.Fatalf("expected input_token_threshold=%d, got %d",
			DefaultAutoCompactInputTokenThreshold, cfg.Context.AutoCompact.InputTokenThreshold)
	}

	if cfg.Context.AutoCompact.Enabled != false {
		t.Fatalf("expected enabled=false, got %v", cfg.Context.AutoCompact.Enabled)
	}
}

func TestAutoCompactConfigApplyDefaults(t *testing.T) {
	t.Parallel()

	cfg := AutoCompactConfig{}
	defaults := AutoCompactConfig{
		InputTokenThreshold: 50000,
	}

	cfg.ApplyDefaults(defaults)

	if cfg.InputTokenThreshold != 50000 {
		t.Fatalf("expected threshold=50000, got %d", cfg.InputTokenThreshold)
	}
}

func TestAutoCompactConfigApplyDefaultsPreservesExplicit(t *testing.T) {
	t.Parallel()

	cfg := AutoCompactConfig{
		InputTokenThreshold: 200000,
	}
	defaults := AutoCompactConfig{
		InputTokenThreshold: 50000,
	}

	cfg.ApplyDefaults(defaults)

	if cfg.InputTokenThreshold != 200000 {
		t.Fatalf("expected explicit threshold=200000 to be preserved, got %d", cfg.InputTokenThreshold)
	}
}

func TestAutoCompactConfigApplyDefaultsNilReceiver(t *testing.T) {
	t.Parallel()

	var cfg *AutoCompactConfig
	cfg.ApplyDefaults(AutoCompactConfig{InputTokenThreshold: 50000})
}

func TestContextConfigApplyDefaultsPropagatesAutoCompactDefaults(t *testing.T) {
	t.Parallel()

	cfg := ContextConfig{}
	cfg.ApplyDefaults(ContextConfig{
		AutoCompact: AutoCompactConfig{
			InputTokenThreshold: 50000,
		},
		Compact: CompactConfig{
			ManualStrategy:           CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
			ReadTimeMaxMessageSpans:  24,
		},
	})

	if cfg.AutoCompact.InputTokenThreshold != 50000 {
		t.Fatalf("expected auto compact threshold=50000, got %d", cfg.AutoCompact.InputTokenThreshold)
	}
}

func TestAutoCompactConfigValidateEnabledWithoutThreshold(t *testing.T) {
	t.Parallel()

	cfg := AutoCompactConfig{
		Enabled:             true,
		InputTokenThreshold: 0,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "input_token_threshold") {
		t.Fatalf("expected error about input_token_threshold, got %v", err)
	}
}

func TestAutoCompactConfigValidateDisabledWithoutThreshold(t *testing.T) {
	t.Parallel()

	cfg := AutoCompactConfig{
		Enabled:             false,
		InputTokenThreshold: 0,
	}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for disabled auto compact, got %v", err)
	}
}

func TestAutoCompactConfigValidateEnabledWithThreshold(t *testing.T) {
	t.Parallel()

	cfg := AutoCompactConfig{
		Enabled:             true,
		InputTokenThreshold: 50000,
	}

	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected validation to pass, got %v", err)
	}
}

func TestAutoCompactConfigClone(t *testing.T) {
	t.Parallel()

	cfg := AutoCompactConfig{
		Enabled:             true,
		InputTokenThreshold: 75000,
	}

	cloned := cfg.Clone()

	if cfg.Enabled != cloned.Enabled {
		t.Fatalf("expected enabled=%v to be cloned, got %v", cfg.Enabled, cloned.Enabled)
	}
	if cfg.InputTokenThreshold != cloned.InputTokenThreshold {
		t.Fatalf("expected threshold=%d to be cloned, got %d",
			cfg.InputTokenThreshold, cloned.InputTokenThreshold)
	}

	cloned.InputTokenThreshold = 100000
	if cfg.InputTokenThreshold == cloned.InputTokenThreshold {
		t.Fatalf("clone should be independent from source")
	}
}

func TestAutoCompactConfigContextConfigValidate(t *testing.T) {
	t.Parallel()

	ctx := ContextConfig{
		AutoCompact: AutoCompactConfig{
			Enabled:             true,
			InputTokenThreshold: 0,
		},
		Compact: CompactConfig{
			ManualStrategy:           CompactManualStrategyKeepRecent,
			ManualKeepRecentMessages: 10,
			MaxSummaryChars:          1200,
			ReadTimeMaxMessageSpans:  24,
		},
	}

	err := ctx.Validate()
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "auto_compact") {
		t.Fatalf("expected error to contain 'auto_compact', got %v", err)
	}
}

func TestMemoConfigClone(t *testing.T) {
	t.Parallel()

	original := MemoConfig{
		Enabled:       true,
		AutoExtract:   false,
		MaxIndexLines: 100,
	}
	cloned := original.Clone()
	if cloned != original {
		t.Fatalf("Clone() = %+v, want %+v", cloned, original)
	}
	cloned.MaxIndexLines = 200
	if original.MaxIndexLines != 100 {
		t.Error("modifying clone should not affect original (value type check)")
	}
}

func TestMemoConfigApplyDefaults(t *testing.T) {
	t.Parallel()

	t.Run("fills zero MaxIndexLines", func(t *testing.T) {
		cfg := MemoConfig{Enabled: true, MaxIndexLines: 0}
		cfg.ApplyDefaults(MemoConfig{MaxIndexLines: DefaultMemoMaxIndexLines})
		if cfg.MaxIndexLines != DefaultMemoMaxIndexLines {
			t.Errorf("MaxIndexLines = %d, want %d", cfg.MaxIndexLines, DefaultMemoMaxIndexLines)
		}
	})

	t.Run("preserves explicit MaxIndexLines", func(t *testing.T) {
		cfg := MemoConfig{MaxIndexLines: 50}
		cfg.ApplyDefaults(MemoConfig{MaxIndexLines: DefaultMemoMaxIndexLines})
		if cfg.MaxIndexLines != 50 {
			t.Errorf("MaxIndexLines = %d, want 50", cfg.MaxIndexLines)
		}
	})

	t.Run("nil receiver is no-op", func(t *testing.T) {
		var cfg *MemoConfig
		cfg.ApplyDefaults(MemoConfig{MaxIndexLines: 200})
	})
}

func TestMemoConfigValidate(t *testing.T) {
	t.Parallel()

	t.Run("valid config", func(t *testing.T) {
		cfg := MemoConfig{MaxIndexLines: 100}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("valid config should not error: %v", err)
		}
	})

	t.Run("negative MaxIndexLines", func(t *testing.T) {
		cfg := MemoConfig{MaxIndexLines: -1}
		if err := cfg.Validate(); err == nil {
			t.Fatal("negative MaxIndexLines should fail validation")
		}
	})

	t.Run("zero MaxIndexLines is valid", func(t *testing.T) {
		cfg := MemoConfig{MaxIndexLines: 0}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("zero MaxIndexLines should be valid: %v", err)
		}
	})
}

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

func TestApplyDefaultsNilReceivers(t *testing.T) {
	t.Parallel()

	var toolsCfg *ToolsConfig
	toolsCfg.ApplyDefaults(ToolsConfig{})

	var ctxCfg *ContextConfig
	ctxCfg.ApplyDefaults(ContextConfig{})

	var wfCfg *WebFetchConfig
	wfCfg.ApplyDefaults(WebFetchConfig{})
}

func TestApplyStaticDefaultsNilReceiver(t *testing.T) {
	t.Parallel()

	var cfg *Config
	cfg.applyStaticDefaults(*StaticDefaults())
	if cfg != nil {
		t.Fatal("expected nil config to remain nil")
	}
}

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
				ReadTimeMaxMessageSpans:  24,
			},
		},
	}
	err := cfg.ValidateSnapshot()
	if err == nil || !strings.Contains(err.Error(), "compact") {
		t.Fatalf("expected compact error, got %v", err)
	}
}

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
		Name:      testProviderName,
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

func TestManagerUpdateRestoresProvidersWhenCleared(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewManager(NewLoader(tempDir, testDefaultConfig()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	err := manager.Update(context.Background(), func(cfg *Config) error {
		cfg.Providers = nil
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

func TestConfigCloneNilReceiverReturnsDefaults(t *testing.T) {
	t.Parallel()

	var cfg *Config
	cloned := cfg.Clone()
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
