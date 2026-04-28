package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"
	"time"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/session"
)

type ProviderSource string

const (
	ProviderSourceBuiltin ProviderSource = "builtin"
	ProviderSourceCustom  ProviderSource = "custom"
)

type ProviderConfig struct {
	Name                   string                          `yaml:"name"`
	Driver                 string                          `yaml:"driver"`
	BaseURL                string                          `yaml:"base_url"`
	Model                  string                          `yaml:"model"`
	APIKeyEnv              string                          `yaml:"api_key_env"`
	GenerateMaxRetries     int                             `yaml:"generate_max_retries,omitempty"`
	GenerateMaxRetriesSet  bool                            `yaml:"-"`
	GenerateIdleTimeoutSec int                             `yaml:"generate_idle_timeout_sec,omitempty"`
	ModelSource            string                          `yaml:"-"`
	ChatAPIMode            string                          `yaml:"-"`
	ChatEndpointPath       string                          `yaml:"-"`
	DiscoveryEndpointPath  string                          `yaml:"-"`
	Models                 []providertypes.ModelDescriptor `yaml:"-"`
	Source                 ProviderSource                  `yaml:"-"`
}

type ResolvedProviderConfig struct {
	ProviderConfig
	GenerateStartTimeoutSec int                         `yaml:"-"`
	SessionAssetPolicy      session.AssetPolicy         `yaml:"-"`
	RequestAssetBudget      provider.RequestAssetBudget `yaml:"-"`
}

// ResolveSelectedProvider 解析当前配置中选中的 provider，并补全运行时所需的运行时策略。
func ResolveSelectedProvider(cfg Config) (ResolvedProviderConfig, error) {
	providerName := strings.TrimSpace(cfg.SelectedProvider)
	if providerName == "" {
		return ResolvedProviderConfig{}, errors.New("config: selected provider is empty")
	}

	providerCfg, err := cfg.ProviderByName(providerName)
	if err != nil {
		return ResolvedProviderConfig{}, err
	}
	resolved := ResolvedProviderConfig{
		ProviderConfig:          providerCfg,
		GenerateStartTimeoutSec: cfg.GenerateStartTimeoutSec,
	}
	resolved.SessionAssetPolicy = cfg.Runtime.ResolveSessionAssetPolicy()
	resolved.RequestAssetBudget = cfg.Runtime.ResolveRequestAssetBudget()
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
	if err := validateOptionalGenerateMaxRetries(p.GenerateMaxRetries); err != nil {
		return fmt.Errorf("provider %q: %w", p.Name, err)
	}
	if err := validateOptionalGenerateDurationSeconds("generate_idle_timeout_sec", p.GenerateIdleTimeoutSec); err != nil {
		return fmt.Errorf("provider %q: %w", p.Name, err)
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

// validateOptionalNonNegativeGenerateControl 校验可选的整型生成控制字段，拒绝会被运行时静默吞掉的负数输入。
func validateOptionalNonNegativeGenerateControl(field string, value int) error {
	if value < 0 {
		return fmt.Errorf("%s must be greater than or equal to 0", field)
	}
	return nil
}

// validateOptionalGenerateMaxRetries 校验额外重试次数，防止超大值导致生成阶段重试循环过长。
func validateOptionalGenerateMaxRetries(value int) error {
	if err := validateOptionalNonNegativeGenerateControl("generate_max_retries", value); err != nil {
		return err
	}
	if value > provider.MaxGenerateMaxRetries {
		return fmt.Errorf("generate_max_retries must be less than or equal to %d", provider.MaxGenerateMaxRetries)
	}
	return nil
}

// validateOptionalGenerateDurationSeconds 校验秒级超时字段，避免负值和 duration 溢出在运行时被悄悄回退为默认值。
func validateOptionalGenerateDurationSeconds(field string, value int) error {
	if err := validateOptionalNonNegativeGenerateControl(field, value); err != nil {
		return err
	}
	if int64(value) > math.MaxInt64/int64(time.Second) {
		return fmt.Errorf("%s exceeds supported range", field)
	}
	return nil
}

func (p ProviderConfig) Identity() (provider.ProviderIdentity, error) {
	return providerIdentityFromConfig(p)
}

func (p ProviderConfig) ResolveAPIKey() (string, error) {
	if strings.TrimSpace(p.APIKeyEnv) == "" {
		return "", fmt.Errorf("config: provider %q api_key_env is empty", p.Name)
	}
	return resolveRuntimeAPIKey(p.APIKeyEnv)
}

func (p ProviderConfig) Resolve() (ResolvedProviderConfig, error) {
	return ResolvedProviderConfig{
		ProviderConfig: p,
	}, nil
}

// resolveGenerateMaxRetries 统一解析 provider 级生成重试次数，兼容“未配置使用默认值”和“显式 0 关闭重试”两种语义。
func (p ProviderConfig) resolveGenerateMaxRetries() int {
	if p.GenerateMaxRetriesSet || p.GenerateMaxRetries > 0 {
		return provider.NormalizeGenerateMaxRetries(p.GenerateMaxRetries)
	}
	return provider.DefaultGenerateMaxRetries
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
		APIKeyEnv:             p.APIKeyEnv,
		APIKeyResolver:        resolveRuntimeAPIKey,
		SessionAssetPolicy:    p.SessionAssetPolicy,
		RequestAssetBudget:    p.RequestAssetBudget,
		ChatAPIMode:           chatAPIMode,
		ChatEndpointPath:      chatEndpointPath,
		DiscoveryEndpointPath: discoveryEndpointPath,
		GenerateMaxRetries:    p.resolveGenerateMaxRetries(),
		GenerateStartTimeout:  provider.NormalizeGenerateStartTimeout(time.Duration(p.GenerateStartTimeoutSec) * time.Second),
		GenerateIdleTimeout:   provider.NormalizeGenerateIdleTimeout(time.Duration(p.GenerateIdleTimeoutSec) * time.Second),
	}, nil
}

// resolveRuntimeAPIKey 在 provider 真正发起请求前解析 API Key，并在需要时补回当前进程环境。
func resolveRuntimeAPIKey(envName string) (string, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return "", errors.New("config: provider api_key_env is empty")
	}

	value := strings.TrimSpace(os.Getenv(envName))
	if value != "" {
		return value, nil
	}

	userValue, exists, err := LookupUserEnvVar(envName)
	if err != nil {
		return "", fmt.Errorf("config: lookup user environment variable %s: %w", envName, err)
	}
	if exists {
		trimmedUserValue := strings.TrimSpace(userValue)
		if trimmedUserValue != "" {
			return trimmedUserValue, nil
		}
	}

	return "", fmt.Errorf("config: environment variable %s is empty", envName)
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

	QiniuName             = "qiniu"
	QiniuDefaultBaseURL   = "https://api.qnaigc.com/v1"
	QiniuDefaultModel     = "z-ai/glm-5.1"
	QiniuDefaultAPIKeyEnv = "QINIU_API_KEY"

	ModelScopeName             = "modelscope"
	ModelScopeDefaultBaseURL   = "https://api-inference.modelscope.cn/v1"
	ModelScopeDefaultModel     = "Qwen/Qwen2.5-7B-Instruct"
	ModelScopeDefaultAPIKeyEnv = "MODELSCOPE_API_KEY"
)

