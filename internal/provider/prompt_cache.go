package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	providertypes "neo-code/internal/provider/types"
)

// PromptCacheMode 控制 provider 是否在请求中携带 prompt 缓存标记。
const (
	// PromptCacheModeAuto 表示按各 adapter 的能力启用缓存标记。
	PromptCacheModeAuto = "auto"
	// PromptCacheModeOff 表示完全禁用缓存标记，用于规避缓存写入溢价或排障。
	PromptCacheModeOff = "off"
)

// NormalizePromptCacheMode 归一化缓存模式配置，空值回落为 auto，未知值同样回落为 auto。
// 仅用于不区分 driver 的场景；需要按 driver 取默认值时使用 ResolvePromptCacheMode。
func NormalizePromptCacheMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case PromptCacheModeOff, "false", "disabled", "none":
		return PromptCacheModeOff
	default:
		return PromptCacheModeAuto
	}
}

// DefaultPromptCacheMode 返回指定 driver 在未显式配置时的缓存模式。
// 仅 anthropic 默认开启：cache_control 是官方协议字段，不会被端点拒绝。
// openaicompat 服务任意兼容网关，prompt_cache_key 属于可选扩展参数，在未确认端点
// 支持前默认发送会让原本可用的网关返回 400，因此默认关闭，由用户在 provider 配置中
// 显式写 prompt_cache: auto 声明该端点支持。
func DefaultPromptCacheMode(driver string) string {
	if strings.EqualFold(strings.TrimSpace(driver), DriverAnthropic) {
		return PromptCacheModeAuto
	}
	return PromptCacheModeOff
}

// ResolvePromptCacheMode 结合显式配置与 driver 默认值，得出最终缓存模式。
// 配置为空或无法识别时使用 driver 默认值，避免把拼写错误静默解释成"开启"。
func ResolvePromptCacheMode(configuredMode string, driver string) string {
	switch strings.ToLower(strings.TrimSpace(configuredMode)) {
	case PromptCacheModeAuto:
		return PromptCacheModeAuto
	case PromptCacheModeOff, "false", "disabled", "none":
		return PromptCacheModeOff
	default:
		return DefaultPromptCacheMode(driver)
	}
}

// PromptCacheEnabled 判断给定配置与 driver 下是否允许发送缓存标记。
func PromptCacheEnabled(configuredMode string, driver string) bool {
	return ResolvePromptCacheMode(configuredMode, driver) == PromptCacheModeAuto
}

// SupportsPromptCacheKey 判断该 driver 是否允许发送 OpenAI 的 prompt_cache_key 路由键。
// 五个内置厂商走的是自研 HTTP 实现，各自有独立的上下文缓存机制或对未知字段校验更严格，
// 因此永不发送该字段：DeepSeek 按前缀全局命中，Qwen / GLM 走厂商自有接口。
// 该判断是硬约束，优先于显式 prompt_cache: auto 配置。
func SupportsPromptCacheKey(driver string) bool {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case DriverDeepSeek, DriverQwen, DriverGLM, DriverMiMo, DriverMiniMax:
		return false
	default:
		return true
	}
}

// ShouldSendPromptCacheKey 综合配置、driver 默认值与硬约束，决定是否发送缓存路由键。
func ShouldSendPromptCacheKey(configuredMode string, driver string) bool {
	if !SupportsPromptCacheKey(driver) {
		return false
	}
	return PromptCacheEnabled(configuredMode, driver)
}

// PromptCacheKeyHexLength 是缓存路由键的十六进制长度，远低于各厂商 64 字符的上限。
const PromptCacheKeyHexLength = 32

// promptCacheKeyHexLength 是内部别名，保持 key 生成处可读。
const promptCacheKeyHexLength = PromptCacheKeyHexLength

// ComputePromptCacheKey 基于模型、稳定前缀与工具名集合生成缓存路由键。
// 三者任一变化都应产生新键：不同前缀共享同一键会把请求错误地路由到一起。
// 无稳定前缀时返回空串，调用方据此不发送该字段。
func ComputePromptCacheKey(model string, stablePrefix string, toolNames []string) string {
	if strings.TrimSpace(stablePrefix) == "" {
		return ""
	}

	digest := sha256.New()
	digest.Write([]byte(strings.TrimSpace(model)))
	digest.Write([]byte{0})
	digest.Write([]byte(stablePrefix))
	for _, name := range toolNames {
		digest.Write([]byte{0})
		digest.Write([]byte(strings.TrimSpace(name)))
	}
	return hex.EncodeToString(digest.Sum(nil))[:promptCacheKeyHexLength]
}

// PromptCacheToolNames 提取工具名列表，供 ComputePromptCacheKey 参与键计算。
func PromptCacheToolNames(specs []providertypes.ToolSpec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}
