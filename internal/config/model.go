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
	DefaultWorkdir                        = "."
	DefaultMaxLoops                       = 8
	DefaultToolTimeoutSec                 = 20
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
	Name      string   `yaml:"name"`
	Driver    string   `yaml:"driver"`
	BaseURL   string   `yaml:"base_url"`
	Model     string   `yaml:"model"`
	Models    []string `yaml:"models,omitempty"`
	APIKeyEnv string   `yaml:"api_key_env"`
}

type ResolvedProviderConfig struct {
	ProviderConfig
	APIKey string `yaml:"-"`
}

type ToolsConfig struct {
	WebFetch WebFetchConfig `yaml:"webfetch,omitempty"`
}

type WebFetchConfig struct {
	MaxResponseBytes      int64    `yaml:"max_response_bytes,omitempty"`
	SupportedContentTypes []string `yaml:"supported_content_types,omitempty"`
}

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
	clone.Providers = cloneProviders(c.Providers)
	clone.Tools = c.Tools.Clone()
	return clone
}

func (c *Config) ApplyDefaultsFrom(defaults Config) {
	if c == nil {
		return
	}

	if len(c.Providers) == 0 {
		c.Providers = cloneProviders(defaults.Providers)
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
	if selected, err := c.SelectedProviderConfig(); err == nil && !containsModelID(selected.SupportedModels(), c.CurrentModel) {
		c.CurrentModel = selected.Model
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
	if !containsModelID(selected.SupportedModels(), c.CurrentModel) {
		return fmt.Errorf("config: current_model %q is not supported by provider %q", c.CurrentModel, selected.Name)
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
	if models := normalizeModelIDs(p.Models); len(p.Models) > 0 {
		if len(models) == 0 {
			return fmt.Errorf("provider %q models is empty", p.Name)
		}
		if !containsModelID(models, p.Model) {
			return fmt.Errorf("provider %q default model %q is not in models", p.Name, strings.TrimSpace(p.Model))
		}
	}
	return nil
}

func (p ProviderConfig) SupportedModels() []string {
	models := normalizeModelIDs(p.Models)
	if len(models) > 0 {
		return models
	}

	model := strings.TrimSpace(p.Model)
	if model == "" {
		return nil
	}
	return []string{model}
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
	if len(provider.Models) == 0 {
		provider.Models = cloneModelIDs(base.Models)
	}
	if strings.TrimSpace(provider.APIKeyEnv) == "" {
		provider.APIKeyEnv = base.APIKeyEnv
	}

	return provider
}

func matchDefaultProvider(provider ProviderConfig, defaults []ProviderConfig) (ProviderConfig, bool) {
	name := strings.ToLower(strings.TrimSpace(provider.Name))
	if name == "" {
		return ProviderConfig{}, false
	}

	for _, candidate := range defaults {
		if strings.ToLower(candidate.Name) == name {
			return candidate, true
		}
	}

	return ProviderConfig{}, false
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

func (c *ToolsConfig) ApplyDefaults(defaults ToolsConfig) {
	if c == nil {
		return
	}

	c.WebFetch.ApplyDefaults(defaults.WebFetch)
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

func (c *WebFetchConfig) ApplyDefaults(defaults WebFetchConfig) {
	if c == nil {
		return
	}

	if c.MaxResponseBytes <= 0 {
		c.MaxResponseBytes = defaults.MaxResponseBytes
	}
	c.SupportedContentTypes = normalizeContentTypes(c.SupportedContentTypes, defaults.SupportedContentTypes)
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

func cloneProviders(providers []ProviderConfig) []ProviderConfig {
	if len(providers) == 0 {
		return nil
	}

	cloned := make([]ProviderConfig, 0, len(providers))
	for _, provider := range providers {
		next := provider
		next.Models = cloneModelIDs(provider.Models)
		cloned = append(cloned, next)
	}
	return cloned
}

func cloneModelIDs(models []string) []string {
	normalized := normalizeModelIDs(models)
	if len(normalized) == 0 {
		return nil
	}
	return append([]string(nil), normalized...)
}

func normalizeModelIDs(models []string) []string {
	if len(models) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

func containsModelID(models []string, model string) bool {
	target := strings.ToLower(strings.TrimSpace(model))
	if target == "" {
		return false
	}
	for _, candidate := range normalizeModelIDs(models) {
		if strings.EqualFold(candidate, target) {
			return true
		}
	}
	return false
}
