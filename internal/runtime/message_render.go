package runtime

import (
	"strings"

	"neo-code/internal/partsrender"
	providertypes "neo-code/internal/provider/types"
)

// renderPartsForVerification 将多模态消息压平成验收与完成信号解析使用的稳定文本。
func renderPartsForVerification(parts []providertypes.ContentPart) string {
	return strings.TrimSpace(partsrender.RenderDisplayParts(parts))
}
