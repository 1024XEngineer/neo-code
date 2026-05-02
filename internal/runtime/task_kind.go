package runtime

import (
	"strings"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/runtime/decider"
)

// inferTaskKindFromInput 基于用户输入文本推断任务类型，避免将简单状态任务误判为通用写入验证任务。
func inferTaskKindFromInput(parts []providertypes.ContentPart) decider.TaskKind {
	var builder strings.Builder
	for _, part := range parts {
		if part.Kind != providertypes.ContentPartText {
			continue
		}
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(text)
	}
	return decider.InferTaskKind(builder.String())
}
