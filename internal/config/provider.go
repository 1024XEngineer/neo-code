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
	Name                  string                          `yaml:"name"`
	Driver                string                          `yaml:"driver"`
	BaseURL               string                          `yaml:"base_url"`
	Model                 string                          `yaml:"model"`
	APIKeyEnv             string                          `yaml:"api_key_env"`
	ModelSource           string                          `yaml:"-"`
	ChatAPIMode           string                          `yaml:"-"`
	ChatEndpointPath      string                          `yaml:"-"`
	DiscoveryEndpointPath string                          `yaml:"-"`
	Models                []providertypes.ModelDescriptor `yaml:"-"`
	Source                ProviderSource                  `yaml:"-"`
}

type ResolvedProviderConfig struct {
	ProviderConfig
	APIKey             string                           `yaml:"-"`
	SessionAssetLimits providertypes.SessionAssetLimits `yaml:"-"`
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
	resolved, err := providerCfg.Resolve()
	if err != nil {
		return ResolvedProviderConfig{}, err
	}
	resolved.SessionAssetLimits = cfg.Runtime.ResolveSessionAssetLimits()
	return resolved, nil
}

func (p ProviderConfig) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("provider name is empty")
	}
	normalizedDriver := normalizeProviderDriver(p.Driver)
	if normalizedDriver == "" {
		return fmt.Errorf("provider %q driver is empty", p.Name)
	}
	if normalizedDriver != provider.DriverOpenAICompat && strings.TrimSpace(p.ChatAPIMode) != "" {
		return fmt.Errorf("provider %q chat_api_mode is only supported for openaicompat driver", p.Name)
	}
	if strings.TrimSpace(p.BaseURL) == "" && !allowsEmptyBaseURL(normalizedDriver) {
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

	normalizedModelSource := NormalizeModelSource(p.ModelSource)
	if normalizedModelSource == "" {
		normalizedModelSource = ModelSourceDiscover
	}
	if normalizedModelSource == ModelSourceManual && len(p.Models) == 0 {
		return fmt.Errorf("provider %q manual model source requires non-empty models", p.Name)
	}
	if _, err := provider.NormalizeProviderChatAPIMode(p.ChatAPIMode); err != nil {
		return fmt.Errorf("provider %q: %w", p.Name, err)
	}
	if p.Source == ProviderSourceCustom && normalizedModelSource == ModelSourceDiscover &&
		requiresDiscoveryEndpointPath(p.Driver) &&
		strings.TrimSpace(p.DiscoveryEndpointPath) == "" {
		return fmt.Errorf(
			"provider %q model source discover requires discovery_endpoint_path; set model_source to manual if endpoint is unavailable",
			p.Name,
		)
	}

	if _, _, err := normalizeProviderRuntimePathsFromConfig(p); err != nil {
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
	baseURL := identityBaseURL(cfg)
	chatAPIMode, err := provider.NormalizeProviderChatAPIMode(cfg.ChatAPIMode)
	if err != nil {
		return provider.ProviderIdentity{}, err
	}
	identity := provider.ProviderIdentity{
		Driver:      cfg.Driver,
		BaseURL:     baseURL,
		ChatAPIMode: chatAPIMode,
	}

	if normalizeProviderDriver(cfg.Driver) == provider.DriverOpenAICompat {
		chatEndpointPath, err := provider.NormalizeProviderChatEndpointPath(cfg.ChatEndpointPath)
		if err != nil {
			return provider.ProviderIdentity{}, err
		}
		discoveryEndpointPath, err := normalizeProviderDiscoverySettingsFromConfig(cfg)
		if err != nil {
			return provider.ProviderIdentity{}, err
		}
		identity.ChatEndpointPath = chatEndpointPath
		identity.DiscoveryEndpointPath = discoveryEndpointPath
		return provider.NormalizeProviderIdentity(identity)
	}

	normalizedDriver := normalizeProviderDriver(cfg.Driver)
	if normalizedDriver == provider.DriverGemini || normalizedDriver == provider.DriverAnthropic {
		return provider.NormalizeProviderIdentity(identity)
	}

	discoveryEndpointPath, err := normalizeProviderDiscoverySettingsFromConfig(cfg)
	if err != nil {
		return provider.ProviderIdentity{}, err
	}
	identity.DiscoveryEndpointPath = discoveryEndpointPath
	return provider.NormalizeProviderIdentity(identity)
}

// ToRuntimeConfig 将解析后的 provider 配置收敛为 provider 层使用的最小运行时输入。
func (p ResolvedProviderConfig) ToRuntimeConfig() (provider.RuntimeConfig, error) {
	chatEndpointPath, discoveryEndpointPath, err := normalizeProviderRuntimePathsFromConfig(p.ProviderConfig)
	if err != nil {
		return provider.RuntimeConfig{}, err
	}
	chatAPIMode, err := provider.NormalizeProviderChatAPIMode(p.ChatAPIMode)
	if err != nil {
		return provider.RuntimeConfig{}, err
	}
	if normalizeProviderDriver(p.Driver) != provider.DriverOpenAICompat {
		chatAPIMode = ""
	}
	baseURL := sanitizeRuntimeBaseURL(p.BaseURL)

	return provider.RuntimeConfig{
		Name:                  p.Name,
		Driver:                p.Driver,
		BaseURL:               baseURL,
		DefaultModel:          p.Model,
		APIKey:                p.APIKey,
		SessionAssetLimits:    p.SessionAssetLimits,
		ChatAPIMode:           chatAPIMode,
		ChatEndpointPath:      chatEndpointPath,
		DiscoveryEndpointPath: discoveryEndpointPath,
	}, nil
}

// normalizeProviderDiscoverySettingsFromConfig 归一化 discovery 所需的最小路径配置。
func normalizeProviderDiscoverySettingsFromConfig(cfg ProviderConfig) (string, error) {
	return provider.NormalizeProviderDiscoverySettings(cfg.Driver, cfg.DiscoveryEndpointPath)
}

// normalizeProviderRuntimePathsFromConfig 归一化运行时真正消费的端点路径。
func normalizeProviderRuntimePathsFromConfig(cfg ProviderConfig) (string, string, error) {
	chatEndpointPath, err := provider.NormalizeProviderChatEndpointPath(cfg.ChatEndpointPath)
	if err != nil {
		return "", "", err
	}
	discoveryEndpointPath := ""
	if requiresDiscoveryEndpointPath(cfg.Driver) || strings.TrimSpace(cfg.DiscoveryEndpointPath) != "" {
		discoveryEndpointPath, err = normalizeProviderDiscoverySettingsFromConfig(cfg)
		if err != nil {
			return "", "", err
		}
	}
	if normalizeProviderDriver(cfg.Driver) != provider.DriverOpenAICompat {
		chatEndpointPath = ""
	}
	return chatEndpointPath, discoveryEndpointPath, nil
}

// requiresDiscoveryEndpointPath 标记哪些 driver 的 discover 仍依赖 HTTP endpoint 配置。
func requiresDiscoveryEndpointPath(driver string) bool {
	return normalizeProviderDriver(driver) == provider.DriverOpenAICompat
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

// allowsEmptyBaseURL 判断指定 driver 是否允许通过 SDK 默认地址运行。
func allowsEmptyBaseURL(driver string) bool {
	switch normalizeProviderDriver(driver) {
	case provider.DriverGemini, provider.DriverAnthropic:
		return true
	default:
		return false
	}
}

// identityBaseURL 返回用于身份归一化的 base_url，确保空值场景也有稳定键。
func identityBaseURL(cfg ProviderConfig) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		return cfg.BaseURL
	}
	switch normalizeProviderDriver(cfg.Driver) {
	case provider.DriverGemini:
		return GeminiDefaultBaseURL
	case provider.DriverAnthropic:
		return AnthropicDefaultBaseURL
	default:
		return cfg.BaseURL
	}
}

