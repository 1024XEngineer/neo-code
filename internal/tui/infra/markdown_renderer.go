package infra

import "github.com/charmbracelet/glamour"

// NewGlamourTermRenderer 创建指定宽度的 Glamour 终端渲染器。
func NewGlamourTermRenderer(style string, width int) (*glamour.TermRenderer, error) {
	return glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
}
