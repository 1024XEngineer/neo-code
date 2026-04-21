package provider

import "strings"

const (
	// MaxSessionAssetsTotalBytes 定义单次请求允许携带的 session_asset 原始总字节上限（20 MiB）。
	MaxSessionAssetsTotalBytes int64 = 20 * 1024 * 1024
	// dataURLBasePrefixBytes 表示 data URL 固定前缀 "data:;base64," 的字节长度。
	dataURLBasePrefixBytes int64 = 13
	// dataURLJSONOverheadBytes 估算 data URL 被 JSON 字符串包装时的附加开销。
	dataURLJSONOverheadBytes int64 = 2
)

// RequestAssetBudget 描述单次模型请求可携带的附件总预算限制。
type RequestAssetBudget struct {
	MaxSessionAssetsTotalBytes int64
}

// DefaultRequestAssetBudget 返回请求附件预算的默认值。
func DefaultRequestAssetBudget() RequestAssetBudget {
	return RequestAssetBudget{
		MaxSessionAssetsTotalBytes: MaxSessionAssetsTotalBytes,
	}
}

// NormalizeRequestAssetBudget 归一化请求附件预算，确保不越过硬上限且不低于单个附件上限。
func NormalizeRequestAssetBudget(budget RequestAssetBudget, maxSessionAssetBytes int64) RequestAssetBudget {
	normalized := budget
	if normalized.MaxSessionAssetsTotalBytes <= 0 {
		normalized.MaxSessionAssetsTotalBytes = MaxSessionAssetsTotalBytes
	}
	if normalized.MaxSessionAssetsTotalBytes > MaxSessionAssetsTotalBytes {
		normalized.MaxSessionAssetsTotalBytes = MaxSessionAssetsTotalBytes
	}
	if normalized.MaxSessionAssetsTotalBytes < maxSessionAssetBytes {
		normalized.MaxSessionAssetsTotalBytes = maxSessionAssetBytes
	}
	return normalized
}

// EstimateDataURLTransportBytes 估算图片以 data URL 进入 JSON 请求体时的传输字节数。
func EstimateDataURLTransportBytes(rawBytes int64, mimeType string) int64 {
	if rawBytes <= 0 {
		return 0
	}
	encodedBytes := ((rawBytes + 2) / 3) * 4
	return encodedBytes + dataURLBasePrefixBytes + int64(len(strings.TrimSpace(mimeType))) + dataURLJSONOverheadBytes
}
