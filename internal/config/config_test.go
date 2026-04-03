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
