package tui

import (
	"neo-code/internal/partsrender"
	providertypes "neo-code/internal/provider/types"
)

// renderMessagePartsForDisplay 将消息分片渲染为 TUI 展示文本，图片只显示安全占位。
func renderMessagePartsForDisplay(parts []providertypes.ContentPart) string {
	return partsrender.RenderDisplayParts(parts)
}