func newBuiltinOpenAICompatProvider(name, baseURL, model, apiKeyEnv string) ProviderConfig {
	return ProviderConfig{
		Name:                  name,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               baseURL,
		Model:                 model,
		APIKeyEnv:             apiKeyEnv,
		ModelSource:           ModelSourceDiscover,
		ChatAPIMode:           provider.ChatAPIModeChatCompletions,
		ChatEndpointPath:      "/chat/completions",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		Source:                ProviderSourceBuiltin,
	}
}

// OpenAIProvider returns the builtin OpenAI provider definition.
func OpenAIProvider() ProviderConfig {
	return newBuiltinOpenAICompatProvider(OpenAIName, OpenAIDefaultBaseURL, OpenAIDefaultModel, OpenAIDefaultAPIKeyEnv)
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

// QiniuProvider returns the builtin Qiniu provider definition.
func QiniuProvider() ProviderConfig {
	return newBuiltinOpenAICompatProvider(QiniuName, QiniuDefaultBaseURL, QiniuDefaultModel, QiniuDefaultAPIKeyEnv)
}

// ModelScopeProvider 返回内置的 ModelScope provider 配置。
func ModelScopeProvider() ProviderConfig {
	return newBuiltinOpenAICompatProvider(ModelScopeName, ModelScopeDefaultBaseURL, ModelScopeDefaultModel, ModelScopeDefaultAPIKeyEnv)
}

// DefaultProviders returns all builtin provider definitions.
func DefaultProviders() []ProviderConfig {
	return []ProviderConfig{
		OpenAIProvider(),
		GeminiProvider(),
		QiniuProvider(),
		ModelScopeProvider(),
	}
}
