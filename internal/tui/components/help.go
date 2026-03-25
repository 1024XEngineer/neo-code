package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func RenderHelp(width int) string {
	var b strings.Builder

	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#61AFEF")).
		Bold(true).
		Render("NeoCode Help")

	b.WriteString(title)
	b.WriteString("\n\n")

	commands := []struct {
		cmd  string
		desc string
	}{
		{"/help", "Show help"},
		{"/pwd | /workspace", "Show the current workspace path"},
		{"/apikey <env_name>", "Switch the API key environment variable"},
		{"/provider <name>", "Switch the model provider"},
		{"/switch <model>", "Switch the active model"},
		{"/memory", "Show memory stats"},
		{"/memory-list", "List stored memory items"},
		{"/project-memory", "Show loaded project memory files"},
		{"/memory-mode", "Show the current memory extraction mode"},
		{"/remember <text>", "Store an explicit durable memory note"},
		{"/resume", "Show the saved working session summary"},
		{"/clear-memory confirm", "Clear persistent memory"},
		{"/clear-context", "Clear the session context"},
		{"/todo | /todo list", "Open or refresh the todo list"},
		{"/todo add <content> [priority]", "Add a todo item"},
		{"/run <code>", "Run code"},
		{"/explain <code>", "Explain code"},
		{"/y | /n", "Approve or reject a pending security prompt"},
		{"/exit | /quit | /q", "Exit the app"},
	}

	cmdStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#98C379")).
		Width(22)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ABB2BF"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#5C6370"))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#61AFEF"))

	for _, c := range commands {
		b.WriteString(cmdStyle.Render(c.cmd))
		b.WriteString(descStyle.Render(c.desc))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Input supports cursor movement, paste, and scrolling. Press F5/F8 to send."))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Chat supports PgUp/PgDn, the mouse wheel, and clicking [Copy] on code blocks."))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Cancel: Ctrl+C"))

	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("Press Esc or /help to close"))

	return lipgloss.NewStyle().MaxWidth(width).Render(b.String())
}
