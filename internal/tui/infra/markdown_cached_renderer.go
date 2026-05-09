package infra

import (
	"container/list"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
)

var markdownANSIPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// CachedMarkdownRenderer 负责按宽度复用渲染器并缓存渲染结果。
type CachedMarkdownRenderer struct {
	style            string
	emptyPlaceholder string
	renderers        map[int]*glamour.TermRenderer
	cache            map[string]string
	cacheOrder       *list.List
	cacheNodes       map[string]*list.Element
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
		cacheOrder:       list.New(),
		cacheNodes:       make(map[string]*list.Element),
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
	cacheKey := hashMarkdownCacheKey(renderWidth, content)
	if cached, ok := r.cache[cacheKey]; ok {
		if node, exists := r.cacheNodes[cacheKey]; exists {
			r.cacheOrder.MoveToBack(node)
		}
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
	rendered = normalizeMarkdownANSIStyles(rendered)
	rendered = strings.TrimRight(rendered, "\n")
	visible := markdownANSIPattern.ReplaceAllString(rendered, "")
	if strings.TrimSpace(visible) == "" {
		rendered = r.emptyPlaceholder
	}

	r.cacheResult(cacheKey, rendered)
	return rendered, nil
}

func normalizeMarkdownForTerminal(content string) string {
	return content
}

// SetMaxCacheEntries 调整渲染结果缓存上限。
func (r *CachedMarkdownRenderer) SetMaxCacheEntries(max int) {
	if max < 0 {
		max = 0
	}
	r.maxCacheEntries = max
	for r.cacheOrder.Len() > max {
		oldest := r.cacheOrder.Front()
		if oldest == nil {
			break
		}
		oldestKey, _ := oldest.Value.(string)
		r.cacheOrder.Remove(oldest)
		delete(r.cacheNodes, oldestKey)
		delete(r.cache, oldestKey)
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
	return r.cacheOrder.Len()
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
	if node, exists := r.cacheNodes[key]; exists {
		r.cache[key] = value
		r.cacheOrder.MoveToBack(node)
		return
	}
	if r.cacheOrder.Len() >= r.maxCacheEntries {
		oldest := r.cacheOrder.Front()
		if oldest != nil {
			oldestKey, _ := oldest.Value.(string)
			r.cacheOrder.Remove(oldest)
			delete(r.cache, oldestKey)
			delete(r.cacheNodes, oldestKey)
		}
	}
	r.cacheNodes[key] = r.cacheOrder.PushBack(key)
	r.cache[key] = value
}

// maxInt 返回两个整数中的较大值。

// hashMarkdownCacheKey 生成固定长度的缓存键，避免长内容撑大 map key。
func hashMarkdownCacheKey(width int, content string) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%d:", width)
	h.Write([]byte(content))
	return fmt.Sprintf("%016x", h.Sum64())
}
