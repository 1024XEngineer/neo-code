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
	if !supportsChatAPIMode(normalizedDriver) && strings.TrimSpace(p.ChatAPIMode) != "" {
		return fmt.Errorf("provider %q chat_api_mode is only supported for openaicompat-compatible drivers", p.Name)
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
	if p.Source == ProviderSourceBuiltin && len(p.Models) > 0 && !containsModelDescriptorID(p.Models, p.Model) {
		return fmt.Errorf("provider %q model %q must exist in builtin models", p.Name, p.Model)
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

// containsModelDescriptorID 判断模型列表中是否包含指定 ID。
func containsModelDescriptorID(models []providertypes.ModelDescriptor, modelID string) bool {
	target := provider.NormalizeKey(modelID)
	if target == "" {
		return false
	}
	for _, model := range models {
		if provider.NormalizeKey(model.ID) == target {
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
	return isOpenAICompatLike(driver)
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

// supportsChatAPIMode 判断指定 driver 是否允许配置 chat_api_mode 字段。
func supportsChatAPIMode(driver string) bool {
	return isOpenAICompatLike(driver)
}

// isOpenAICompatLike 判断指定 driver 是否使用 OpenAI-compatible HTTP discovery。
func isOpenAICompatLike(driver string) bool {
	switch normalizeProviderDriver(driver) {
	case provider.DriverOpenAICompat, provider.DriverDeepSeek, provider.DriverQwen,
		provider.DriverGLM, provider.DriverMiMo, provider.DriverMiniMax:
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
	case provider.DriverDeepSeek:
		return DeepSeekDefaultBaseURL
	case provider.DriverQwen:
		return QwenDefaultBaseURL
	case provider.DriverGLM:
		return GLMDefaultBaseURL
	case provider.DriverMiMo:
		return MiMoDefaultBaseURL
	case provider.DriverMiniMax:
		return MiniMaxDefaultBaseURL
	default:
		return cfg.BaseURL
	}
}

const (
	OpenAIName             = "openai"
	OpenAIDefaultBaseURL   = "https://api.openai.com/v1"
	OpenAIDefaultModel     = "gpt-5.5"
	OpenAIDefaultAPIKeyEnv = "OPENAI_API_KEY"

	GeminiName             = "gemini"
	GeminiDefaultBaseURL   = "https://generativelanguage.googleapis.com/v1beta"
	GeminiDefaultModel     = "gemini-3.1-pro-preview"
	GeminiDefaultAPIKeyEnv = "GEMINI_API_KEY"

	AnthropicDefaultBaseURL = "https://api.anthropic.com/v1"

	QiniuName             = "qiniu"
	QiniuDefaultBaseURL   = "https://api.qnaigc.com/v1"
	QiniuDefaultModel     = "z-ai/glm-5.1"
	QiniuDefaultAPIKeyEnv = "QINIU_API_KEY"

	ModelScopeName             = "modelscope"
	ModelScopeDefaultBaseURL   = "https://api-inference.modelscope.cn/v1"
	ModelScopeDefaultModel     = "deepseek-ai/DeepSeek-V3.2"
	ModelScopeDefaultAPIKeyEnv = "MODELSCOPE_API_KEY"
)

var openAIStaticModels = []providertypes.ModelDescriptor{
	builtinModel(
		"gpt-5.5",
		"GPT-5.5",
		"OpenAI flagship GPT-5.5 model with 1M context, enhanced reasoning and agentic coding.",
		1000000,
		128000,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			[]string{"low", "medium", "high", "xhigh"},
			"high",
			false,
		),
	),
	builtinModel(
		"gpt-5.4",
		"GPT-5.4",
		"Flagship GPT-5 model for reasoning, coding, and multimodal agent workflows.",
		400000,
		128000,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateSupported),
	),
	builtinModel(
		"gpt-5.4-mini",
		"GPT-5.4 Mini",
		"Lower-latency GPT-5 variant for everyday coding, chat, and multimodal tasks.",
		400000,
		128000,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateSupported),
	),
	builtinModel(
		"gpt-5.3-codex",
		"GPT-5.3 Codex",
		"GPT-5 Codex family model tuned for code generation, editing, and agentic development loops.",
		400000,
		128000,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateSupported),
	),
	builtinModel(
		"gpt-4.1",
		"GPT-4.1",
		"High-capability GPT-4.1 model for complex coding and long-context multimodal work.",
		1047576,
		32768,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateSupported),
	),
	builtinModel(
		"gpt-4o",
		"GPT-4o",
		"General-purpose GPT-4o omni model for realtime, text, and image workflows.",
		128000,
		16384,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateSupported),
	),
	builtinModel(
		"gpt-4o-mini",
		"GPT-4o Mini",
		"Cost-efficient GPT-4o variant for fast multimodal and tool-using tasks.",
		128000,
		16384,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateSupported),
	),
}