const (
	OpenAIName             = "openai"
	OpenAIDefaultBaseURL   = "https://api.openai.com/v1"
	OpenAIDefaultModel     = "gpt-5.4"
	OpenAIDefaultAPIKeyEnv = "OPENAI_API_KEY"

	GeminiName             = "gemini"
	GeminiDefaultBaseURL   = "https://generativelanguage.googleapis.com/v1beta"
	GeminiDefaultModel     = "gemini-2.5-flash"
	GeminiDefaultAPIKeyEnv = "GEMINI_API_KEY"

	AnthropicDefaultBaseURL = "https://api.anthropic.com/v1"

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
		Name:                  OpenAIName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               OpenAIDefaultBaseURL,
		Model:                 OpenAIDefaultModel,
		APIKeyEnv:             OpenAIDefaultAPIKeyEnv,
		ModelSource:           ModelSourceDiscover,
		ChatAPIMode:           provider.ChatAPIModeChatCompletions,
		ChatEndpointPath:      "/chat/completions",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceBuiltin,
	}
}

// GeminiProvider returns the builtin Gemini provider definition.
func GeminiProvider() ProviderConfig {
	return ProviderConfig{
		Name:                  GeminiName,
		Driver:                provider.DriverGemini,
		BaseURL:               GeminiDefaultBaseURL,
		Model:                 GeminiDefaultModel,
		APIKeyEnv:             GeminiDefaultAPIKeyEnv,
		ModelSource:           ModelSourceDiscover,
		ChatEndpointPath:      "",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceBuiltin,
	}
}

// OpenLLProvider returns the builtin OpenLL provider definition.
func OpenLLProvider() ProviderConfig {
	return ProviderConfig{
		Name:                  OpenLLName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               OpenLLDefaultBaseURL,
		Model:                 OpenLLDefaultModel,
		APIKeyEnv:             OpenLLDefaultAPIKeyEnv,
		ModelSource:           ModelSourceDiscover,
		ChatAPIMode:           provider.ChatAPIModeChatCompletions,
		ChatEndpointPath:      "/chat/completions",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceBuiltin,
	}
}

// QiniuProvider returns the builtin Qiniu provider definition.
func QiniuProvider() ProviderConfig {
	return ProviderConfig{
		Name:                  QiniuName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               QiniuDefaultBaseURL,
		Model:                 QiniuDefaultModel,
		APIKeyEnv:             QiniuDefaultAPIKeyEnv,
		ModelSource:           ModelSourceDiscover,
		ChatAPIMode:           provider.ChatAPIModeChatCompletions,
		ChatEndpointPath:      "/chat/completions",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceBuiltin,
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
