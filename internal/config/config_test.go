package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseConfigFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		data   string
		assert func(t *testing.T, cfg *Config)
	}{
		{
			name: "current format",
			data: `
selected_provider: openai
current_model: gpt-5.4
workdir: .
shell: powershell
tools:
  webfetch:
    max_response_bytes: 4096
    supported_content_types:
      - text/html
      - text/plain
providers:
  - name: openai
    type: openai
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
				if provider.BaseURL != "https://example.com/v1" {
					t.Fatalf("expected custom base url, got %q", provider.BaseURL)
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
			name: "legacy format",
			data: `
selected_provider: openai
current_model: gpt-4o
workspace_root: .
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
				if cfg.MaxLoops != 5 {
					t.Fatalf("expected max loops 5, got %d", cfg.MaxLoops)
				}
				provider, err := cfg.SelectedProviderConfig()
				if err != nil {
					t.Fatalf("selected provider: %v", err)
				}
				if provider.Model != "gpt-4o" {
					t.Fatalf("expected provider model gpt-4o, got %q", provider.Model)
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
			if err != nil {
				t.Fatalf("parseConfig() error = %v", err)
			}
			cfg.ApplyDefaults()
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
				Name:      ProviderOpenAI,
				Type:      ProviderOpenAI,
				BaseURL:   DefaultOpenAIBaseURL,
				Model:     DefaultOpenAIModel,
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
			Type:      "custom",
			BaseURL:   "https://example.com",
			Model:     "custom-model",
			APIKeyEnv: "MISSING_PROVIDER_KEY",
		}).Resolve()
		if err == nil || !strings.Contains(err.Error(), "MISSING_PROVIDER_KEY") {
			t.Fatalf("expected missing env resolve error, got %v", err)
		}
	})
}

func TestLoaderLoadEnvironmentSources(t *testing.T) {
	tests := []struct {
		name           string
		processDotEnv  string
		managedDotEnv  string
		expectedAPIKey string
	}{
		{
			name:           "loads key from managed env",
			managedDotEnv:  "OPENAI_API_KEY=managed-key\n",
			expectedAPIKey: "managed-key",
		},
		{
			name:           "falls back to cwd dotenv",
			processDotEnv:  "OPENAI_API_KEY=process-key\n",
			expectedAPIKey: "process-key",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			restoreEnv(t, DefaultOpenAIAPIKeyEnv)
			_ = os.Unsetenv(DefaultOpenAIAPIKeyEnv)

			previousWD, err := os.Getwd()
			if err != nil {
				t.Fatalf("Getwd() error = %v", err)
			}
			if err := os.Chdir(tempDir); err != nil {
				t.Fatalf("Chdir() error = %v", err)
			}
			t.Cleanup(func() {
				_ = os.Chdir(previousWD)
			})

			if tt.processDotEnv != "" {
				if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(tt.processDotEnv), 0o644); err != nil {
					t.Fatalf("write process .env: %v", err)
				}
			}

			loader := NewLoader(filepath.Join(tempDir, ".neocode"))
			if tt.managedDotEnv != "" {
				if err := os.MkdirAll(loader.BaseDir(), 0o755); err != nil {
					t.Fatalf("mkdir managed dir: %v", err)
				}
				if err := os.WriteFile(loader.EnvPath(), []byte(tt.managedDotEnv), 0o644); err != nil {
					t.Fatalf("write managed .env: %v", err)
				}
			}

			loader.LoadEnvironment()

			provider := ProviderConfig{
				Name:      ProviderOpenAI,
				Type:      ProviderOpenAI,
				BaseURL:   DefaultOpenAIBaseURL,
				Model:     DefaultOpenAIModel,
				APIKeyEnv: DefaultOpenAIAPIKeyEnv,
			}

			key, err := provider.ResolveAPIKey()
			if err != nil {
				t.Fatalf("ResolveAPIKey() error = %v", err)
			}
			if key != tt.expectedAPIKey {
				t.Fatalf("expected %q, got %q", tt.expectedAPIKey, key)
			}
		})
	}
}

