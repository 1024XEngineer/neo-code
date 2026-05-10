package tui

import tuiinfra "neo-code/internal/tui/infra"

const (
	defaultMarkdownStyle    = "dark"
	defaultMarkdownCacheMax = 512
)

type markdownContentRenderer interface {
	Render(content string, width int) (string, error)
}

// newMarkdownRenderer 创建 TUI 使用的 Markdown 渲染器，实际实现下沉到 infra 层。
func newMarkdownRenderer() (markdownContentRenderer, error) {
	return tuiinfra.NewCachedMarkdownRenderer(defaultMarkdownStyle, defaultMarkdownCacheMax, emptyMessageText), nil
}
