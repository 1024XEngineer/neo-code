package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// NormalizeBlockRightEdge 对多行块内容进行统一宽度补齐，避免右边界抖动。
func NormalizeBlockRightEdge(content string, maxWidth int) string {
	if strings.TrimSpace(content) == "" {
		return content
	}

	lines := strings.Split(content, "\n")
	targetWidth := 0
	for _, line := range lines {
		targetWidth = max(targetWidth, lipgloss.Width(line))
	}
	targetWidth = clamp(targetWidth, 1, maxWidth)

	padStyle := lipgloss.NewStyle().Width(targetWidth)
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		normalized = append(normalized, padStyle.Render(line))
	}
	return strings.Join(normalized, "\n")
}

// TrimRenderedTrailingWhitespace 清理渲染后每行末尾的空白字符。
func TrimRenderedTrailingWhitespace(content string) string {
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Join(lines, "\n")
}

// clampInt 将数值限制在给定区间内。
