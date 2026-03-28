package config

import (
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const (
	DefaultWorkdir        = "."
	DefaultMaxLoops       = 8
	DefaultToolTimeoutSec = 20
	// DefaultWebFetchMaxResponseBytes bounds the amount of web content returned to the model.
	DefaultWebFetchMaxResponseBytes int64 = 256 * 1024
)

var defaultWebFetchSupportedContentTypes = []string{
	"text/html",
	"application/xhtml+xml",
	"text/plain",
	"application/json",
	"application/xml",
	"text/xml",
}

type Config struct {
	Providers        []ProviderConfig `yaml:"-"`
	SelectedProvider string           `yaml:"selected_provider"`
	CurrentModel     string           `yaml:"current_model"`
	Workdir          string           `yaml:"workdir"`
	Shell            string           `yaml:"shell"`
	MaxLoops         int              `yaml:"max_loops,omitempty"`
	ToolTimeoutSec   int              `yaml:"tool_timeout_sec,omitempty"`
	Tools            ToolsConfig      `yaml:"tools,omitempty"`
}

type ProviderConfig struct {
	Name      string `yaml:"name"`
	Driver    string `yaml:"type"`
	BaseURL   string `yaml:"base_url"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type ResolvedProviderConfig struct {
	ProviderConfig
	APIKey string `yaml:"-"`
}

type ProviderOverride struct {
	Name      string `yaml:"name"`
	Driver    string `yaml:"type,omitempty"`
	BaseURL   string `yaml:"base_url,omitempty"`
	Model     string `yaml:"model,omitempty"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
}

// ToolsConfig stores tool-specific configuration values.
type ToolsConfig struct {
	WebFetch WebFetchConfig `yaml:"webfetch,omitempty"`
}

// WebFetchConfig controls response filtering and limits for the webfetch tool.
type WebFetchConfig struct {
	MaxResponseBytes      int64    `yaml:"max_response_bytes,omitempty"`
	SupportedContentTypes []string `yaml:"supported_content_types,omitempty"`
}

// DefaultWebFetchSupportedContentTypes returns the default media types accepted by webfetch.
func DefaultWebFetchSupportedContentTypes() []string {
	return append([]string(nil), defaultWebFetchSupportedContentTypes...)
}

func Default() *Config {
	return &Config{
		Workdir:        DefaultWorkdir,
		Shell:          defaultShell(),
		MaxLoops:       DefaultMaxLoops,
		ToolTimeoutSec: DefaultToolTimeoutSec,
		Tools: ToolsConfig{
			WebFetch: defaultWebFetchConfig(),
		},

	}
}

func (c *Config) Clone() Config {
	if c == nil {
		return *Default()
	}

	clone := *c
	clone.Providers = append([]ProviderConfig(nil), c.Providers...)
	clone.Tools = c.Tools.Clone()
	return clone
}

func (c *Config) ApplyDefaultsFrom(defaults Config) {
	if c == nil {
		return
	}

	if len(c.Providers) == 0 {
		c.Providers = append([]ProviderConfig(nil), defaults.Providers...)
	} else {
		c.Providers = applyProviderDefaults(c.Providers, defaults.Providers)
	}

	if strings.TrimSpace(c.SelectedProvider) == "" {
		c.SelectedProvider = defaults.SelectedProvider
	}
	if strings.TrimSpace(c.CurrentModel) == "" {
		if selected, err := c.SelectedProviderConfig(); err == nil {
			c.CurrentModel = selected.Model
		} else if strings.TrimSpace(defaults.CurrentModel) != "" {
			c.CurrentModel = defaults.CurrentModel
		}
	}
	if strings.TrimSpace(c.Workdir) == "" {
		c.Workdir = defaults.Workdir
	}
	if strings.TrimSpace(c.Shell) == "" {
		c.Shell = defaults.Shell
	}
	if c.MaxLoops <= 0 {
		c.MaxLoops = defaults.MaxLoops
	}
	if c.ToolTimeoutSec <= 0 {
		c.ToolTimeoutSec = defaults.ToolTimeoutSec
	}
	c.Tools.ApplyDefaults(defaults.Tools)

	c.Workdir = normalizeWorkdir(c.Workdir)
}

func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config: config is nil")
	}
	if len(c.Providers) == 0 {
		return errors.New("config: providers is empty")
	}

	seen := make(map[string]struct{}, len(c.Providers))
	for i, provider := range c.Providers {
		if err := provider.Validate(); err != nil {
			return fmt.Errorf("config: provider[%d]: %w", i, err)
		}

		key := strings.ToLower(strings.TrimSpace(provider.Name))
		if _, exists := seen[key]; exists {
			return fmt.Errorf("config: duplicate provider name %q", provider.Name)
		}
		seen[key] = struct{}{}
	}

	if strings.TrimSpace(c.SelectedProvider) == "" {
		return errors.New("config: selected_provider is empty")
	}
	selected, err := c.SelectedProviderConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.CurrentModel) == "" {
		return errors.New("config: current_model is empty")
	}
	if strings.TrimSpace(c.Workdir) == "" {
		return errors.New("config: workdir is empty")
	}
	if !filepath.IsAbs(c.Workdir) {
		return fmt.Errorf("config: workdir must be absolute, got %q", c.Workdir)
	}
	if strings.TrimSpace(selected.Model) == "" {
		return fmt.Errorf("config: selected provider %q has empty model", selected.Name)
	}
	if err := c.Tools.Validate(); err != nil {
		return fmt.Errorf("config: tools: %w", err)
	}

	return nil
}

