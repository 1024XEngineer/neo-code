package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"

	providertypes "neo-code/internal/provider/types"
)

const (
	EstimateSourceNative = "native"
	EstimateSourceLocal  = "local"
	EstimateGateAdvisory = "advisory"
	EstimateGateGateable = "gateable"
	localEstimateSlack   = 1.15
)

// EstimateSerializedPayloadTokens 基于最终协议载荷的序列化结果估算输入 token 数。
func EstimateSerializedPayloadTokens(payload any) (int, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	return EstimateTextTokens(string(encoded)), nil
}

// EstimateTextTokens 对文本做保守放大的本地 token 估算，供 provider 预算预检复用。
func EstimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	return int(math.Ceil(float64(len([]byte(text))) / 4.0 * localEstimateSlack))
}

// BuildGenerateRequestSignature 生成 GenerateRequest 的稳定签名，用于估算与发送阶段的请求复用匹配。
func BuildGenerateRequestSignature(req providertypes.GenerateRequest) string {
	encoded, err := json.Marshal(req)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
