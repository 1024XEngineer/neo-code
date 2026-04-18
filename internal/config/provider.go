package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

type ProviderSource string

const (
	ProviderSourceBuiltin ProviderSource = "builtin"
	ProviderSourceCustom  ProviderSource = "custom"
)

type ProviderConfig struct {
	Name                     string                          `yaml:"name"`
	Driver                   string                          `yaml:"driver"`
	BaseURL                  string                          `yaml:"base_url"`
	Model                    string                          `yaml:"model"`
	APIKeyEnv                string                          `yaml:"api_key_env"`
	ModelSource              string                          `yaml:"-"`
	ChatProtocol             string                          `yaml:"-"`
	ChatEndpointPath         string                          `yaml:"-"`
	DiscoveryProtocol        string                          `yaml:"-"`
	AuthStrategy             string                          `yaml:"-"`
	ResponseProfile          string                          `yaml:"-"`
	APIStyle                 string                          `yaml:"-"`
	DeploymentMode           string                          `yaml:"-"`
	APIVersion               string                          `yaml:"-"`
	DiscoveryEndpointPath    string                          `yaml:"-"`
	DiscoveryResponseProfile string                          `yaml:"-"`
	ModelFieldAliases        string                          `yaml:"-"`
	Models                   []providertypes.ModelDescriptor `yaml:"-"`
	Source                   ProviderSource                  `yaml:"-"`
}

type ResolvedProviderConfig struct {
	ProviderConfig
	APIKey string `yaml:"-"`
}

// ResolveSelectedProvider 解析当前配置中选中的 provider，并补全运行时所需的密钥信息。
func ResolveSelectedProvider(cfg Config) (ResolvedProviderConfig, error) {
	providerName := strings.TrimSpace(cfg.SelectedProvider)
	if providerName == "" {
		return ResolvedProviderConfig{}, errors.New("config: selected provider is empty")
	}

	providerCfg, err := cfg.ProviderByName(providerName)
	if err != nil {
		return ResolvedProviderConfig{}, err
	}
	return providerCfg.Resolve()
}

func (p ProviderConfig) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("provider name is empty")
	}
	if normalizeProviderDriver(p.Driver) == "" {
		return fmt.Errorf("provider %q driver is empty", p.Name)
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return fmt.Errorf("provider %q base_url is empty", p.Name)
	}
	if p.Source == ProviderSourceCustom && strings.TrimSpace(p.Model) != "" {
		return fmt.Errorf("provider %q custom providers must not define model", p.Name)
	}
	if p.Source != ProviderSourceCustom && strings.TrimSpace(p.Model) == "" {
		return fmt.Errorf("provider %q model is empty", p.Name)
	}
	if strings.TrimSpace(p.APIKeyEnv) == "" {
		return fmt.Errorf("provider %q api_key_env is empty", p.Name)
	}

	normalizedModelSource := provider.NormalizeModelSource(p.ModelSource)
	if normalizedModelSource == "" {
		normalizedModelSource = provider.ModelSourceDiscover
	}
	if normalizedModelSource == provider.ModelSourceManual && strings.TrimSpace(p.DiscoveryEndpointPath) != "" {
		return fmt.Errorf("provider %q manual model source must not set discovery_endpoint_path", p.Name)
	}
	if normalizedModelSource == provider.ModelSourceManual && len(p.Models) == 0 {
		return fmt.Errorf("provider %q manual model source requires non-empty models", p.Name)
	}

	normalizedProtocols, err := provider.NormalizeProviderProtocolSettings(
		p.Driver,
		p.ChatProtocol,
		p.ChatEndpointPath,
		p.DiscoveryProtocol,
		p.DiscoveryEndpointPath,
		p.AuthStrategy,
		p.ResponseProfile,
		p.APIStyle,
		p.DiscoveryResponseProfile,
	)
	if err != nil {
		return fmt.Errorf("provider %q: %w", p.Name, err)
	}
	if _, err := provider.NormalizeProviderDiscoveryResponseProfile(normalizedProtocols.ResponseProfile); err != nil {
		return fmt.Errorf("provider %q: %w", p.Name, err)
	}
	if _, err := p.Identity(); err != nil {
		return fmt.Errorf("provider %q: %w", p.Name, err)
	}
	return nil
}

