package infra

import (
	"testing"

	"github.com/charmbracelet/glamour/ansi"
)

func TestStripNonCodeHighlightBackgrounds(t *testing.T) {
	red := "#ff0000"
	orange := "#ff9900"
	gray := "#333333"
	dark := "#202020"
	inverse := true

	cfg := ansi.StyleConfig{
		Text: ansi.StylePrimitive{
			BackgroundColor: &red,
			Inverse:         &inverse,
		},
		Emph: ansi.StylePrimitive{
			BackgroundColor: &orange,
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BackgroundColor: &gray,
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					BackgroundColor: &dark,
				},
			},
			Chroma: &ansi.Chroma{
				LiteralString: ansi.StylePrimitive{
					BackgroundColor: &orange,
				},
				GenericDeleted: ansi.StylePrimitive{
					BackgroundColor: &red,
					Inverse:         &inverse,
				},
				Background: ansi.StylePrimitive{
					BackgroundColor: &gray,
				},
			},
		},
	}

	stripNonCodeHighlightBackgrounds(&cfg)

	if cfg.Text.BackgroundColor != nil || cfg.Text.Inverse != nil {
		t.Fatalf("expected text highlight background/inverse to be cleared")
	}
	if cfg.Text.Color != nil || cfg.Text.Bold != nil {
		t.Fatalf("expected text decorative styling to be cleared")
	}
	if cfg.Emph.BackgroundColor != nil {
		t.Fatalf("expected emphasis highlight background to be cleared")
	}
	if cfg.Emph.Color != nil || cfg.Emph.Italic != nil {
		t.Fatalf("expected emphasis decorative styling to be cleared")
	}
	if cfg.Code.BackgroundColor == nil {
		t.Fatalf("expected inline code gray background to be preserved")
	}
	if got := *cfg.Code.BackgroundColor; got != markdownNeutralCodeBackground {
		t.Fatalf("expected inline code gray background %q, got %q", markdownNeutralCodeBackground, got)
	}
	if cfg.CodeBlock.BackgroundColor == nil {
		t.Fatalf("expected code block gray background to be preserved")
	}
	if got := *cfg.CodeBlock.BackgroundColor; got != markdownNeutralCodeBackground {
		t.Fatalf("expected code block gray background %q, got %q", markdownNeutralCodeBackground, got)
	}
	if cfg.CodeBlock.Chroma == nil {
		t.Fatalf("expected chroma config to remain present")
	}
	if cfg.CodeBlock.Chroma.LiteralString.BackgroundColor != nil {
		t.Fatalf("expected chroma token background to be cleared")
	}
	if cfg.CodeBlock.Chroma.GenericDeleted.BackgroundColor != nil || cfg.CodeBlock.Chroma.GenericDeleted.Inverse != nil {
		t.Fatalf("expected chroma deleted token highlight to be cleared")
	}
	if cfg.CodeBlock.Chroma.Background.BackgroundColor == nil {
		t.Fatalf("expected neutral chroma background to be preserved")
	}
	if got := *cfg.CodeBlock.Chroma.Background.BackgroundColor; got != markdownNeutralCodeBackground {
		t.Fatalf("expected neutral chroma background %q, got %q", markdownNeutralCodeBackground, got)
	}
}
