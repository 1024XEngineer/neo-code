package components

import (
	"strings"

	tuistate "neo-code/internal/tui/state"
)

// ActivityPreviewHeight 根据活动条目数量计算预览区高度。
func ActivityPreviewHeight(count int) int {
	if count == 0 {
		return 0
	}
	return 6
}

// RenderActivityLine 将活动条目渲染为单行文本，错误条目会优先标记为 ERROR。
func RenderActivityLine(entry tuistate.ActivityEntry, width int) string {
	timeLabel := entry.Time.Format("15:04:05")
	kind := strings.TrimSpace(entry.Kind)
	if entry.IsError {
		kind = "error"
	}
	kindLabel := strings.ToUpper(fallback(kind, "event"))

	text := entry.Title
	if strings.TrimSpace(entry.Detail) != "" {
		text = text + ": " + entry.Detail
	}

	return trimMiddle(
		timeLabel+" "+kindLabel+" "+strings.Join(strings.Fields(text), " "),
		max(12, width),
	)
}
