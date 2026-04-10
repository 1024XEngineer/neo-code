package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
		Driver:    testProviderName,
		BaseURL:   testBaseURL,
		Model:     testModel,
		APIKeyEnv: testAPIKeyEnv,
	}
}

func testDefaultConfig() *Config {
	cfg := Default()
	defaultProvider := testDefaultProviderConfig()
	cfg.Providers = []ProviderConfig{defaultProvider}
	cfg.SelectedProvider = defaultProvider.Name
	cfg.CurrentModel = defaultProvider.Model
	return cfg
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
			name: "current format ignores persisted provider metadata",
			data: `
selected_provider: openai
current_model: gpt-5.4
shell: powershell

provider_overrides:
tools:
  webfetch:
    max_response_bytes: 4096
    supported_content_types:
      - text/html
      - text/plain
providers:
  - name: openai
    base_url: https://example.com/v1
    model: gpt-5.4
    api_key_env: OPENAI_API_KEY
`,
			assert: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.CurrentModel != "gpt-5.4" {
					t.Fatalf("expected current model gpt-5.4, got %q", cfg.CurrentModel)
				}
				provider, err := cfg.SelectedProviderConfig()
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
			err: "legacy config key \"default_workdir\" is no longer supported",
		},
		{
			name: "legacy workdir key is rejected",
			data: `
selected_provider: openai
current_model: gpt-4.1
workdir: ./from-legacy
shell: powershell
`,
			err: "legacy config key \"workdir\" is no longer supported",
		},
		{
			name: "legacy persisted providers list keeps selection only",
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
			assert: func(t *testing.T, cfg *Config) {
				t.Helper()
				provider, err := cfg.SelectedProviderConfig()
				if err != nil {
					t.Fatalf("selected provider: %v", err)
				}
				if provider.BaseURL != testBaseURL {
					t.Fatalf("expected builtin base url %q, got %q", testBaseURL, provider.BaseURL)
				}
				if provider.Model != testModel {
					t.Fatalf("expected builtin default model %q, got %q", testModel, provider.Model)
				}
				if cfg.CurrentModel != "gpt-5.4" {
					t.Fatalf("expected selected current model to stay %q, got %q", "gpt-5.4", cfg.CurrentModel)
				}
			},
		},
		{
			name: "legacy fields are ignored",
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
			assert: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.MaxLoops != DefaultMaxLoops {
					t.Fatalf("expected legacy max_loop to be ignored, got %d", cfg.MaxLoops)
				}
				provider, err := cfg.SelectedProviderConfig()
				if err != nil {
					t.Fatalf("selected provider: %v", err)
				}
				if provider.Model != testModel {
					t.Fatalf("expected builtin default model %q, got %q", testModel, provider.Model)
				}
				if cfg.CurrentModel != "gpt-4o" {
					t.Fatalf("expected current model %q, got %q", "gpt-4o", cfg.CurrentModel)
				}
				if strings.Contains(filepath.ToSlash(cfg.Workdir), "definitely-legacy-root") {
					t.Fatalf("expected legacy workspace_root to be ignored, got %q", cfg.Workdir)
				}
				if cfg.Tools.WebFetch.MaxResponseBytes != DefaultWebFetchMaxResponseBytes {
					t.Fatalf("expected default max_response_bytes %d, got %d", DefaultWebFetchMaxResponseBytes, cfg.Tools.WebFetch.MaxResponseBytes)
				}
				if len(cfg.Tools.WebFetch.SupportedContentTypes) != len(DefaultWebFetchSupportedContentTypes()) {
					t.Fatalf("expected default supported content types, got %+v", cfg.Tools.WebFetch.SupportedContentTypes)
				}
			},
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
			cfg.ApplyDefaultsFrom(*testDefaultConfig())
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
				Driver:    testProviderName,
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
		_, err := cfg.SelectedProviderConfig()
		if err == nil || !strings.Contains(err.Error(), "config is nil") {
			t.Fatalf("expected nil config error, got %v", err)
		}
	})

	t.Run("provider lookup not found", func(t *testing.T) {
		cfg := Default()
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
				if _, err := cfg.SelectedProviderConfig(); err != nil {
					t.Errorf("SelectedProviderConfig() error = %v", err)
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
	finalConfig.ApplyDefaultsFrom(*testDefaultConfig())
	if err := finalConfig.Validate(); err != nil {
		t.Fatalf("final config should validate, got %v", err)
	}
}

func TestConfigApplyDefaultsFillsMissingFields(t *testing.T) {
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

	cfg.ApplyDefaultsFrom(*testDefaultConfig())

	provider, err := cfg.SelectedProviderConfig()
	if err != nil {
		t.Fatalf("SelectedProviderConfig() error = %v", err)
	}
	if provider.BaseURL != testBaseURL {
		t.Fatalf("expected default base url %q, got %q", testBaseURL, provider.BaseURL)
	}
	if provider.APIKeyEnv != testAPIKeyEnv {
		t.Fatalf("expected default api key env %q, got %q", testAPIKeyEnv, provider.APIKeyEnv)
	}
	if cfg.CurrentModel != testModel {
		t.Fatalf("expected current model %q, got %q", testModel, cfg.CurrentModel)
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
	validConfig.ApplyDefaultsFrom(*testDefaultConfig())

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
					Driver:    "OPENAI",
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
			err := tt.config.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
				t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
			}
		})
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
			name: "missing base url",
			provider: ProviderConfig{
				Name:   testProviderName,
				Driver: testProviderName,
			},
			expectErr: "base_url is empty",
		},
		{
			name: "missing model",
			provider: ProviderConfig{
				Name:    testProviderName,
				Driver:  testProviderName,
				BaseURL: testBaseURL,
			},
			expectErr: "model is empty",
		},
		{
			name: "missing api key env",
			provider: ProviderConfig{
				Name:    testProviderName,
				Driver:  testProviderName,
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

	resolved, err := manager.ResolvedSelectedProvider()
	if err != nil {
		t.Fatalf("ResolvedSelectedProvider() error = %v", err)
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
	provider, err := reloaded.SelectedProviderConfig()
	if err != nil {
		t.Fatalf("SelectedProviderConfig() reload error = %v", err)
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

	provider, err := reloaded.SelectedProviderConfig()
	if err != nil {
		t.Fatalf("SelectedProviderConfig() error = %v", err)
	}
	if provider.BaseURL != "https://new.example/v1" {
		t.Fatalf("expected latest builtin base url, got %q", provider.BaseURL)
	}
}

func TestApplyDefaultsReplacesProvidersWithBuiltinSnapshot(t *testing.T) {
	t.Parallel()

	current := Config{
		Providers: []ProviderConfig{{
			Name:      "openai-alt",
			Driver:    "custom",
			BaseURL:   "https://example.com/v1",
			Model:     "custom-model",
			APIKeyEnv: "CUSTOM_API_KEY",
		}},
		SelectedProvider: "openai-alt",
		CurrentModel:     "custom-model",
	}

	current.ApplyDefaultsFrom(*testDefaultConfig())

	if len(current.Providers) != 1 {
		t.Fatalf("expected builtin provider snapshot, got %+v", current.Providers)
	}
	if _, err := current.ProviderByName("openai-alt"); err == nil {
		t.Fatalf("expected custom provider to be dropped, got %+v", current.Providers)
	}
	provider, err := current.ProviderByName(testProviderName)
	if err != nil {
		t.Fatalf("ProviderByName(%s) error = %v", testProviderName, err)
	}
	if provider.BaseURL != testBaseURL || provider.Model != testModel || provider.APIKeyEnv != testAPIKeyEnv {
		t.Fatalf("expected builtin provider metadata, got %+v", provider)
	}
	if current.SelectedProvider != testProviderName {
		t.Fatalf("expected selected provider to reset to builtin %q, got %q", testProviderName, current.SelectedProvider)
	}
	if current.CurrentModel != testModel {
		t.Fatalf("expected current model to reset with selected builtin provider, got %q", current.CurrentModel)
	}
}

func TestApplyDefaultsPreservesDynamicCurrentModel(t *testing.T) {
	t.Parallel()

	cfg := testDefaultConfig()
	cfg.CurrentModel = "server-discovered-model"

	cfg.ApplyDefaultsFrom(*testDefaultConfig())

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
	clonedNil.ApplyDefaultsFrom(*testDefaultConfig())
	if err := clonedNil.Validate(); err != nil {
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
	if compactCfg.MicroCompactDisabled {
		t.Fatalf("expected micro compact to be enabled by default")
	}

	cfg.Context.Compact.ManualStrategy = CompactManualStrategyFullReplace
	cfg.Context.Compact.ManualKeepRecentMessages = 2
	cfg.Context.Compact.MaxSummaryChars = 900
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
			},
			expectErr: "manual_strategy",
		},
		{
			name: "invalid manual keep messages",
			compact: CompactConfig{
				ManualStrategy:           CompactManualStrategyKeepRecent,
				ManualKeepRecentMessages: 0,
				MaxSummaryChars:          1200,
			},
			expectErr: "manual_keep_recent_messages",
		},
		{
			name: "invalid summary chars",
			compact: CompactConfig{
				ManualStrategy:           CompactManualStrategyKeepRecent,
				ManualKeepRecentMessages: 10,
				MaxSummaryChars:          0,
			},
			expectErr: "max_summary_chars",
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

	cfg := Default()

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

// ---- selection.go 工具函数覆盖 ----

func TestDescriptorFromRawModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		raw    map[string]any
		want   ModelDescriptor
		wantOK bool
	}{
		{
			name:   "empty map returns false",
			raw:    map[string]any{},
			wantOK: false,
		},
		{
			name: "id from model field",
			raw: map[string]any{
				"model": "gpt-4.1",
			},
			want:   ModelDescriptor{ID: "gpt-4.1", Name: "gpt-4.1"},
			wantOK: true,
		},
		{
			name: "full descriptor",
			raw: map[string]any{
				"id":                "gpt-4.1",
				"display_name":      "GPT-4.1",
				"description":       "desc",
				"context_window":    128000,
				"max_output_tokens": 16384,
			},
			want: ModelDescriptor{
				ID:              "gpt-4.1",
				Name:            "GPT-4.1",
				Description:     "desc",
				ContextWindow:   128000,
				MaxOutputTokens: 16384,
			},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := DescriptorFromRawModel(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got ok=%v", tt.wantOK, ok)
			}
			if !tt.wantOK {
				return
			}
			if got.ID != tt.want.ID {
				t.Fatalf("expected ID=%q, got %q", tt.want.ID, got.ID)
			}
			if got.Name != tt.want.Name {
				t.Fatalf("expected Name=%q, got %q", tt.want.Name, got.Name)
			}
			if got.Description != tt.want.Description {
				t.Fatalf("expected Description=%q, got %q", tt.want.Description, got.Description)
			}
			if got.ContextWindow != tt.want.ContextWindow {
				t.Fatalf("expected ContextWindow=%d, got %d", tt.want.ContextWindow, got.ContextWindow)
			}
			if got.MaxOutputTokens != tt.want.MaxOutputTokens {
				t.Fatalf("expected MaxOutputTokens=%d, got %d", tt.want.MaxOutputTokens, got.MaxOutputTokens)
			}
		})
	}
}

func TestMergeModelDescriptors(t *testing.T) {
	t.Parallel()

	a := []ModelDescriptor{{ID: "m1", Name: "Model1"}}
	b := []ModelDescriptor{{ID: "m2", Name: "Model2"}, {ID: "m1", Description: "fallback"}}

	merged := MergeModelDescriptors(a, b)
	if len(merged) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(merged))
	}

	// m1 from first source should keep its Name, get description from second
	var m1 *ModelDescriptor
	for i := range merged {
		if merged[i].ID == "m1" {
			m1 = &merged[i]
			break
		}
	}
	if m1 == nil {
		t.Fatalf("expected m1 to be present")
	}
	if m1.Name != "Model1" {
		t.Fatalf("expected Name=Model1 from first source, got %q", m1.Name)
	}
	if m1.Description != "fallback" {
		t.Fatalf("expected Description=fallback from second source, got %q", m1.Description)
	}
}