var geminiStaticModels = []providertypes.ModelDescriptor{
	builtinModel(
		"gemini-3.1-pro-preview",
		"Gemini 3.1 Pro",
		"Latest Gemini 3.1 flagship with 2M context, advanced reasoning and agentic coding.",
		2097152,
		65536,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			[]string{"low", "high"},
			"high",
			true, // ThinkingForceEnabled: only downgrade to LOW, cannot disable
		),
	),
	builtinModel(
		"gemini-3.1-flash",
		"Gemini 3.1 Flash",
		"Fast Gemini 3.1 model for cost-efficient multimodal tasks.",
		1048576,
		65536,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			[]string{"low", "high"},
			"high",
			true, // ThinkingForceEnabled
		),
	),
	builtinModel(
		"gemini-2.5-flash",
		"Gemini 2.5 Flash",
		"Fast Gemini 2.5 model with long-context multimodal input and tool support.",
		1048576,
		65536,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateSupported),
	),
	builtinModel(
		"gemini-2.5-pro",
		"Gemini 2.5 Pro",
		"High-reasoning Gemini 2.5 model with long-context multimodal input and tool support.",
		1048576,
		65536,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateSupported),
	),
}

var qiniuStaticModels = []providertypes.ModelDescriptor{
	builtinModel(
		"z-ai/glm-5.1",
		"GLM 5.1",
		"GLM-5.1 model exposed by the Qiniu gateway for long-context reasoning and tool-using tasks.",
		200000,
		128000,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateUnsupported),
	),
}

var modelScopeStaticModels = []providertypes.ModelDescriptor{
	builtinModel(
		"deepseek-ai/DeepSeek-V3.2",
		"DeepSeek V3.2",
		"Reasoning-first DeepSeek model available from ModelScope API inference with tool use support.",
		128000,
		8192,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateUnsupported),
	),
	builtinModel(
		"MiniMax/MiniMax-M2.5",
		"MiniMax M2.5",
		"General-purpose MiniMax model available from ModelScope API inference for coding and agent workflows.",
		204800,
		0,
		builtinCapabilities(providertypes.ModelCapabilityStateSupported, providertypes.ModelCapabilityStateUnsupported),
	),
}

// builtinCapabilities 构造内建静态模型的能力提示，显式表达支持、未知或不支持状态。
func builtinCapabilities(
	toolCalling providertypes.ModelCapabilityState,
	imageInput providertypes.ModelCapabilityState,
) providertypes.ModelCapabilityHints {
	return providertypes.ModelCapabilityHints{
		ToolCalling: toolCalling,
		ImageInput:  imageInput,
	}
}

// builtinCapabilitiesV2 构造包含 thinking 能力的静态模型能力提示。
func builtinCapabilitiesV2(
	toolCalling, imageInput, thinking providertypes.ModelCapabilityState,
	thinkingEfforts []string,
	thinkingDefaultEffort string,
	thinkingForceEnabled bool,
) providertypes.ModelCapabilityHints {
	return providertypes.ModelCapabilityHints{
		ToolCalling:           toolCalling,
		ImageInput:            imageInput,
		Thinking:              thinking,
		ThinkingEfforts:       thinkingEfforts,
		ThinkingDefaultEffort: thinkingDefaultEffort,
		ThinkingForceEnabled:  thinkingForceEnabled,
	}
}

// builtinModel 构造内建 provider 使用的静态模型条目。
func builtinModel(
	id string,
	name string,
	description string,
	contextWindow int,
	maxOutputTokens int,
	capabilityHints providertypes.ModelCapabilityHints,
) providertypes.ModelDescriptor {
	return providertypes.ModelDescriptor{
		ID:              strings.TrimSpace(id),
		Name:            strings.TrimSpace(name),
		Description:     strings.TrimSpace(description),
		ContextWindow:   contextWindow,
		MaxOutputTokens: maxOutputTokens,
		CapabilityHints: capabilityHints,
	}
}

