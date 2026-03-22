package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type Message struct {
	Role      string
	Content   string
	Timestamp time.Time
	Streaming bool
}

type MessageList struct {
	Messages []Message
	Width    int
}

func (ml MessageList) Render() string {
	if len(ml.Messages) == 0 {
		return ""
	}

	const maxVisible = 30
	visibleMessages := ml.Messages
	if len(ml.Messages) > maxVisible {
		visibleMessages = ml.Messages[len(ml.Messages)-maxVisible:]
	}

	userMsgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#98C379")).
		Bold(true)

	assistantMsgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5C07B"))

	systemMsgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C678DD"))

	var b strings.Builder

	for i, msg := range visibleMessages {
		idx := len(ml.Messages) - len(visibleMessages) + i + 1
		switch msg.Role {
		case "user":
			b.WriteString(userMsgStyle.Render(fmt.Sprintf("你 [%d]:", idx)))
			b.WriteString(" ")
			b.WriteString(msg.Content)
			b.WriteString("\n\n")

		case "assistant":
			b.WriteString(assistantMsgStyle.Render(fmt.Sprintf("Neo [%d]:", idx)))
			b.WriteString("\n")
			b.WriteString(RenderContent(msg.Content))
			b.WriteString("\n\n")

		case "system":
			b.WriteString(systemMsgStyle.Render("[系统]"))
			b.WriteString(" ")
			b.WriteString(msg.Content)
			b.WriteString("\n\n")
		}
	}

	return b.String()
}