func TestManagerUpsertEnvAndReload(t *testing.T) {
	tempDir := t.TempDir()
	loader := NewLoader(tempDir)
	manager := NewManager(loader)

	if err := manager.UpsertEnv("OPENAI_API_KEY", "first"); err != nil {
		t.Fatalf("UpsertEnv() error = %v", err)
	}
	if err := manager.UpsertEnv("OPENAI_API_KEY", "second"); err != nil {
		t.Fatalf("UpsertEnv() overwrite error = %v", err)
	}
	if err := loader.OverloadManagedEnvironment(); err != nil {
		t.Fatalf("OverloadManagedEnvironment() error = %v", err)
	}

	data, err := os.ReadFile(loader.EnvPath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Count(string(data), "OPENAI_API_KEY=") != 1 {
		t.Fatalf("expected single env assignment, got %q", string(data))
	}
	if got := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); got != "second" {
		t.Fatalf("expected env value second, got %q", got)
	}
}

func TestManagerConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewManager(NewLoader(tempDir))
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
	finalConfig.ApplyDefaults()
	if err := finalConfig.Validate(); err != nil {
		t.Fatalf("final config should validate, got %v", err)
	}
}

func TestConfigApplyDefaultsFillsMissingFields(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Providers: []ProviderConfig{
			{
				Name: ProviderOpenAI,
			},
		},
		SelectedProvider: ProviderOpenAI,
		CurrentModel:     "",
		Workdir:          ".",
	}

	cfg.ApplyDefaults()

	provider, err := cfg.SelectedProviderConfig()
	if err != nil {
		t.Fatalf("SelectedProviderConfig() error = %v", err)
	}
	if provider.BaseURL != DefaultOpenAIBaseURL {
		t.Fatalf("expected default base url %q, got %q", DefaultOpenAIBaseURL, provider.BaseURL)
	}
	if provider.APIKeyEnv != DefaultOpenAIAPIKeyEnv {
		t.Fatalf("expected default api key env %q, got %q", DefaultOpenAIAPIKeyEnv, provider.APIKeyEnv)
	}
	if cfg.CurrentModel != DefaultOpenAIModel {
		t.Fatalf("expected current model %q, got %q", DefaultOpenAIModel, cfg.CurrentModel)
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

	validConfig := Default().Clone()
	validConfig.ApplyDefaults()

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
				SelectedProvider: ProviderOpenAI,
				CurrentModel:     DefaultOpenAIModel,
				Workdir:          filepath.Clean(t.TempDir()),
			},
			expectErr: "providers is empty",
		},
		{
			name: "duplicate providers",
			config: &Config{
				Providers: []ProviderConfig{
					{Name: ProviderOpenAI, Type: ProviderOpenAI, BaseURL: DefaultOpenAIBaseURL, Model: DefaultOpenAIModel, APIKeyEnv: DefaultOpenAIAPIKeyEnv},
					{Name: ProviderOpenAI, Type: ProviderOpenAI, BaseURL: DefaultOpenAIBaseURL, Model: DefaultOpenAIModel, APIKeyEnv: DefaultOpenAIAPIKeyEnv},
				},
				SelectedProvider: ProviderOpenAI,
				CurrentModel:     DefaultOpenAIModel,
				Workdir:          filepath.Clean(t.TempDir()),
			},
			expectErr: "duplicate provider name",
		},
		{
			name: "relative workdir",
			config: &Config{
				Providers: []ProviderConfig{
					{Name: ProviderOpenAI, Type: ProviderOpenAI, BaseURL: DefaultOpenAIBaseURL, Model: DefaultOpenAIModel, APIKeyEnv: DefaultOpenAIAPIKeyEnv},
				},
				SelectedProvider: ProviderOpenAI,
				CurrentModel:     DefaultOpenAIModel,
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
			name: "missing type",
			provider: ProviderConfig{
				Name: ProviderOpenAI,
			},
			expectErr: "type is empty",
		},
		{
			name: "missing base url",
			provider: ProviderConfig{
				Name: ProviderOpenAI,
				Type: ProviderOpenAI,
			},
			expectErr: "base_url is empty",
		},
		{
			name: "missing model",
			provider: ProviderConfig{
				Name:    ProviderOpenAI,
				Type:    ProviderOpenAI,
				BaseURL: DefaultOpenAIBaseURL,
			},
			expectErr: "model is empty",
		},
		{
			name: "missing api key env",
			provider: ProviderConfig{
				Name:    ProviderOpenAI,
				Type:    ProviderOpenAI,
				BaseURL: DefaultOpenAIBaseURL,
				Model:   DefaultOpenAIModel,
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
	t.Setenv(DefaultOpenAIAPIKeyEnv, "lookup-key")

	manager := NewManager(NewLoader(t.TempDir()))
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	cfg := manager.Get()
	provider, err := cfg.ProviderByName("OPENAI")
	if err != nil {
		t.Fatalf("ProviderByName() error = %v", err)
	}
	if provider.Name != ProviderOpenAI {
		t.Fatalf("expected provider %q, got %q", ProviderOpenAI, provider.Name)
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
	loader := NewLoader(tempDir)

	cfg, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, err := os.Stat(loader.ConfigPath()); err != nil {
		t.Fatalf("expected config file to exist, got %v", err)
	}

	cfg.CurrentModel = "gpt-5.4"
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == cfg.SelectedProvider {
			cfg.Providers[i].Model = "gpt-5.4"
		}
	}
	cfg.Tools.WebFetch.MaxResponseBytes = 1024
	cfg.Tools.WebFetch.SupportedContentTypes = []string{"text/html", "application/json"}
	if err := loader.Save(context.Background(), cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloaded, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() reload error = %v", err)
	}
	if reloaded.CurrentModel != "gpt-5.4" {
		t.Fatalf("expected current model %q, got %q", "gpt-5.4", reloaded.CurrentModel)
	}
	if reloaded.Tools.WebFetch.MaxResponseBytes != 1024 {
		t.Fatalf("expected max_response_bytes %d, got %d", 1024, reloaded.Tools.WebFetch.MaxResponseBytes)
	}
	if len(reloaded.Tools.WebFetch.SupportedContentTypes) != 2 {
		t.Fatalf("expected persisted supported content types, got %+v", reloaded.Tools.WebFetch.SupportedContentTypes)
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
	clonedNil.ApplyDefaults()
	if err := clonedNil.Validate(); err != nil {
		t.Fatalf("cloned nil config should validate, got %v", err)
	}

	cfg := Default()
	cloned := cfg.Clone()
	cloned.CurrentModel = "modified"
	cloned.Tools.WebFetch.SupportedContentTypes[0] = "application/json"
	if cfg.CurrentModel == cloned.CurrentModel {
		t.Fatalf("expected clone to be independent from source")
	}
	if cfg.Tools.WebFetch.SupportedContentTypes[0] == cloned.Tools.WebFetch.SupportedContentTypes[0] {
		t.Fatalf("expected webfetch supported content types to be cloned")
	}
}

func TestManagerHelperMethodsAndReloads(t *testing.T) {
	tempDir := t.TempDir()
	manager := NewManager(NewLoader(tempDir))

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
	if got := manager.EnvPath(); got != filepath.Join(tempDir, envName) {
		t.Fatalf("expected env path %q, got %q", filepath.Join(tempDir, envName), got)
	}

	if err := manager.UpsertEnv(DefaultOpenAIAPIKeyEnv, "manager-key"); err != nil {
		t.Fatalf("UpsertEnv() error = %v", err)
	}
	manager.ReloadEnvironment()
	if err := manager.OverloadManagedEnvironment(); err != nil {
		t.Fatalf("OverloadManagedEnvironment() error = %v", err)
	}
	if got := os.Getenv(DefaultOpenAIAPIKeyEnv); got != "manager-key" {
		t.Fatalf("expected env value %q, got %q", "manager-key", got)
	}
}

func TestLoaderDefaultsAndBuiltinCatalog(t *testing.T) {
	t.Parallel()

	loader := NewLoader("")
	if loader.BaseDir() == "" {
		t.Fatalf("expected default base dir to be set")
	}
	if !strings.HasSuffix(filepath.ToSlash(loader.BaseDir()), "/"+dirName) {
		t.Fatalf("expected loader base dir to end with %q, got %q", dirName, loader.BaseDir())
	}
	if defaultBaseDir() == "" {
		t.Fatalf("expected defaultBaseDir() to return a value")
	}

	catalog := BuiltinModelCatalog()
	if len(catalog) == 0 {
		t.Fatalf("expected builtin model catalog to be non-empty")
	}
	if catalog[0].Name == "" || catalog[0].Description == "" {
		t.Fatalf("expected model catalog entries to contain name and description")
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