func (c *Config) SelectedProviderConfig() (ProviderConfig, error) {
	if c == nil {
		return ProviderConfig{}, errors.New("config: config is nil")
	}
	return c.ProviderByName(c.SelectedProvider)
}

func (c *Config) ProviderByName(name string) (ProviderConfig, error) {
	if c == nil {
		return ProviderConfig{}, errors.New("config: config is nil")
	}

	target := strings.ToLower(strings.TrimSpace(name))
	for _, provider := range c.Providers {
		if strings.ToLower(strings.TrimSpace(provider.Name)) == target {
			return provider, nil
		}
	}

	return ProviderConfig{}, fmt.Errorf("config: provider %q not found", name)
}

func (p ProviderConfig) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("provider name is empty")
	}
	if strings.TrimSpace(p.Driver) == "" {
		return fmt.Errorf("provider %q driver is empty", p.Name)
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return fmt.Errorf("provider %q base_url is empty", p.Name)
	}
	if strings.TrimSpace(p.Model) == "" {
		return fmt.Errorf("provider %q model is empty", p.Name)
	}
	if strings.TrimSpace(p.APIKeyEnv) == "" {
		return fmt.Errorf("provider %q api_key_env is empty", p.Name)
	}
	return nil
}

func (p ProviderConfig) ResolveAPIKey() (string, error) {
	envName := strings.TrimSpace(p.APIKeyEnv)
	if envName == "" {
		return "", fmt.Errorf("config: provider %q api_key_env is empty", p.Name)
	}

	value := strings.TrimSpace(os.Getenv(envName))
	if value == "" {
		return "", fmt.Errorf("config: environment variable %s is empty", envName)
	}

	return value, nil
}

func (p ProviderConfig) Resolve() (ResolvedProviderConfig, error) {
	apiKey, err := p.ResolveAPIKey()
	if err != nil {
		return ResolvedProviderConfig{}, err
	}

	return ResolvedProviderConfig{
		ProviderConfig: p,
		APIKey:         apiKey,
	}, nil
}

func applyProviderDefaults(providers []ProviderConfig, defaults []ProviderConfig) []ProviderConfig {
	out := make([]ProviderConfig, 0, len(providers))
	for _, provider := range providers {
		out = append(out, mergeProviderDefaults(provider, defaults))
	}
	return out
}