func TestDescriptorsFromIDs(t *testing.T) {
	t.Parallel()

	result := DescriptorsFromIDs([]string{"gpt-4.1", "gpt-4.1-mini"})
	if len(result) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(result))
	}
	if result[0].ID != "gpt-4.1" {
		t.Fatalf("expected first ID=gpt-4.1, got %q", result[0].ID)
	}
	if result[1].Name != "gpt-4.1-mini" {
		t.Fatalf("expected second Name=gpt-4.1-mini, got %q", result[1].Name)
	}
}

func TestFirstNonEmptyString(t *testing.T) {
	t.Parallel()

	if got := firstNonEmptyString("", "  ", "hello", "world"); got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
	if got := firstNonEmptyString("", "  "); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestFirstPositiveInt(t *testing.T) {
	t.Parallel()

	if got := firstPositiveInt(0, -1, 42, 100); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
	if got := firstPositiveInt(int32(5)); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
	if got := firstPositiveInt(int64(10)); got != 10 {
		t.Fatalf("expected 10, got %d", got)
	}
	if got := firstPositiveInt(float64(3.14)); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
	if got := firstPositiveInt(0, -5); got != 0 {
		t.Fatalf("expected 0 when none positive, got %d", got)
	}
}

func TestBoolMapValue(t *testing.T) {
	t.Parallel()

	result := boolMapValue(map[string]any{"a": true, "b": "notbool", "c": false})
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if !result["a"] {
		t.Fatalf("expected a=true")
	}
	if result["c"] {
		t.Fatalf("expected c=false")
	}

	if result := boolMapValue("not a map"); result != nil {
		t.Fatalf("expected nil for non-map, got %v", result)
	}
}

func TestMergeStringBoolMaps(t *testing.T) {
	t.Parallel()

	primary := map[string]bool{"a": true}
	secondary := map[string]bool{"b": false, "a": false}

	result := mergeStringBoolMaps(primary, secondary)
	if !result["a"] {
		t.Fatalf("expected a=true (primary should win)")
	}
	if result["b"] {
		t.Fatalf("expected b=false")
	}

	if result := mergeStringBoolMaps(nil, nil); result != nil {
		t.Fatalf("expected nil for both empty")
	}
}

func TestMergeModelDescriptorFallback(t *testing.T) {
	t.Parallel()

	primary := ModelDescriptor{ID: "m1"}
	secondary := ModelDescriptor{
		Name:            "Fallback",
		Description:     "desc",
		ContextWindow:   8000,
		MaxOutputTokens: 4096,
	}

	result := mergeModelDescriptor(primary, secondary)
	if result.Name != "Fallback" {
		t.Fatalf("expected Name=Fallback from secondary, got %q", result.Name)
	}
	if result.ContextWindow != 8000 {
		t.Fatalf("expected ContextWindow=8000 from secondary, got %d", result.ContextWindow)
	}
	if result.MaxOutputTokens != 4096 {
		t.Fatalf("expected MaxOutputTokens=4096 from secondary, got %d", result.MaxOutputTokens)
	}
}
