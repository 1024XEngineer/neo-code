package core

import (
	"fmt"
	"strings"

	"go-llm-demo/internal/tui/components"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type composerModel struct {
	textarea.Model
}

type composerConsoleState struct {
	modeLabel string
	modeColor string
	metaText  string
	status    string
	noteText  string
}

type composerProfile struct {
	label string
	color string
	hint  string
	note  string
}

func newComposerModel() composerModel {
	input := textarea.New()
	focusedStyle, blurredStyle := textarea.DefaultStyles()
	focusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#61AFEF"))
	blurredStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#5C6370"))
	focusedStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6EAF2"))
	blurredStyle.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("#AAB2C0"))
	input.FocusedStyle = focusedStyle
	input.BlurredStyle = blurredStyle
	input.Placeholder = "Type a message..."
	input.Focus()
	input.ShowLineNumbers = false
	input.SetHeight(1)
	input.Prompt = "> "
	input.CharLimit = 0
	input.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "alt+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "newline"),
	)
	input.KeyMap.InsertNewline.SetEnabled(true)
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#61AFEF"))
	input.Cursor.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6EAF2"))
	_ = input.Cursor.SetMode(cursor.CursorBlink)

	return composerModel{Model: input}
}

func (c *composerModel) desiredHeight() int {
	lines := strings.Count(c.Value(), "\n") + 1
	if lines < 1 {
		return 1
	}
	if lines > 8 {
		return 8
	}
	return lines
}

func (c *composerModel) syncWidth(width int) {
	if width < 20 {
		width = 20
	}
	c.SetWidth(width)
	c.SetHeight(c.desiredHeight())
	c.Prompt = "> "
}

func (c *composerModel) resetForSubmit() {
	c.Reset()
	c.SetHeight(c.desiredHeight())
}

func (c *composerModel) update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	c.Model, cmd = c.Model.Update(msg)
	return cmd
}

func (c *composerModel) focus() tea.Cmd {
	return c.Focus()
}

func (c *composerModel) blur() {
	c.Blur()
}

func (m Model) renderInputArea() string {
	return m.textarea.render(m.composerConsoleState())
}

func visibleComposerBody(body string, height int) string {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return body
	}
	if height <= 0 {
		height = 1
	}

	lines := strings.Split(body, "\n")
	if len(lines) <= height {
		return body
	}
	return strings.Join(lines[len(lines)-height:], "\n")
}

func (m Model) composerConsoleState() composerConsoleState {
	draft := m.textarea.Value()
	trimmedDraft := strings.TrimSpace(draft)
	lines := 0
	if draft != "" {
		lines = strings.Count(draft, "\n") + 1
	}
	charCount := len([]rune(draft))
	profile := m.composerProfile(trimmedDraft)

	return composerConsoleState{
		modeLabel: profile.label,
		modeColor: profile.color,
		metaText:  m.composerMetaText(lines, charCount),
		status:    m.statusText(),
		noteText:  profile.note,
	}
}

func (m Model) composerProfile(trimmedDraft string) composerProfile {
	switch {
	case !m.chat.APIKeyReady:
		return composerProfile{
			label: "RECOVERY",
			color: "#E06C75",
			hint:  "recovery commands only",
			note:  "Available now: /apikey, /provider, /switch, /help, /pwd, /workspace, /exit.",
		}
	case m.chat.PendingApproval != nil && !strings.HasPrefix(trimmedDraft, "/"):
		return composerProfile{
			label: "APPROVAL",
			color: "#D19A66",
			hint:  "approval response required",
			note:  "Approval is pending. Use /y to allow once or /n to reject before sending new text.",
		}
	case strings.HasPrefix(trimmedDraft, "/"):
		return composerProfile{
			label: "COMMAND",
			color: "#61AFEF",
			hint:  "command palette",
			note:  m.commandDraftHelp(trimmedDraft),
		}
	case m.chat.Generating:
		return composerProfile{
			label: "DRAFT",
			color: "#E5C07B",
			hint:  "draft while Neo works",
			note:  "Neo is responding. You can keep drafting the next request while the current answer streams.",
		}
	default:
		note := "Message mode. Enter sends immediately; Shift+Enter inserts a newline."
		if trimmedDraft == "" {
			note = "Ask for code changes, inspections, or use /help to browse commands."
		}
		return composerProfile{
			label: "CHAT",
			color: "#98C379",
			hint:  "Enter sends | Shift+Enter newline",
			note:  note,
		}
	}
}

func (m Model) composerMetaText(lines int, charCount int) string {
	if charCount == 0 {
		return "empty draft"
	}
	return fmt.Sprintf("%d line(s) | %d chars", maxInt(1, lines), charCount)
}

func (m Model) commandDraftHelp(trimmedDraft string) string {
	command := trimmedDraft
	if fields := strings.Fields(trimmedDraft); len(fields) > 0 {
		command = fields[0]
	}

	helpText := map[string]string{
		"/help":          "Open the command and key reference.",
		"/todo":          "View or manage the todo list.",
		"/memory":        "Inspect memory statistics.",
		"/clear-memory":  "Clear persistent memory after confirmation.",
		"/clear":         "Reset the current session context.",
		"/clear-context": "Reset the current session context.",
		"/provider":      "Switch the active provider.",
		"/switch":        "Switch the active model.",
		"/apikey":        "Change the API key environment variable name.",
		"/pwd":           "Show the current workspace path.",
		"/workspace":     "Show the current workspace path.",
		"/y":             "Approve the pending tool call once.",
		"/n":             "Reject the pending tool call.",
		"/exit":          "Quit NeoCode.",
		"/quit":          "Quit NeoCode.",
		"/q":             "Quit NeoCode.",
		"/run":           "Run a code snippet locally.",
		"/explain":       "Send code to Neo for explanation.",
	}

	if text, ok := helpText[command]; ok {
		return text
	}
	return "Command mode. Press Enter to run the command or /help to browse available commands."
}

func (c composerModel) render(state composerConsoleState) string {
	return components.InputBox{
		ModeLabel: state.modeLabel,
		ModeColor: state.modeColor,
		MetaText:  state.metaText,
		Body:      visibleComposerBody(c.View(), c.Height()),
		NoteText:  state.noteText,
		Status:    state.status,
		Width:     maxInt(1, c.Width()),
	}.Render()
}
