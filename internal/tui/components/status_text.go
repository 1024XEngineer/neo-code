package components

import (
	"strings"
)

// CompactStatusText 压缩状态文本为单行展示，支持可选长度限制。
func CompactStatusText(text string, limit int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.Join(strings.Fields(line), " ")
		if limit > 0 {
			return trimMiddle(line, limit)
		}
		return line
	}
	return ""
}
