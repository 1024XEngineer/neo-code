package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"

	providertypes "neo-code/internal/provider/types"
)

const (
	EstimateSourceNative = "native"
	EstimateSourceLocal  = "local"
	EstimateGateAdvisory = "advisory"
	EstimateGateGateable = "gateable"
	localEstimateSlack   = 1.15
	// DefaultImageInputTokenEstimate 是无法读取图片尺寸时单张图片的保守预算估算值。
	DefaultImageInputTokenEstimate = 2048
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
	return EstimateByteCountTokens(len([]byte(text)))
}

// EstimateByteCountTokens 按字节数做保守放大的本地 token 估算。
// 供不必构造完整文本、只需按规模做阈值判断的场景复用。
func EstimateByteCountTokens(byteCount int) int {
	if byteCount <= 0 {
		return 0
	}
	return int(math.Ceil(float64(byteCount) / 4.0 * localEstimateSlack))
}

// RequestContainsImagePart 判断请求中是否包含图片分片，供 provider 选择多模态投影估算路径。
func RequestContainsImagePart(req providertypes.GenerateRequest) bool {
	for _, message := range req.Messages {
		for _, part := range message.Parts {
			if part.Kind == providertypes.ContentPartImage {
				return true
			}
		}
	}
	return false
}

// ResolveRequestModel 按请求模型优先、配置默认模型兜底的规则解析实际模型名。
func ResolveRequestModel(req providertypes.GenerateRequest, defaultModel string) string {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(defaultModel)
	}
	return model
}

// EstimateProjectedInputTokens 只估算语义输入，不把图片的 base64 传输体计入 prompt token。
func EstimateProjectedInputTokens(req providertypes.GenerateRequest, model string) (int, error) {
	if strings.TrimSpace(model) == "" {
		return 0, errors.New("model is empty")
	}

	var textBuilder strings.Builder
	textBuilder.WriteString(model)
	textBuilder.WriteByte('\n')
	textBuilder.WriteString(req.SystemPrompt)
	textBuilder.WriteByte('\n')

	imageCount := 0
	for _, message := range req.Messages {
		if err := providertypes.ValidateParts(message.Parts); err != nil {
			return 0, err
		}
		textBuilder.WriteString(message.Role)
		textBuilder.WriteByte('\n')
		textBuilder.WriteString(message.ToolCallID)
		textBuilder.WriteByte('\n')
		for _, part := range message.Parts {
			switch part.Kind {
			case providertypes.ContentPartText:
				textBuilder.WriteString(part.Text)
				textBuilder.WriteByte('\n')
			case providertypes.ContentPartImage:
				imageCount++
				if part.Image != nil {
					textBuilder.WriteString(string(part.Image.SourceType))
					textBuilder.WriteByte('\n')
					textBuilder.WriteString(part.Image.URL)
					textBuilder.WriteByte('\n')
					if part.Image.Asset != nil {
						textBuilder.WriteString(part.Image.Asset.ID)
						textBuilder.WriteByte('\n')
						textBuilder.WriteString(part.Image.Asset.MimeType)
						textBuilder.WriteByte('\n')
					}
				}
			}
		}
		for _, call := range message.ToolCalls {
			textBuilder.WriteString(call.ID)
			textBuilder.WriteByte('\n')
			textBuilder.WriteString(call.Name)
			textBuilder.WriteByte('\n')
			textBuilder.WriteString(call.Arguments)
			textBuilder.WriteByte('\n')
		}
		for key, value := range message.ToolMetadata {
			textBuilder.WriteString(key)
			textBuilder.WriteByte('=')
			textBuilder.WriteString(value)
			textBuilder.WriteByte('\n')
		}
	}

	for _, spec := range req.Tools {
		textBuilder.WriteString(spec.Name)
		textBuilder.WriteByte('\n')
		textBuilder.WriteString(spec.Description)
		textBuilder.WriteByte('\n')
		normalized := NormalizeToolSchemaObject(spec.Schema)
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return 0, err
		}
		textBuilder.Write(encoded)
		textBuilder.WriteByte('\n')
	}
	if req.ThinkingConfig != nil {
		textBuilder.WriteString(req.ThinkingConfig.Effort)
		textBuilder.WriteByte('\n')
		if req.ThinkingConfig.Enabled {
			textBuilder.WriteString("thinking_enabled")
		}
	}

	return EstimateTextTokens(textBuilder.String()) + imageCount*DefaultImageInputTokenEstimate, nil
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
