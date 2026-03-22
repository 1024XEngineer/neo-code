package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var codeBlockStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#ABB2BF")).
	Background(lipgloss.Color("#282C34")).
	Padding(0, 1)

func RenderContent(content string) string {
	if content == "" {
		return "..."
	}

	lines := strings.Split(content, "\n")
	var b strings.Builder

	inCodeBlock := false
	codeLang := ""
	var codeLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if !inCodeBlock {
				inCodeBlock = true
				codeLang = strings.TrimPrefix(line, "```")
				codeLang = strings.TrimSpace(codeLang)
				if codeLang == "" {
					codeLang = "go"
				}
				codeLines = []string{}
				b.WriteString("\n")
			} else {
				inCodeBlock = false
				highlighted := HighlightCodeBlock(codeLines, codeLang)
				b.WriteString(highlighted)
				b.WriteString(codeBlockStyle.Render("```\n"))
				codeLines = nil
			}
			continue
		}

		if inCodeBlock {
			codeLines = append(codeLines, line)
		} else {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return b.String()
}

func HighlightCodeBlock(lines []string, lang string) string {
	var b strings.Builder
	code := strings.Join(lines, "\n")

	b.WriteString(codeBlockStyle.Render("```" + lang + "\n"))

	highlighted := HighlightCode(code, lang)
	highlightedLines := strings.Split(highlighted, "\n")
	for _, line := range highlightedLines {
		b.WriteString(codeBlockStyle.Render(line))
		b.WriteString("\n")
	}

	return b.String()
}
