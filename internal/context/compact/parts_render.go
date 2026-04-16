package compact

import (
	"neo-code/internal/partsrender"
	providertypes "neo-code/internal/provider/types"
)

// renderTranscriptParts 将消息 Parts 渲染为 transcript 可读文本，避免泄露二进制内容。
func renderTranscriptParts(parts []providertypes.ContentPart) string {
	return partsrender.RenderTranscriptParts(parts)
}
