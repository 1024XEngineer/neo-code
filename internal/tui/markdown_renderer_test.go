package tui

import (
	"regexp"
	"strings"
	"testing"
)

var markdownTestANSIPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestNewMarkdownRendererAndRender(t *testing.T) {
	rendererAny, err := newMarkdownRenderer()
	if err != nil {
		t.Fatalf("newMarkdownRenderer() error = %v", err)
	}

	renderer, ok := rendererAny.(*glamourMarkdownRenderer)
	if !ok {
		t.Fatalf("expected glamourMarkdownRenderer type, got %T", rendererAny)
	}

	output, err := renderer.Render("# Title\n\n- one\n- two", 40)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if output == "" {
		t.Fatalf("expected non-empty markdown output")
	}
	if len(renderer.renderers) != 1 {
		t.Fatalf("expected one cached term renderer, got %d", len(renderer.renderers))
	}
	if len(renderer.cache) != 1 {
		t.Fatalf("expected one cached render result, got %d", len(renderer.cache))
	}
}

func TestMarkdownRendererHandlesEmptyInputAndCacheEviction(t *testing.T) {
	rendererAny, err := newMarkdownRenderer()
	if err != nil {
		t.Fatalf("newMarkdownRenderer() error = %v", err)
	}
	renderer := rendererAny.(*glamourMarkdownRenderer)

	emptyOutput, err := renderer.Render(" \n\t ", 32)
	if err != nil {
		t.Fatalf("Render(empty) error = %v", err)
	}
	if emptyOutput != emptyMessageText {
		t.Fatalf("expected empty message placeholder, got %q", emptyOutput)
	}

	renderer.maxCacheEntries = 1
	if _, err := renderer.Render("first", 20); err != nil {
		t.Fatalf("Render(first) error = %v", err)
	}
	if _, err := renderer.Render("second", 20); err != nil {
		t.Fatalf("Render(second) error = %v", err)
	}
	if len(renderer.cacheOrder) != 1 || len(renderer.cache) != 1 {
		t.Fatalf("expected cache eviction to keep one entry, got order=%d cache=%d", len(renderer.cacheOrder), len(renderer.cache))
	}
}

func TestMarkdownRendererCachesByWidth(t *testing.T) {
	rendererAny, err := newMarkdownRenderer()
	if err != nil {
		t.Fatalf("newMarkdownRenderer() error = %v", err)
	}
	renderer := rendererAny.(*glamourMarkdownRenderer)

	text := "plain text"
	if _, err := renderer.Render(text, 20); err != nil {
		t.Fatalf("Render(width=20) error = %v", err)
	}
	if _, err := renderer.Render(text, 50); err != nil {
		t.Fatalf("Render(width=50) error = %v", err)
	}
	if len(renderer.renderers) != 2 {
		t.Fatalf("expected width-specific renderer cache, got %d", len(renderer.renderers))
	}
}

func TestMarkdownRendererPreservesChineseText(t *testing.T) {
	rendererAny, err := newMarkdownRenderer()
	if err != nil {
		t.Fatalf("newMarkdownRenderer() error = %v", err)
	}
	renderer := rendererAny.(*glamourMarkdownRenderer)

	output, err := renderer.Render("中文标题\n\n- 第一项\n- 第二项", 40)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	visible := markdownTestANSIPattern.ReplaceAllString(output, "")
	if !strings.Contains(visible, "中文标题") || !strings.Contains(visible, "第一项") || !strings.Contains(visible, "第二项") {
		t.Fatalf("expected chinese markdown content to be preserved, got %q", visible)
	}
}
