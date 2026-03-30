package tui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var writeClipboard = clipboard.WriteAll

func (a *App) shouldSendInput(msg tea.KeyMsg) bool {
	return key.Matches(msg, a.keys.Send) || msg.String() == "ctrl+enter"
}

func (a *App) handleMouse(msg tea.MouseMsg) {
	if !a.transcriptRect.contains(msg.X, msg.Y) {
		return
	}

	switch msg.Button { //nolint:exhaustive
	case tea.MouseButtonWheelUp:
		if msg.Action == tea.MouseActionPress {
			a.focus = panelTranscript
			a.applyFocus()
			a.transcript.LineUp(3)
		}
	case tea.MouseButtonWheelDown:
		if msg.Action == tea.MouseActionPress {
			a.focus = panelTranscript
			a.applyFocus()
			a.transcript.LineDown(3)
		}
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return
		}
		if target := a.hitCodeBlockTarget(msg.X, msg.Y); target != nil {
			a.copyCodeBlock(*target)
			return
		}
		a.focus = panelTranscript
		a.applyFocus()
	}
}

func (a *App) hitCodeBlockTarget(mouseX int, mouseY int) *codeBlockTarget {
	if !a.transcriptRect.contains(mouseX, mouseY) {
		return nil
	}

	contentLine := a.transcript.YOffset + (mouseY - a.transcriptRect.Y)
	contentX := mouseX - a.transcriptRect.X
	for i := range a.codeBlocks {
		target := &a.codeBlocks[i]
		if target.Line != contentLine {
			continue
		}
		if contentX >= target.X && contentX < target.X+target.Width {
			return target
		}
	}
	return nil
}

func (a *App) copyCodeBlock(target codeBlockTarget) {
	if err := writeClipboard(target.Content); err != nil {
		notice := fmt.Sprintf("Copy failed: %v", err)
		a.state.ExecutionError = notice
		a.state.StatusText = notice
		a.appendInlineMessage(roleError, notice)
		a.rebuildTranscript()
		return
	}

	label := "code block"
	if trimmed := strings.TrimSpace(target.Language); trimmed != "" && !strings.EqualFold(trimmed, "text") {
		label = trimmed + " code block"
	}
	notice := fmt.Sprintf("Copied %s.", label)
	a.state.ExecutionError = ""
	a.state.StatusText = notice
	a.appendInlineMessage(roleSystem, notice)
	a.rebuildTranscript()
}

func (a *App) composerRows(availableWidth int) int {
	width := max(8, availableWidth-lipgloss.Width(a.input.Prompt))
	lines := wrappedLineCount(a.input.Value(), width)
	if strings.TrimSpace(a.input.Value()) == "" {
		lines = max(lines, 1)
	}
	return clamp(lines, 1, 8)
}

func (a *App) updateTranscriptRect(lay layout) {
	docX := a.styles.doc.GetPaddingLeft()
	docY := a.styles.doc.GetPaddingTop()
	headerHeight := lipgloss.Height(a.renderHeader(lay.contentWidth))
	rightX := docX
	rightY := docY + headerHeight
	if lay.stacked {
		rightY += lay.sidebarHeight
	} else {
		rightX += lay.sidebarWidth + lay.bodyGap
	}

	a.transcriptRect = rect{
		X:      rightX,
		Y:      rightY,
		Width:  a.transcript.Width,
		Height: a.transcript.Height,
	}
}