// cloneBuiltinModels 返回静态模型清单的独立副本，避免不同配置快照共享底层切片。
func cloneBuiltinModels(models []providertypes.ModelDescriptor) []providertypes.ModelDescriptor {
	return providertypes.CloneModelDescriptors(models)
}

func newBuiltinOpenAICompatProvider(name, baseURL, model, apiKeyEnv string) ProviderConfig {
	return ProviderConfig{
		Name:             name,
		Driver:           provider.DriverOpenAICompat,
		BaseURL:          baseURL,
		Model:            model,
		APIKeyEnv:        apiKeyEnv,
		ChatAPIMode:      provider.ChatAPIModeChatCompletions,
		ChatEndpointPath: "/chat/completions",
		ModelSource:      ModelSourceDiscover,
		Source:           ProviderSourceBuiltin,
	}
}

// OpenAIProvider returns the builtin OpenAI provider definition.
func OpenAIProvider() ProviderConfig {
	cfg := newBuiltinOpenAICompatProvider(OpenAIName, OpenAIDefaultBaseURL, OpenAIDefaultModel, OpenAIDefaultAPIKeyEnv)
	cfg.Models = cloneBuiltinModels(openAIStaticModels)
	return cfg
}

// GeminiProvider returns the builtin Gemini provider definition.
func GeminiProvider() ProviderConfig {
	return ProviderConfig{
		Name:             GeminiName,
		Driver:           provider.DriverGemini,
		BaseURL:          GeminiDefaultBaseURL,
		Model:            GeminiDefaultModel,
		APIKeyEnv:        GeminiDefaultAPIKeyEnv,
		ChatEndpointPath: "",
		Models:           cloneBuiltinModels(geminiStaticModels),
		ModelSource:      ModelSourceDiscover,
		Source:           ProviderSourceBuiltin,
	}
}

// QiniuProvider returns the builtin Qiniu provider definition.
func QiniuProvider() ProviderConfig {
	cfg := newBuiltinOpenAICompatProvider(QiniuName, QiniuDefaultBaseURL, QiniuDefaultModel, QiniuDefaultAPIKeyEnv)
	cfg.Models = cloneBuiltinModels(qiniuStaticModels)
	cfg.ModelSource = ModelSourceManual
	return cfg
}

// ModelScopeProvider 返回内置的 ModelScope provider 配置。
func ModelScopeProvider() ProviderConfig {
	cfg := newBuiltinOpenAICompatProvider(ModelScopeName, ModelScopeDefaultBaseURL, ModelScopeDefaultModel, ModelScopeDefaultAPIKeyEnv)
	cfg.Models = cloneBuiltinModels(modelScopeStaticModels)
	cfg.ModelSource = ModelSourceManual
	return cfg
}

const (
	// New provider constants
	DeepSeekName             = "deepseek"
	DeepSeekDefaultBaseURL   = "https://api.deepseek.com"
	DeepSeekDefaultModel     = "deepseek-v4-pro"
	DeepSeekDefaultAPIKeyEnv = "DEEPSEEK_API_KEY"

	KimiName             = "kimi"
	KimiDefaultBaseURL   = "https://api.moonshot.cn/v1"
	KimiDefaultModel     = "kimi-k2.6"
	KimiDefaultAPIKeyEnv = "KIMI_API_KEY"

	QwenName             = "qwen"
	QwenDefaultBaseURL   = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	QwenDefaultModel     = "qwen3.6-plus"
	QwenDefaultAPIKeyEnv = "QWEN_API_KEY"

	GLMName             = "glm"
	GLMDefaultBaseURL   = "https://api.z.ai/api/paas/v4"
	GLMDefaultModel     = "glm-5.1"
	GLMDefaultAPIKeyEnv = "GLM_API_KEY"

	MiMoName             = "mimo"
	MiMoDefaultBaseURL   = "https://api.xiaomimimo.com/v1"
	MiMoDefaultModel     = "mimo-v2.5-pro"
	MiMoDefaultAPIKeyEnv = "MIMO_API_KEY"

	MiniMaxName             = "minimax"
	MiniMaxDefaultBaseURL   = "https://api.minimax.chat/v1/text/chatcompletion_v2"
	MiniMaxDefaultModel     = "MiniMax-M2.7"
	MiniMaxDefaultAPIKeyEnv = "MINIMAX_API_KEY"
)

