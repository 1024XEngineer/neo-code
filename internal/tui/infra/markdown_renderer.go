package infra

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
)

const markdownNeutralCodeBackground = "#3f3f46"

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
	stripNonCodeHighlightBackgrounds(&cfg)

	return cfg, true
}

func stripNonCodeHighlightBackgrounds(cfg *ansi.StyleConfig) {
	if cfg == nil {
		return
	}

	clearBlockHighlights(&cfg.Document)
	clearBlockHighlights(&cfg.BlockQuote)
	clearBlockHighlights(&cfg.Paragraph)
	clearBlockHighlights(&cfg.List.StyleBlock)
	clearBlockHighlights(&cfg.Heading)
	clearBlockHighlights(&cfg.H1)
	clearBlockHighlights(&cfg.H2)
	clearBlockHighlights(&cfg.H3)
	clearBlockHighlights(&cfg.H4)
	clearBlockHighlights(&cfg.H5)
	clearBlockHighlights(&cfg.H6)
	clearPrimitiveHighlights(&cfg.Text)
	clearPrimitiveHighlights(&cfg.Strikethrough)
	clearPrimitiveHighlights(&cfg.Emph)
	clearPrimitiveHighlights(&cfg.Strong)
	clearPrimitiveHighlights(&cfg.HorizontalRule)
	clearPrimitiveHighlights(&cfg.Item)
	clearPrimitiveHighlights(&cfg.Enumeration)
	clearPrimitiveHighlights(&cfg.Task.StylePrimitive)
	clearPrimitiveHighlights(&cfg.Link)
	clearPrimitiveHighlights(&cfg.LinkText)
	clearPrimitiveHighlights(&cfg.Image)
	clearPrimitiveHighlights(&cfg.ImageText)
	clearBlockHighlights(&cfg.DefinitionList)
	clearPrimitiveHighlights(&cfg.DefinitionTerm)
	clearPrimitiveHighlights(&cfg.DefinitionDescription)
	clearBlockHighlights(&cfg.HTMLBlock)
	clearBlockHighlights(&cfg.HTMLSpan)
	clearBlockHighlights(&cfg.Table.StyleBlock)

	applyNeutralCodeBackground(&cfg.Code.StylePrimitive)
	applyNeutralCodeBackground(&cfg.CodeBlock.StylePrimitive)

	// Keep neutral code backgrounds for readability, but drop token-level color blocks.
	if cfg.CodeBlock.Chroma != nil {
		clearChromaTokenHighlights(cfg.CodeBlock.Chroma)
		applyNeutralCodeBackground(&cfg.CodeBlock.Chroma.Background)
	}
}

func clearBlockHighlights(block *ansi.StyleBlock) {
	if block == nil {
		return
	}
	clearPrimitiveHighlights(&block.StylePrimitive)
}

func clearPrimitiveHighlights(primitive *ansi.StylePrimitive) {
	if primitive == nil {
		return
	}
	primitive.BackgroundColor = nil
	primitive.Inverse = nil
	clearPrimitiveDecorations(primitive)
}

func clearPrimitiveDecorations(primitive *ansi.StylePrimitive) {
	if primitive == nil {
		return
	}
	primitive.Color = nil
	primitive.Underline = nil
	primitive.Bold = nil
	primitive.Upper = nil
	primitive.Lower = nil
	primitive.Title = nil
	primitive.Italic = nil
	primitive.CrossedOut = nil
	primitive.Faint = nil
	primitive.Conceal = nil
	primitive.Overlined = nil
	primitive.Blink = nil
	primitive.Format = ""
}

func applyNeutralCodeBackground(primitive *ansi.StylePrimitive) {
	if primitive == nil {
		return
	}
	gray := markdownNeutralCodeBackground
	primitive.BackgroundColor = &gray
	primitive.Inverse = nil
}

func clearChromaTokenHighlights(chroma *ansi.Chroma) {
	if chroma == nil {
		return
	}

	clearPrimitiveHighlights(&chroma.Text)
	clearPrimitiveHighlights(&chroma.Error)
	clearPrimitiveHighlights(&chroma.Comment)
	clearPrimitiveHighlights(&chroma.CommentPreproc)
	clearPrimitiveHighlights(&chroma.Keyword)
	clearPrimitiveHighlights(&chroma.KeywordReserved)
	clearPrimitiveHighlights(&chroma.KeywordNamespace)
	clearPrimitiveHighlights(&chroma.KeywordType)
	clearPrimitiveHighlights(&chroma.Operator)
	clearPrimitiveHighlights(&chroma.Punctuation)
	clearPrimitiveHighlights(&chroma.Name)
	clearPrimitiveHighlights(&chroma.NameBuiltin)
	clearPrimitiveHighlights(&chroma.NameTag)
	clearPrimitiveHighlights(&chroma.NameAttribute)
	clearPrimitiveHighlights(&chroma.NameClass)
	clearPrimitiveHighlights(&chroma.NameConstant)
	clearPrimitiveHighlights(&chroma.NameDecorator)
	clearPrimitiveHighlights(&chroma.NameException)
	clearPrimitiveHighlights(&chroma.NameFunction)
	clearPrimitiveHighlights(&chroma.NameOther)
	clearPrimitiveHighlights(&chroma.Literal)
	clearPrimitiveHighlights(&chroma.LiteralNumber)
	clearPrimitiveHighlights(&chroma.LiteralDate)
	clearPrimitiveHighlights(&chroma.LiteralString)
	clearPrimitiveHighlights(&chroma.LiteralStringEscape)
	clearPrimitiveHighlights(&chroma.GenericDeleted)
	clearPrimitiveHighlights(&chroma.GenericEmph)
	clearPrimitiveHighlights(&chroma.GenericInserted)
	clearPrimitiveHighlights(&chroma.GenericStrong)
	clearPrimitiveHighlights(&chroma.GenericSubheading)
}
