package memo

import (
	"bufio"
	"fmt"
	"strings"
	"time"
)

// RenderIndex 将 Index 渲染为 MEMO.md 文本格式。
// 输出格式按类型分组，每条目占一行：`- [type] title (topic_file)`。
func RenderIndex(index *Index) string {
	if index == nil || len(index.Entries) == 0 {
		return ""
	}

	// 按类型分组，保持固定顺序
	typeOrder := []Type{TypeUser, TypeFeedback, TypeProject, TypeReference}
	groups := make(map[Type][]Entry)
	for _, entry := range index.Entries {
		groups[entry.Type] = append(groups[entry.Type], entry)
	}

	var builder strings.Builder
	for _, t := range typeOrder {
		entries, ok := groups[t]
		if !ok || len(entries) == 0 {
			continue
		}
		builder.WriteString("## ")
		builder.WriteString(typeDisplayName(t))
		builder.WriteString("\n")
		for _, entry := range entries {
			builder.WriteString("- [")
			builder.WriteString(string(entry.Type))
			builder.WriteString("] ")
			builder.WriteString(entry.Title)
			if entry.TopicFile != "" {
				builder.WriteString(" (")
				builder.WriteString(entry.TopicFile)
				builder.WriteString(")")
			}
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}

	return builder.String()
}

// ParseIndex 解析 MEMO.md 文本为 Index 结构。
func ParseIndex(content string) (*Index, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return &Index{}, nil
	}

	var entries []Entry
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		entry, ok := parseIndexLine(line)
		if ok {
			entries = append(entries, entry)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("memo: parse index: %w", err)
	}

	return &Index{
		Entries:   entries,
		UpdatedAt: time.Now(),
	}, nil
}

// parseIndexLine 解析单行索引条目，格式：`- [type] title (topic_file)`。
func parseIndexLine(line string) (Entry, bool) {
	// 必须以 "- [" 开头
	if !strings.HasPrefix(line, "- [") {
		return Entry{}, false
	}

	// 提取 [type]
	closeBracket := strings.Index(line, "]")
	if closeBracket < 0 {
		return Entry{}, false
	}
	typeStr := line[3:closeBracket]
	t, ok := ParseType(typeStr)
	if !ok {
		return Entry{}, false
	}

	// 剩余部分
	rest := strings.TrimSpace(line[closeBracket+1:])
	if rest == "" {
		return Entry{}, false
	}

	// 提取 (topic_file) 后缀
	var topicFile string
	if openParen := strings.LastIndex(rest, "("); openParen >= 0 {
		closeParen := strings.LastIndex(rest, ")")
		if closeParen > openParen {
			topicFile = rest[openParen+1 : closeParen]
			rest = strings.TrimSpace(rest[:openParen])
		}
	}

	return Entry{
		Type:      t,
		Title:     rest,
		TopicFile: topicFile,
	}, true
}

// typeDisplayName 返回类型的显示名称。
func typeDisplayName(t Type) string {
	switch t {
	case TypeUser:
		return "User"
	case TypeFeedback:
		return "Feedback"
	case TypeProject:
		return "Project"
	case TypeReference:
		return "Reference"
	default:
		return string(t)
	}
}

// NormalizeTitle 将记忆标题标准化为安全单行文本，避免破坏索引格式与解析约定。
func NormalizeTitle(title string) string {
	normalized := strings.Join(strings.Fields(title), " ")
	normalized = strings.NewReplacer("(", "{", ")", "}").Replace(normalized)
	return strings.TrimSpace(normalized)
}

// RenderTopic 将 Entry 渲染为 topic 文件格式（含 frontmatter）。
func RenderTopic(entry *Entry) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: ")
	builder.WriteString(topicNameFromEntry(entry))
	builder.WriteString("\n")
	builder.WriteString("type: ")
	builder.WriteString(string(entry.Type))
	builder.WriteString("\n")
	if len(entry.Keywords) > 0 {
		builder.WriteString("keywords: [")
		builder.WriteString(strings.Join(entry.Keywords, ", "))
		builder.WriteString("]\n")
	}
	builder.WriteString("source: ")
	builder.WriteString(entry.Source)
	builder.WriteString("\n")
	builder.WriteString("---\n\n")
	builder.WriteString(entry.Content)
	builder.WriteString("\n")
	return builder.String()
}

// topicNameFromEntry 从 Entry 生成 topic 文件名。
func topicNameFromEntry(entry *Entry) string {
	if entry.TopicFile != "" {
		return strings.TrimSuffix(entry.TopicFile, ".md")
	}
	// 按 type + source 生成默认名
	return fmt.Sprintf("%s_%s", entry.Type, entry.Source)
}