// New static model lists.
var deepSeekStaticModels = []providertypes.ModelDescriptor{
	builtinModel(
		"deepseek-v4-pro",
		"DeepSeek V4 Pro",
		"Flagship DeepSeek MoE model with 1M context and thinking/non-thinking dual modes.",
		1048576,
		384000,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateUnsupported,
			providertypes.ModelCapabilityStateSupported,
			[]string{"high", "max"},
			"high",
			false,
		),
	),
	builtinModel(
		"deepseek-v4-flash",
		"DeepSeek V4 Flash",
		"Fast, economical DeepSeek MoE model with 1M context and dual modes.",
		1048576,
		384000,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateUnsupported,
			providertypes.ModelCapabilityStateSupported,
			[]string{"high", "max"},
			"high",
			false,
		),
	),
}

var kimiStaticModels = []providertypes.ModelDescriptor{
	builtinModel(
		"kimi-k2.6",
		"Kimi K2.6",
		"Moonshot flagship 1T MoE model with 256K context and agent swarm support.",
		262144,
		4096,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateUnsupported,
			providertypes.ModelCapabilityStateSupported,
			nil,
			"",
			false,
		),
	),
	builtinModel(
		"kimi-k2.5",
		"Kimi K2.5",
		"Previous generation Moonshot model, flexible thinking default-on.",
		131072,
		4096,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateUnsupported,
			providertypes.ModelCapabilityStateSupported,
			nil,
			"",
			false,
		),
	),
}

var qwenStaticModels = []providertypes.ModelDescriptor{
	builtinModel(
		"qwen3.6-plus",
		"Qwen 3.6 Plus",
		"Alibaba flagship agentic coding model with 1M context and hybrid thinking.",
		1048576,
		128000,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			nil,
			"",
			false,
		),
	),
	builtinModel(
		"qwen3.6-flash",
		"Qwen 3.6 Flash",
		"Fast Qwen MoE model (35B total / 3B active) for cost-efficient agent tasks.",
		1048576,
		32768,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			nil,
			"",
			false,
		),
	),
	builtinModel(
		"qwen3.6-max-preview",
		"Qwen 3.6 Max Preview",
		"Latest Qwen flagship preview with preserve_thinking for sustained chain-of-thought.",
		1048576,
		128000,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			nil,
			"",
			false,
		),
	),
}

var glmStaticModels = []providertypes.ModelDescriptor{
	builtinModel(
		"glm-5.1",
		"GLM-5.1",
		"Zhipu flagship model, first Chinese model matching Claude Opus 4.6 across the board.",
		200000,
		128000,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateUnsupported,
			providertypes.ModelCapabilityStateSupported,
			nil,
			"",
			false,
		),
	),
	builtinModel(
		"glm-5",
		"GLM-5",
		"Zhipu 744B MoE model with DeepSeek Sparse Attention, MIT licensed.",
		200000,
		128000,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateUnsupported,
			providertypes.ModelCapabilityStateSupported,
			nil,
			"",
			false,
		),
	),
}

var miMoStaticModels = []providertypes.ModelDescriptor{
	builtinModel(
		"mimo-v2.5-pro",
		"MiMo V2.5 Pro",
		"Xiaomi flagship 1.02T MoE model with 1M context, #1 open-source on Artificial Analysis.",
		1048576,
		128000,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			nil,
			"",
			false,
		),
	),
	builtinModel(
		"mimo-v2-flash",
		"MiMo V2 Flash",
		"Xiaomi fast MoE model (309B/15B active), #1 open-source coding at 150 tok/s.",
		262144,
		32768,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateSupported,
			nil,
			"",
			false,
		),
	),
}

var miniMaxStaticModels = []providertypes.ModelDescriptor{
	builtinModel(
		"MiniMax-M2.7",
		"MiniMax M2.7",
		"Latest MiniMax interleaved-thinking model with cost-aggressive pricing.",
		205000,
		8192,
		builtinCapabilitiesV2(
			providertypes.ModelCapabilityStateSupported,
			providertypes.ModelCapabilityStateUnsupported,
			providertypes.ModelCapabilityStateSupported,
			nil,
			"",
			true, // ThinkingForceEnabled: enable_thinking=false is unreliable
		),
	),
}