func (p ProviderConfig) Identity() (provider.ProviderIdentity, error) {
	return providerIdentityFromConfig(p)
}

func (p ProviderConfig) ResolveAPIKey() (string, error) {
	envName := strings.TrimSpace(p.APIKeyEnv)
	if envName == "" {
		return "", fmt.Errorf("config: provider %q api_key_env is empty", p.Name)
	}

	value := strings.TrimSpace(os.Getenv(envName))
	if value != "" {
		return value, nil
	}

	// 进程环境未命中时回退读取用户级环境变量（Windows 为注册表持久化），
	// 并回填到当前进程环境，避免后续链路重复出现“变量为空”的假阴性。
	userValue, exists, err := LookupUserEnvVar(envName)
	if err != nil {
		return "", fmt.Errorf("config: lookup user environment variable %s: %w", envName, err)
	}
	if exists {
		trimmedUserValue := strings.TrimSpace(userValue)
		if trimmedUserValue != "" {
			_ = os.Setenv(envName, trimmedUserValue)
			return trimmedUserValue, nil
		}
	}

	return "", fmt.Errorf("config: environment variable %s is empty", envName)
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

func cloneProviders(providers []ProviderConfig) []ProviderConfig {
	if len(providers) == 0 {
		return nil
	}

	cloned := make([]ProviderConfig, 0, len(providers))
	for _, p := range providers {
		cloned = append(cloned, cloneProviderConfig(p))
	}
	return cloned
}

// cloneProviderConfig 返回 provider 配置的深拷贝，避免模型元数据等切片在不同快照间共享。
func cloneProviderConfig(provider ProviderConfig) ProviderConfig {
	cloned := provider
	cloned.Models = providertypes.CloneModelDescriptors(provider.Models)
	return cloned
}

func containsProviderName(providers []ProviderConfig, name string) bool {
	target := normalizeProviderName(name)
	if target == "" {
		return false
	}
	for _, p := range providers {
		if normalizeProviderName(p.Name) == target {
			return true
		}
	}
	return false
}

// normalizeConfigKey 统一规范 config 层比较使用的字符串键，避免大小写和空白造成分支漂移。
func normalizeConfigKey(value string) string {
	return provider.NormalizeKey(value)
}

// normalizeProviderName 统一规范 provider 名称，供 config 层查找、去重与比较逻辑复用。
func normalizeProviderName(name string) string {
	return provider.NormalizeKey(name)
}

// normalizeProviderDriver 统一规范 driver 名称，供 config 层校验和配置解析分支复用。
func normalizeProviderDriver(driver string) string {
	return provider.NormalizeProviderDriver(driver)
}

// providerIdentityFromConfig 根据 provider 配置构造用于去重与缓存的规范化连接身份。
func providerIdentityFromConfig(cfg ProviderConfig) (provider.ProviderIdentity, error) {
	normalizedProtocols, err := provider.NormalizeProviderProtocolSettings(
		cfg.Driver,
		cfg.ChatProtocol,
		cfg.ChatEndpointPath,
		cfg.DiscoveryProtocol,
		cfg.DiscoveryEndpointPath,
		cfg.AuthStrategy,
		cfg.ResponseProfile,
		cfg.APIStyle,
		cfg.DiscoveryResponseProfile,
	)
	if err != nil {
		return provider.ProviderIdentity{}, err
	}
	return provider.NormalizeProviderIdentity(provider.ProviderIdentity{
		Driver:                   cfg.Driver,
		BaseURL:                  cfg.BaseURL,
		ChatProtocol:             normalizedProtocols.ChatProtocol,
		ChatEndpointPath:         normalizedProtocols.ChatEndpointPath,
		DiscoveryProtocol:        normalizedProtocols.DiscoveryProtocol,
		AuthStrategy:             normalizedProtocols.AuthStrategy,
		ResponseProfile:          normalizedProtocols.ResponseProfile,
		APIStyle:                 normalizedProtocols.LegacyAPIStyle,
		DeploymentMode:           cfg.DeploymentMode,
		APIVersion:               cfg.APIVersion,
		DiscoveryEndpointPath:    normalizedProtocols.DiscoveryEndpointPath,
		DiscoveryResponseProfile: normalizedProtocols.ResponseProfile,
	})
}

// ToRuntimeConfig 将解析后的 provider 配置收敛为 provider 层使用的最小运行时输入。
func (p ResolvedProviderConfig) ToRuntimeConfig() provider.RuntimeConfig {
	normalizedProtocols, _ := provider.NormalizeProviderProtocolSettings(
		p.Driver,
		p.ChatProtocol,
		p.ChatEndpointPath,
		p.DiscoveryProtocol,
		p.DiscoveryEndpointPath,
		p.AuthStrategy,
		p.ResponseProfile,
		p.APIStyle,
		p.DiscoveryResponseProfile,
	)
	baseURL := sanitizeRuntimeBaseURL(p.BaseURL)

	return provider.RuntimeConfig{
		Name:                     p.Name,
		Driver:                   p.Driver,
		BaseURL:                  baseURL,
		DefaultModel:             p.Model,
		APIKey:                   p.APIKey,
		ChatProtocol:             normalizedProtocols.ChatProtocol,
		ChatEndpointPath:         normalizedProtocols.ChatEndpointPath,
		DiscoveryProtocol:        normalizedProtocols.DiscoveryProtocol,
		AuthStrategy:             normalizedProtocols.AuthStrategy,
		ResponseProfile:          normalizedProtocols.ResponseProfile,
		APIStyle:                 normalizedProtocols.LegacyAPIStyle,
		DeploymentMode:           p.DeploymentMode,
		APIVersion:               p.APIVersion,
		DiscoveryEndpointPath:    normalizedProtocols.DiscoveryEndpointPath,
		DiscoveryResponseProfile: normalizedProtocols.ResponseProfile,
		ModelFieldAliases:        p.ModelFieldAliases,
	}
}

// sanitizeRuntimeBaseURL 对运行时 base_url 做最小安全规整，确保不会透传 userinfo 等敏感片段。
func sanitizeRuntimeBaseURL(raw string) string {
	normalized, err := provider.NormalizeProviderBaseURL(raw)
	if err == nil {
		return normalized
	}

	parsed, parseErr := url.Parse(strings.TrimSpace(raw))
	if parseErr != nil {
		return strings.TrimSpace(raw)
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimSpace(parsed.String())
}

const (
	OpenAIName             = "openai"
	OpenAIDefaultBaseURL   = "https://api.openai.com/v1"
	OpenAIDefaultModel     = "gpt-5.4"
	OpenAIDefaultAPIKeyEnv = "OPENAI_API_KEY"

	GeminiName             = "gemini"
	GeminiDefaultBaseURL   = "https://generativelanguage.googleapis.com/v1beta/openai"
	GeminiDefaultModel     = "gemini-2.5-flash"
	GeminiDefaultAPIKeyEnv = "GEMINI_API_KEY"

	OpenLLName             = "openll"
	OpenLLDefaultBaseURL   = "https://www.openll.top/v1"
	OpenLLDefaultModel     = "gpt-5.4"
	OpenLLDefaultAPIKeyEnv = "AI_API_KEY"

	QiniuName             = "qiniu"
	QiniuDefaultBaseURL   = "https://api.qnaigc.com/v1"
	QiniuDefaultModel     = "z-ai/glm-5.1"
	QiniuDefaultAPIKeyEnv = "QINIU_API_KEY"
)

// OpenAIProvider returns the builtin OpenAI provider definition.
func OpenAIProvider() ProviderConfig {
	return ProviderConfig{
		Name:                     OpenAIName,
		Driver:                   provider.DriverOpenAICompat,
		BaseURL:                  OpenAIDefaultBaseURL,
		Model:                    OpenAIDefaultModel,
		APIKeyEnv:                OpenAIDefaultAPIKeyEnv,
		ModelSource:              provider.ModelSourceDiscover,
		ChatProtocol:             provider.ChatProtocolOpenAIChatCompletions,
		ChatEndpointPath:         "/chat/completions",
		DiscoveryProtocol:        provider.DiscoveryProtocolOpenAIModels,
		AuthStrategy:             provider.AuthStrategyBearer,
		ResponseProfile:          provider.DiscoveryResponseProfileOpenAI,
		APIStyle:                 provider.OpenAICompatibleAPIStyleChatCompletions,
		DiscoveryEndpointPath:    provider.DiscoveryEndpointPathModels,
		DiscoveryResponseProfile: provider.DiscoveryResponseProfileOpenAI,
		Source:                   ProviderSourceBuiltin,
	}
}

// GeminiProvider returns the builtin Gemini provider definition.
func GeminiProvider() ProviderConfig {
	return ProviderConfig{
		Name:                     GeminiName,
		Driver:                   provider.DriverGemini,
		BaseURL:                  GeminiDefaultBaseURL,
		Model:                    GeminiDefaultModel,
		APIKeyEnv:                GeminiDefaultAPIKeyEnv,
		ModelSource:              provider.ModelSourceDiscover,
		ChatProtocol:             provider.ChatProtocolOpenAIChatCompletions,
		ChatEndpointPath:         "/chat/completions",
		DiscoveryProtocol:        provider.DiscoveryProtocolGeminiModels,
		AuthStrategy:             provider.AuthStrategyBearer,
		ResponseProfile:          provider.DiscoveryResponseProfileGemini,
		APIStyle:                 provider.OpenAICompatibleAPIStyleChatCompletions,
		DiscoveryEndpointPath:    provider.DiscoveryEndpointPathModels,
		DiscoveryResponseProfile: provider.DiscoveryResponseProfileGemini,
		Source:                   ProviderSourceBuiltin,
	}
}

// OpenLLProvider returns the builtin OpenLL provider definition.
func OpenLLProvider() ProviderConfig {
	return ProviderConfig{
		Name:                     OpenLLName,
		Driver:                   provider.DriverOpenAICompat,
		BaseURL:                  OpenLLDefaultBaseURL,
		Model:                    OpenLLDefaultModel,
		APIKeyEnv:                OpenLLDefaultAPIKeyEnv,
		ModelSource:              provider.ModelSourceDiscover,
		ChatProtocol:             provider.ChatProtocolOpenAIChatCompletions,
		ChatEndpointPath:         "/chat/completions",
		DiscoveryProtocol:        provider.DiscoveryProtocolOpenAIModels,
		AuthStrategy:             provider.AuthStrategyBearer,
		ResponseProfile:          provider.DiscoveryResponseProfileOpenAI,
		APIStyle:                 provider.OpenAICompatibleAPIStyleChatCompletions,
		DiscoveryEndpointPath:    provider.DiscoveryEndpointPathModels,
		DiscoveryResponseProfile: provider.DiscoveryResponseProfileOpenAI,
		Source:                   ProviderSourceBuiltin,
	}
}

// QiniuProvider returns the builtin Qiniu provider definition.
func QiniuProvider() ProviderConfig {
	return ProviderConfig{
		Name:                     QiniuName,
		Driver:                   provider.DriverOpenAICompat,
		BaseURL:                  QiniuDefaultBaseURL,
		Model:                    QiniuDefaultModel,
		APIKeyEnv:                QiniuDefaultAPIKeyEnv,
		ModelSource:              provider.ModelSourceDiscover,
		ChatProtocol:             provider.ChatProtocolOpenAIChatCompletions,
		ChatEndpointPath:         "/chat/completions",
		DiscoveryProtocol:        provider.DiscoveryProtocolOpenAIModels,
		AuthStrategy:             provider.AuthStrategyBearer,
		ResponseProfile:          provider.DiscoveryResponseProfileOpenAI,
		APIStyle:                 provider.OpenAICompatibleAPIStyleChatCompletions,
		DiscoveryEndpointPath:    provider.DiscoveryEndpointPathModels,
		DiscoveryResponseProfile: provider.DiscoveryResponseProfileOpenAI,
		Source:                   ProviderSourceBuiltin,
	}
}

// DefaultProviders returns all builtin provider definitions.
func DefaultProviders() []ProviderConfig {
	return []ProviderConfig{
		OpenAIProvider(),
		GeminiProvider(),
		OpenLLProvider(),
		QiniuProvider(),
	}
}
