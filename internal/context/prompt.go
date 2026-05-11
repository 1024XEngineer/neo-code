package context

import (
	"strings"

	"neo-code/internal/promptasset"
)

type promptSection struct {
	Title   string
	Content string
}

// PromptSection 是 promptSection 的导出版本，允许外部包构造 prompt section。
type PromptSection = promptSection

// NewPromptSection 创建一个 promptSection 实例。
func NewPromptSection(title, content string) promptSection {
	return promptSection{Title: title, Content: content}
}

// defaultSystemPromptSections 返回由模板资产驱动的主会话核心 prompt sections。
func defaultSystemPromptSections() []promptSection {
	templates := promptasset.CoreSections()
	sections := make([]promptSection, 0, len(templates))
	for _, section := range templates {
		sections = append(sections, promptSection{
			Title:   section.Title,
			Content: section.Content,
		})
	}
	return sections
}

// joinSystemPromptParts 将 stable 和 dynamic 两部分提示词拼接为最终 SystemPrompt。
// 空部分会被跳过，非空部分之间用两个换行分隔。
func joinSystemPromptParts(parts ...string) string {
	rendered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		rendered = append(rendered, part)
	}
	return strings.Join(rendered, "\n\n")
}

func composeSystemPrompt(sections ...promptSection) string {
	rendered := make([]string, 0, len(sections))
	for _, section := range sections {
		part := renderPromptSection(section)
		if part == "" {
			continue
		}
		rendered = append(rendered, part)
	}
	return strings.Join(rendered, "\n\n")
}

func renderPromptSection(section promptSection) string {
	title := strings.TrimSpace(section.Title)
	content := strings.TrimSpace(section.Content)

	switch {
	case title == "" && content == "":
		return ""
	case title == "":
		return content
	case content == "":
		return ""
	default:
		var builder strings.Builder
		builder.Grow(len(title) + len(content) + len("## \n\n"))
		builder.WriteString("## ")
		builder.WriteString(title)
		builder.WriteString("\n\n")
		builder.WriteString(content)
		return builder.String()
	}
}