func mergeProviderDefaults(provider ProviderConfig, defaults []ProviderConfig) ProviderConfig {
	base, ok := matchDefaultProvider(provider, defaults)
	if !ok {
		return provider
	}

	if strings.TrimSpace(provider.Name) == "" {
		provider.Name = base.Name
	}
	if strings.TrimSpace(provider.Driver) == "" {
		provider.Driver = base.Driver
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		provider.BaseURL = base.BaseURL
	}
	if strings.TrimSpace(provider.Model) == "" {
		provider.Model = base.Model
	}
	if strings.TrimSpace(provider.APIKeyEnv) == "" {
		provider.APIKeyEnv = base.APIKeyEnv
	}

	return provider
}

func matchDefaultProvider(provider ProviderConfig, defaults []ProviderConfig) (ProviderConfig, bool) {
	name := strings.ToLower(strings.TrimSpace(provider.Name))
	kind := strings.ToLower(strings.TrimSpace(provider.Driver))

	for _, candidate := range defaults {
		if name != "" && strings.ToLower(candidate.Name) == name {
			return candidate, true
		}
	}
	for _, candidate := range defaults {
		if kind != "" && strings.ToLower(candidate.Driver) == kind {
			return candidate, true
		}
	}

	return ProviderConfig{}, false
}

func MergeProviderOverrides(defaults []ProviderConfig, overrides []ProviderOverride) []ProviderConfig {
	merged := append([]ProviderConfig(nil), defaults...)
	indexByName := make(map[string]int, len(merged))
	for i, provider := range merged {
		indexByName[strings.ToLower(strings.TrimSpace(provider.Name))] = i
	}

	for _, override := range overrides {
		name := strings.TrimSpace(override.Name)
		if name == "" {
			continue
		}

		key := strings.ToLower(name)
		if idx, ok := indexByName[key]; ok {
			merged[idx] = applyProviderOverride(merged[idx], override)
			continue
		}

		custom := applyProviderOverride(ProviderConfig{}, override)
		merged = append(merged, custom)
		indexByName[key] = len(merged) - 1
	}

	return merged
}

func DeriveProviderOverrides(providers []ProviderConfig, defaults []ProviderConfig) []ProviderOverride {
	if len(providers) == 0 {
		return nil
	}

	defaultByName := make(map[string]ProviderConfig, len(defaults))
	for _, provider := range defaults {
		defaultByName[strings.ToLower(strings.TrimSpace(provider.Name))] = provider
	}

	overrides := make([]ProviderOverride, 0, len(providers))
	for _, provider := range providers {
		key := strings.ToLower(strings.TrimSpace(provider.Name))
		base, ok := defaultByName[key]
		if !ok {
			override := ProviderOverride{
				Name:      strings.TrimSpace(provider.Name),
				Driver:    strings.TrimSpace(provider.Driver),
				BaseURL:   strings.TrimSpace(provider.BaseURL),
				Model:     strings.TrimSpace(provider.Model),
				APIKeyEnv: strings.TrimSpace(provider.APIKeyEnv),
			}
			if override.Name == "" {
				continue
			}
			overrides = append(overrides, override)
			continue
		}

		override := diffProviderOverride(base, provider)
		if hasProviderOverrideChanges(override) {
			overrides = append(overrides, override)
		}
	}

	return overrides
}

func applyProviderOverride(base ProviderConfig, override ProviderOverride) ProviderConfig {
	if name := strings.TrimSpace(override.Name); name != "" {
		base.Name = name
	}
	if driver := strings.TrimSpace(override.Driver); driver != "" {
		base.Driver = driver
	}
	if baseURL := strings.TrimSpace(override.BaseURL); baseURL != "" {
		base.BaseURL = baseURL
	}
	if model := strings.TrimSpace(override.Model); model != "" {
		base.Model = model
	}
	if apiKeyEnv := strings.TrimSpace(override.APIKeyEnv); apiKeyEnv != "" {
		base.APIKeyEnv = apiKeyEnv
	}

	return base
}

