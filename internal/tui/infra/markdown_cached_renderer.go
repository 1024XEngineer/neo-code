package infra

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
)

var markdownANSIPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)
var markdownTableSeparatorPattern = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$`)

// CachedMarkdownRenderer 负责按宽度复用渲染器并缓存渲染结果。
type CachedMarkdownRenderer struct {
	style            string
	emptyPlaceholder string
	renderers        map[int]*glamour.TermRenderer
	cache            map[string]string
	cacheOrder       []string
	maxCacheEntries  int
}

// NewCachedMarkdownRenderer 创建带缓存的 Markdown 渲染器。
func NewCachedMarkdownRenderer(style string, maxCacheEntries int, emptyPlaceholder string) *CachedMarkdownRenderer {
	if strings.TrimSpace(style) == "" {
		style = "dark"
	}
	if maxCacheEntries < 0 {
		maxCacheEntries = 0
	}
	return &CachedMarkdownRenderer{
		style:            style,
		emptyPlaceholder: emptyPlaceholder,
		renderers:        make(map[int]*glamour.TermRenderer),
		cache:            make(map[string]string),
		cacheOrder:       make([]string, 0, maxCacheEntries),
		maxCacheEntries:  maxCacheEntries,
	}
}

// Render 按给定宽度渲染 Markdown，并做结果缓存与空内容兜底。
func (r *CachedMarkdownRenderer) Render(content string, width int) (string, error) {
	if strings.TrimSpace(content) == "" {
		return r.emptyPlaceholder, nil
	}
	content = normalizeMarkdownForTerminal(content)

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
		rendered = r.emptyPlaceholder
	}

	r.cacheResult(cacheKey, rendered)
	return rendered, nil
}

func normalizeMarkdownForTerminal(content string) string {
	return fenceMarkdownTables(content)
}

func fenceMarkdownTables(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return content
	}

	result := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if !isMarkdownTableHeader(lines, i) {
			result = append(result, lines[i])
			continue
		}

		indent := leadingWhitespace(lines[i])
		end := i + 2
		for end < len(lines) {
			trimmed := strings.TrimSpace(lines[end])
			if trimmed == "" || strings.HasPrefix(trimmed, "```") {
				break
			}
			if !strings.Contains(trimmed, "|") {
				break
			}
			end++
		}

		result = append(result, indent+"```text")
		for row := i; row < end; row++ {
			result = append(result, strings.TrimRight(lines[row], " \t"))
		}
		result = append(result, indent+"```")
		i = end - 1
	}

	return strings.Join(result, "\n")
}

func isMarkdownTableHeader(lines []string, index int) bool {
	if index+1 >= len(lines) {
		return false
	}
	header := strings.TrimSpace(lines[index])
	separator := strings.TrimSpace(lines[index+1])
	if header == "" || separator == "" {
		return false
	}
	if strings.HasPrefix(header, "```") || strings.HasPrefix(separator, "```") {
		return false
	}
	if !strings.Contains(header, "|") {
		return false
	}
	return markdownTableSeparatorPattern.MatchString(separator)
}

func leadingWhitespace(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return line[:i]
		}
	}
	return line
}

// SetMaxCacheEntries 调整渲染结果缓存上限。
func (r *CachedMarkdownRenderer) SetMaxCacheEntries(max int) {
	if max < 0 {
		max = 0
	}
	r.maxCacheEntries = max
	for len(r.cacheOrder) > max {
		oldest := r.cacheOrder[0]
		r.cacheOrder = r.cacheOrder[1:]
		delete(r.cache, oldest)
	}
}

// RendererCount 返回按宽度缓存的渲染器数量。
func (r *CachedMarkdownRenderer) RendererCount() int {
	return len(r.renderers)
}

// CacheCount 返回渲染结果缓存条目数量。
func (r *CachedMarkdownRenderer) CacheCount() int {
	return len(r.cache)
}

// CacheOrderCount 返回缓存队列长度。
func (r *CachedMarkdownRenderer) CacheOrderCount() int {
	return len(r.cacheOrder)
}

// rendererForWidth 获取或创建指定宽度的底层终端渲染器。
func (r *CachedMarkdownRenderer) rendererForWidth(width int) (*glamour.TermRenderer, error) {
	if renderer, ok := r.renderers[width]; ok {
		return renderer, nil
	}

	renderer, err := NewGlamourTermRenderer(r.style, width)
	if err != nil {
		return nil, err
	}

	r.renderers[width] = renderer
	return renderer, nil
}

// cacheResult 将渲染结果写入 LRU 风格缓存。
func (r *CachedMarkdownRenderer) cacheResult(key string, value string) {
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

// maxInt 返回两个整数中的较大值。
