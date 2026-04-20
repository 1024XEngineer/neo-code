package infra

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
)

// NewGlamourTermRenderer 创建指定宽度的 Glamour 终端渲染器。
func NewGlamourTermRenderer(style string, width int) (*glamour.TermRenderer, error) {
	if cfg, ok := resolveStyleWithoutHeadingHashes(style); ok {
		return glamour.NewTermRenderer(
			glamour.WithStyles(cfg),
			glamour.WithWordWrap(width),
		)
	}

	return glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
}

func resolveStyleWithoutHeadingHashes(style string) (ansi.StyleConfig, bool) {
	normalized := strings.ToLower(strings.TrimSpace(style))
	if normalized == "" {
		normalized = glamourstyles.DarkStyle
	}

	base, ok := glamourstyles.DefaultStyles[normalized]
	if !ok || base == nil {
		return ansi.StyleConfig{}, false
	}

	cfg := *base
	cfg.H1.StylePrimitive.Prefix = ""
	cfg.H1.StylePrimitive.Suffix = ""
	cfg.H2.StylePrimitive.Prefix = ""
	cfg.H2.StylePrimitive.Suffix = ""
	cfg.H3.StylePrimitive.Prefix = ""
	cfg.H3.StylePrimitive.Suffix = ""
	cfg.H4.StylePrimitive.Prefix = ""
	cfg.H4.StylePrimitive.Suffix = ""
	cfg.H5.StylePrimitive.Prefix = ""
	cfg.H5.StylePrimitive.Suffix = ""
	cfg.H6.StylePrimitive.Prefix = ""
	cfg.H6.StylePrimitive.Suffix = ""

	return cfg, true
}
