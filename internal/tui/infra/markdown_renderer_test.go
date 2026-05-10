package infra

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour/ansi"
)

func TestStripNonCodeHighlightBackgrounds(t *testing.T) {
	red := "#ff0000"
	yellow := "#ffff00"
	orange := "#ff9900"
	gray := "#333333"
	dark := "#202020"
	inverse := true
	bold := true

	cfg := ansi.StyleConfig{
		Text: ansi.StylePrimitive{
			BackgroundColor: &red,
			Inverse:         &inverse,
			Color:           &yellow,
			Bold:            &bold,
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
	if cfg.Text.Color == nil || *cfg.Text.Color != yellow {
		t.Fatalf("expected text foreground color to be preserved")
	}
	if cfg.Text.Bold == nil || *cfg.Text.Bold != bold {
		t.Fatalf("expected text emphasis styling to be preserved")
	}
	if cfg.Emph.BackgroundColor != nil {
		t.Fatalf("expected emphasis highlight background to be cleared")
	}
	if cfg.Code.BackgroundColor != nil {
		t.Fatalf("expected inline code background to be removed")
	}
	if cfg.CodeBlock.BackgroundColor != nil {
		t.Fatalf("expected code block background to be removed")
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
	if cfg.CodeBlock.Chroma.Background.BackgroundColor != nil {
		t.Fatalf("expected chroma background to be removed")
	}
}

func TestNormalizeMarkdownANSIStylesStripsBackgroundAndTurnsRedToYellow(t *testing.T) {
	input := "\x1b[31mred\x1b[0m \x1b[1;91mbright-red\x1b[0m \x1b[44mblue-bg\x1b[0m \x1b[33myellow\x1b[0m"
	got := normalizeMarkdownANSIStyles(input)

	if strings.Contains(got, "[31m") || strings.Contains(got, "[91m") {
		t.Fatalf("expected red ANSI foreground to be remapped to yellow, got %q", got)
	}
	if strings.Contains(got, "[44m") {
		t.Fatalf("expected background ANSI code to be removed, got %q", got)
	}
	if !strings.Contains(got, "[33m") || !strings.Contains(got, "[1;93m") {
		t.Fatalf("expected yellow ANSI codes in output, got %q", got)
	}
	if !strings.Contains(got, "\x1b[33myellow\x1b[0m") {
		t.Fatalf("expected existing yellow ANSI foreground to be preserved, got %q", got)
	}
}

func TestNormalizeMarkdownANSIStylesHandlesExtendedSequences(t *testing.T) {
	input := "\x1b[38;5;1mred-256\x1b[0m \x1b[38;2;255;0;0mred-rgb\x1b[0m \x1b[48;5;240mgray-bg\x1b[0m"
	got := normalizeMarkdownANSIStyles(input)

	if strings.Contains(got, "\x1b[38;5;1m") {
		t.Fatalf("expected 256-color red to be remapped, got %q", got)
	}
	if strings.Contains(got, "\x1b[38;2;255;0;0m") {
		t.Fatalf("expected RGB red to be remapped, got %q", got)
	}
	if strings.Contains(got, "48;5;240") {
		t.Fatalf("expected extended background to be removed, got %q", got)
	}
	if !strings.Contains(got, "38;5;11") {
		t.Fatalf("expected 256-color yellow replacement, got %q", got)
	}
	if !strings.Contains(got, "38;2;255;255;0") {
		t.Fatalf("expected RGB yellow replacement, got %q", got)
	}
}
