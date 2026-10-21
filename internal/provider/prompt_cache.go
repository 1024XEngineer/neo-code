package provider

import "strings"

// PromptCacheMode 控制 provider 是否在请求中携带 prompt 缓存标记。
const (
	// PromptCacheModeAuto 表示按各 adapter 的能力自动启用缓存断点。
	PromptCacheModeAuto = "auto"
	// PromptCacheModeOff 表示完全禁用缓存标记，用于规避缓存写入溢价或排障。
	PromptCacheModeOff = "off"
)

// NormalizePromptCacheMode 归一化缓存模式配置，空值回落为 auto，未知值同样回落为 auto。
// 该配置只影响是否发送缓存标记，不影响请求内容本身。
func NormalizePromptCacheMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case PromptCacheModeOff, "false", "disabled", "none":
		return PromptCacheModeOff
	default:
		return PromptCacheModeAuto
	}
}

// PromptCacheEnabled 判断给定模式下是否允许发送缓存标记。
func PromptCacheEnabled(mode string) bool {
	return NormalizePromptCacheMode(mode) == PromptCacheModeAuto
}

// SupportsPromptCacheKey 判断该 driver 是否应发送 OpenAI 的 prompt_cache_key 路由键。
// 五个内置厂商走的是自研 HTTP 实现，各自有独立的上下文缓存机制或对未知字段校验更严格，
// 因此不发送该字段：DeepSeek 按前缀全局命中，Qwen / GLM 走厂商自有接口。
// 其余 driver（含 openaicompat 通用驱动与空 driver 的默认场景）按 OpenAI 语义发送。
func SupportsPromptCacheKey(driver string) bool {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case DriverDeepSeek, DriverQwen, DriverGLM, DriverMiMo, DriverMiniMax:
		return false
	default:
		return true
	}
}