// New provider factory functions.
func DeepSeekProvider() ProviderConfig {
	return ProviderConfig{
		Name:             DeepSeekName,
		Driver:           provider.DriverDeepSeek,
		BaseURL:          DeepSeekDefaultBaseURL,
		Model:            DeepSeekDefaultModel,
		APIKeyEnv:        DeepSeekDefaultAPIKeyEnv,
		ChatAPIMode:      provider.ChatAPIModeChatCompletions,
		ChatEndpointPath: "/chat/completions",
		ModelSource:      ModelSourceManual,
		Models:           cloneBuiltinModels(deepSeekStaticModels),
		Source:           ProviderSourceBuiltin,
	}
}

func KimiProvider() ProviderConfig {
	return ProviderConfig{
		Name:                  KimiName,
		Driver:                provider.DriverOpenAICompat,
		BaseURL:               KimiDefaultBaseURL,
		Model:                 KimiDefaultModel,
		APIKeyEnv:             KimiDefaultAPIKeyEnv,
		ChatAPIMode:           provider.ChatAPIModeChatCompletions,
		ChatEndpointPath:      "/chat/completions",
		DiscoveryEndpointPath: provider.DiscoveryEndpointPathModels,
		ModelSource:           ModelSourceManual,
		Models:                cloneBuiltinModels(kimiStaticModels),
		Source:                ProviderSourceBuiltin,
	}
}

func QwenProvider() ProviderConfig {
	return ProviderConfig{
		Name:             QwenName,
		Driver:           provider.DriverQwen,
		BaseURL:          QwenDefaultBaseURL,
		Model:            QwenDefaultModel,
		APIKeyEnv:        QwenDefaultAPIKeyEnv,
		ChatAPIMode:      provider.ChatAPIModeChatCompletions,
		ChatEndpointPath: "/chat/completions",
		ModelSource:      ModelSourceManual,
		Models:           cloneBuiltinModels(qwenStaticModels),
		Source:           ProviderSourceBuiltin,
	}
}

func GLMProvider() ProviderConfig {
	return ProviderConfig{
		Name:             GLMName,
		Driver:           provider.DriverGLM,
		BaseURL:          GLMDefaultBaseURL,
		Model:            GLMDefaultModel,
		APIKeyEnv:        GLMDefaultAPIKeyEnv,
		ChatAPIMode:      provider.ChatAPIModeChatCompletions,
		ChatEndpointPath: "/chat/completions",
		ModelSource:      ModelSourceManual,
		Models:           cloneBuiltinModels(glmStaticModels),
		Source:           ProviderSourceBuiltin,
	}
}

func MiMoProvider() ProviderConfig {
	return ProviderConfig{
		Name:             MiMoName,
		Driver:           provider.DriverMiMo,
		BaseURL:          MiMoDefaultBaseURL,
		Model:            MiMoDefaultModel,
		APIKeyEnv:        MiMoDefaultAPIKeyEnv,
		ChatAPIMode:      provider.ChatAPIModeChatCompletions,
		ChatEndpointPath: "/chat/completions",
		ModelSource:      ModelSourceManual,
		Models:           cloneBuiltinModels(miMoStaticModels),
		Source:           ProviderSourceBuiltin,
	}
}

func MiniMaxProvider() ProviderConfig {
	return ProviderConfig{
		Name:             MiniMaxName,
		Driver:           provider.DriverMiniMax,
		BaseURL:          MiniMaxDefaultBaseURL,
		Model:            MiniMaxDefaultModel,
		APIKeyEnv:        MiniMaxDefaultAPIKeyEnv,
		ChatAPIMode:      provider.ChatAPIModeChatCompletions,
		ChatEndpointPath: "/",
		ModelSource:      ModelSourceManual,
		Models:           cloneBuiltinModels(miniMaxStaticModels),
		Source:           ProviderSourceBuiltin,
	}
}

// DefaultProviders returns all builtin provider definitions.
func DefaultProviders() []ProviderConfig {
	return []ProviderConfig{
		OpenAIProvider(),
		GeminiProvider(),
		DeepSeekProvider(),
		KimiProvider(),
		QwenProvider(),
		GLMProvider(),
		MiMoProvider(),
		MiniMaxProvider(),
		QiniuProvider(),
		ModelScopeProvider(),
	}
}
