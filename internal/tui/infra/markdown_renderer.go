package infra

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
)

// NewGlamourTermRenderer creates a terminal renderer with the requested style and width.
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
	clearBlockHighlights(&cfg.Code)
	clearBlockHighlights(&cfg.CodeBlock.StyleBlock)

	if cfg.CodeBlock.Chroma != nil {
		clearChromaTokenHighlights(cfg.CodeBlock.Chroma)
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
	clearPrimitiveHighlights(&chroma.Background)
}

func normalizeMarkdownANSIStyles(rendered string) string {
	return markdownANSIPattern.ReplaceAllStringFunc(rendered, normalizeANSIEscapeSequence)
}

func normalizeANSIEscapeSequence(seq string) string {
	if !strings.HasPrefix(seq, "\x1b[") || !strings.HasSuffix(seq, "m") {
		return seq
	}

	raw := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
	if raw == "" {
		return seq
	}

	parts := strings.Split(raw, ";")
	result := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		code, err := strconv.Atoi(part)
		if err != nil {
			result = append(result, part)
			continue
		}

		switch {
		case code == 31:
			result = append(result, "33")
		case code == 91:
			result = append(result, "93")
		case code == 48 && i+1 < len(parts):
			mode := parts[i+1]
			if mode == "5" && i+2 < len(parts) {
				i += 2
				continue
			}
			if mode == "2" && i+4 < len(parts) {
				i += 4
				continue
			}
			result = append(result, part)
		case code >= 40 && code <= 49:
			// Drop standard background colors.
		case code >= 100 && code <= 107:
			// Drop bright background colors.
		case code == 38 && i+1 < len(parts):
			mode := parts[i+1]
			if mode == "5" && i+2 < len(parts) {
				paletteCode, paletteErr := strconv.Atoi(parts[i+2])
				if paletteErr == nil && (paletteCode == 1 || paletteCode == 9) {
					result = append(result, "38", "5", "11")
					i += 2
					continue
				}
			}
			if mode == "2" && i+4 < len(parts) {
				r, rErr := strconv.Atoi(parts[i+2])
				g, gErr := strconv.Atoi(parts[i+3])
				b, bErr := strconv.Atoi(parts[i+4])
				if rErr == nil && gErr == nil && bErr == nil && r > g && r > b {
					result = append(result, "38", "2", "255", "255", "0")
					i += 4
					continue
				}
			}
			result = append(result, part)
		default:
			result = append(result, part)
		}
	}

	if len(result) == 0 {
		return "\x1b[m"
	}
	return "\x1b[" + strings.Join(result, ";") + "m"
}
