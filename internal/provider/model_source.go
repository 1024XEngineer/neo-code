package provider

// NormalizeModelSource 规范化模型来源枚举，未知值返回空字符串供上层做校验与回退。
func NormalizeModelSource(value string) string {
	switch NormalizeKey(value) {
	case ModelSourceDiscover:
		return ModelSourceDiscover
	case ModelSourceManual:
		return ModelSourceManual
	default:
		return ""
	}
}
