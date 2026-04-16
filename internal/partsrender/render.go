package partsrender

import (
	"strings"

	providertypes "neo-code/internal/provider/types"
)

type imageRenderMode string

const (
	imageRenderModeCompactPrompt imageRenderMode = "compact_prompt"
	imageRenderModeTranscript    imageRenderMode = "transcript"
	imageRenderModeDisplay       imageRenderMode = "display"
)

// RenderCompactPromptParts 将消息 Parts 渲染为 compact prompt 可消费的文本表示。
func RenderCompactPromptParts(parts []providertypes.ContentPart) string {
	return renderParts(parts, imageRenderModeCompactPrompt)
}

// RenderTranscriptParts 将消息 Parts 渲染为 transcript 可审计文本，避免泄露原始二进制。
func RenderTranscriptParts(parts []providertypes.ContentPart) string {
	return renderParts(parts, imageRenderModeTranscript)
}

// RenderDisplayParts 将消息 Parts 渲染为通用展示文本，图片只保留安全占位。
func RenderDisplayParts(parts []providertypes.ContentPart) string {
	return renderParts(parts, imageRenderModeDisplay)
}

// renderParts 按场景将多模态 parts 收敛为可读文本；文本原样保留，图片转为占位符。
func renderParts(parts []providertypes.ContentPart, mode imageRenderMode) string {
	var builder strings.Builder
	for _, part := range parts {
		switch part.Kind {
		case providertypes.ContentPartText:
			builder.WriteString(part.Text)
		case providertypes.ContentPartImage:
			builder.WriteString(renderImagePlaceholder(part.Image, mode))
		}
	}
	return builder.String()
}

// renderImagePlaceholder 按不同读取场景输出图片占位文本，避免泄露原始图片数据。
func renderImagePlaceholder(image *providertypes.ImagePart, mode imageRenderMode) string {
	if image == nil {
		return "[Image]"
	}

	switch image.SourceType {
	case providertypes.ImageSourceRemote:
		if mode == imageRenderModeDisplay {
			return "[Image]"
		}
		if mode == imageRenderModeTranscript {
			return "[Image:remote]"
		}
		url := strings.TrimSpace(image.URL)
		if url == "" {
			return "[Image]"
		}
		return "[Image:remote] " + url
	case providertypes.ImageSourceSessionAsset:
		if mode == imageRenderModeDisplay {
			return "[Image]"
		}
		if mode == imageRenderModeTranscript {
			return "[Image:session_asset]"
		}
		if image.Asset == nil {
			return "[Image]"
		}
		assetID := strings.TrimSpace(image.Asset.ID)
		mime := strings.TrimSpace(image.Asset.MimeType)
		if assetID == "" {
			return "[Image]"
		}
		if mime == "" {
			return "[Image:session_asset] " + assetID
		}
		return "[Image:session_asset] " + assetID + " (" + mime + ")"
	default:
		return "[Image]"
	}
}
