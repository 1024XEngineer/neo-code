package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
)

const (
	defaultMarkdownStyle    = "dark"
	defaultMarkdownCacheMax = 128
)

var markdownANSIPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type markdownContentRenderer interface {
	Render(content string, width int) (string, error)
}

type glamourMarkdownRenderer struct {
	renderers       map[int]*glamour.TermRenderer
	cache           map[string]string
	cacheOrder      []string
	maxCacheEntries int
}

func newMarkdownRenderer() (markdownContentRenderer, error) {
	return &glamourMarkdownRenderer{
		renderers:       make(map[int]*glamour.TermRenderer),
		cache:           make(map[string]string),
		cacheOrder:      make([]string, 0, defaultMarkdownCacheMax),
		maxCacheEntries: defaultMarkdownCacheMax,
	}, nil
}

func (r *glamourMarkdownRenderer) Render(content string, width int) (string, error) {
	if strings.TrimSpace(content) == "" {
		return emptyMessageText, nil
	}

	renderWidth := max(16, width)
	cacheKey := fmt.Sprintf("%d:%s", renderWidth, content)
	if cached, ok := r.cache[cacheKey]; ok {
		return cached, nil
	}

	termRenderer, err := r.rendererForWidth(renderWidth)
	if err != nil {
		return "", err
	}

	rendered, err := termRenderer.Render(content)
	if err != nil {
		return "", err
	}
	rendered = strings.TrimRight(rendered, "\n")
	visible := markdownANSIPattern.ReplaceAllString(rendered, "")
	if strings.TrimSpace(visible) == "" {
		rendered = emptyMessageText
	}

	r.cacheResult(cacheKey, rendered)
	return rendered, nil
}

func (r *glamourMarkdownRenderer) rendererForWidth(width int) (*glamour.TermRenderer, error) {
	if renderer, ok := r.renderers[width]; ok {
		return renderer, nil
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(defaultMarkdownStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}

	r.renderers[width] = renderer
	return renderer, nil
}

func (r *glamourMarkdownRenderer) cacheResult(key string, value string) {
	if r.maxCacheEntries <= 0 {
		return
	}
	if _, exists := r.cache[key]; exists {
		r.cache[key] = value
		return
	}
	if len(r.cacheOrder) >= r.maxCacheEntries {
		oldest := r.cacheOrder[0]
		r.cacheOrder = r.cacheOrder[1:]
		delete(r.cache, oldest)
	}
	r.cacheOrder = append(r.cacheOrder, key)
	r.cache[key] = value
}
