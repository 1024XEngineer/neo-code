package context

import (
	"strings"

	"neo-code/internal/partsrender"
	providertypes "neo-code/internal/provider/types"
)

// renderCompactPromptParts 将消息 Parts 渲染为 compact prompt 可消费的文本表示。
func renderCompactPromptParts(parts []providertypes.ContentPart) string {
	return partsrender.RenderCompactPromptParts(parts)
}

// renderTranscriptParts 将消息 Parts 渲染为 transcript 可审计文本，避免泄露原始二进制。
func renderTranscriptParts(parts []providertypes.ContentPart) string {
	return partsrender.RenderTranscriptParts(parts)
}

// renderDisplayParts 将消息 Parts 渲染为通用展示文本，供 display/memo 判定与展示使用。
func renderDisplayParts(parts []providertypes.ContentPart) string {
	return partsrender.RenderDisplayParts(parts)
}

// hasRenderableParts 判断消息是否包含可见语义（非空文本或图片）。
func hasRenderableParts(parts []providertypes.ContentPart) bool {
	for _, part := range parts {
		switch part.Kind {
		case providertypes.ContentPartText:
			if strings.TrimSpace(part.Text) != "" {
				return true
			}
		case providertypes.ContentPartImage:
			if part.Image != nil {
				return true
			}
		}
	}
	return false
}