func diffProviderOverride(base ProviderConfig, current ProviderConfig) ProviderOverride {
	override := ProviderOverride{
		Name: strings.TrimSpace(current.Name),
	}
	if strings.TrimSpace(current.Driver) != strings.TrimSpace(base.Driver) {
		override.Driver = strings.TrimSpace(current.Driver)
	}
	if strings.TrimSpace(current.BaseURL) != strings.TrimSpace(base.BaseURL) {
		override.BaseURL = strings.TrimSpace(current.BaseURL)
	}
	if strings.TrimSpace(current.Model) != strings.TrimSpace(base.Model) {
		override.Model = strings.TrimSpace(current.Model)
	}
	if strings.TrimSpace(current.APIKeyEnv) != strings.TrimSpace(base.APIKeyEnv) {
		override.APIKeyEnv = strings.TrimSpace(current.APIKeyEnv)
	}
	return override
}

func hasProviderOverrideChanges(override ProviderOverride) bool {
	return strings.TrimSpace(override.Driver) != "" ||
		strings.TrimSpace(override.BaseURL) != "" ||
		strings.TrimSpace(override.Model) != "" ||
		strings.TrimSpace(override.APIKeyEnv) != ""
}

func normalizeWorkdir(workdir string) string {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return ""
	}

	if workdir == "." {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return workdir
	}

	if filepath.IsAbs(workdir) {
		return filepath.Clean(workdir)
	}

	if wd, err := os.Getwd(); err == nil {
		return filepath.Clean(filepath.Join(wd, workdir))
	}

	return filepath.Clean(workdir)
}

func defaultShell() string {
	if goruntime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}

func defaultWebFetchConfig() WebFetchConfig {
	return WebFetchConfig{
		MaxResponseBytes:      DefaultWebFetchMaxResponseBytes,
		SupportedContentTypes: DefaultWebFetchSupportedContentTypes(),
	}
}

func (c ToolsConfig) Clone() ToolsConfig {
	return ToolsConfig{
		WebFetch: c.WebFetch.Clone(),
	}
}

func (c *ToolsConfig) ApplyDefaults(def ToolsConfig) {
	if c == nil {
		return
	}

	c.WebFetch.ApplyDefaults(def.WebFetch)
}

func (c ToolsConfig) Validate() error {
	if err := c.WebFetch.Validate(); err != nil {
		return fmt.Errorf("webfetch: %w", err)
	}
	return nil
}

func (c WebFetchConfig) Clone() WebFetchConfig {
	clone := c
	clone.SupportedContentTypes = append([]string(nil), c.SupportedContentTypes...)
	return clone
}

func (c *WebFetchConfig) ApplyDefaults(def WebFetchConfig) {
	if c == nil {
		return
	}

	if c.MaxResponseBytes <= 0 {
		c.MaxResponseBytes = def.MaxResponseBytes
	}
	c.SupportedContentTypes = normalizeContentTypes(c.SupportedContentTypes, def.SupportedContentTypes)
}

func (c WebFetchConfig) Validate() error {
	if c.MaxResponseBytes <= 0 {
		return errors.New("max_response_bytes must be greater than 0")
	}
	if len(c.SupportedContentTypes) == 0 {
		return errors.New("supported_content_types is empty")
	}

	for i, contentType := range c.SupportedContentTypes {
		if normalizeContentType(contentType) == "" {
			return fmt.Errorf("supported_content_types[%d] is empty", i)
		}
	}
	return nil
}

func normalizeContentTypes(values []string, defaults []string) []string {
	source := values
	if len(source) == 0 {
		source = defaults
	}

	normalized := make([]string, 0, len(source))
	seen := make(map[string]struct{}, len(source))
	for _, value := range source {
		contentType := normalizeContentType(value)
		if contentType == "" {
			continue
		}
		if _, exists := seen[contentType]; exists {
			continue
		}
		seen[contentType] = struct{}{}
		normalized = append(normalized, contentType)
	}
	return normalized
}

func normalizeContentType(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return ""
	}

	mediaType, _, err := mime.ParseMediaType(trimmed)
	if err == nil {
		return mediaType
	}

	if index := strings.Index(trimmed, ";"); index >= 0 {
		return strings.TrimSpace(trimmed[:index])
	}
	return trimmed
}
