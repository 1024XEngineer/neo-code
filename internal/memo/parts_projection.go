package memo

import (
	"strings"

	"neo-code/internal/partsrender"
	providertypes "neo-code/internal/provider/types"
)

// renderMemoParts 将消息 Parts 投影为 memo 可消费文本；图片转为占位符。
func renderMemoParts(parts []providertypes.ContentPart) string {
	return partsrender.RenderDisplayParts(parts)
}

// hasMemoRelevantUserInput 判断用户消息是否包含可用于 memo 分析的输入。
func hasMemoRelevantUserInput(parts []providertypes.ContentPart) bool {
	for _, part := range parts {
		if part.Kind == providertypes.ContentPartText && strings.TrimSpace(part.Text) != "" {
			return true
		}
	}
	return false
}
