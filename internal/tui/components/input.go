package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Input struct {
	Buffer     string
	Multiline  bool
	CursorLine int
	CursorCol  int
}

func (i Input) Render() string {
	cleanBuffer := strings.ReplaceAll(i.Buffer, "\r", "")
	cleanBuffer = strings.ReplaceAll(cleanBuffer, "\t", "    ")
	lines := strings.Split(cleanBuffer, "\n")
	lang := DetectLanguage(i.Buffer)

	var b strings.Builder

	for idx, line := range lines {
		lineNumStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#5C6370"))
		lineNum := lineNumStyle.Render(fmt.Sprintf("  %2d: ", idx+1))
		b.WriteString(lineNum)

		runes := []rune(line)
		if i.Multiline && idx == i.CursorLine && i.CursorCol <= len(runes) {
			cursorStyle := lipgloss.NewStyle().
				Background(lipgloss.Color("#3E4451")).
				Foreground(lipgloss.Color("#ABB2BF"))

			var before, after string
			var char string
			if i.CursorCol < len(runes) {
				char = string(runes[i.CursorCol])
				before = string(runes[:i.CursorCol])
				after = string(runes[i.CursorCol+1:])
			} else {
				before = string(runes)
				char = " "
			}

			if before != "" {
				b.WriteString(HighlightCodeInline(before, lang))
			}
			b.WriteString(cursorStyle.Render(char))
			if after != "" {
				b.WriteString(HighlightCodeInline(after, lang))
			}
		} else {
			if line != "" {
				b.WriteString(HighlightCodeInline(line, lang))
			}
		}
		b.WriteString("\n")
	}

	if i.Multiline {
		b.WriteString("[方向键移动 Enter换行 F5/F8发送 Del删除]")
	} else {
		b.WriteString("[Enter换行 F5/F8发送]")
	}

	return b.String()
}
