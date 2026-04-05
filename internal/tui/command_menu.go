package tui

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	maxCommandMenuRows = 6
	commandMenuBrowse  = "@ browse files..."
)

func (a *App) refreshCommandMenu() {
	input := a.input.Value()
	if a.state.ActivePicker != pickerNone {
		a.commandMenu.SetItems(nil)
		a.commandMenuMeta = commandMenuMeta{}
		return
	}

	items, meta := a.buildCommandMenuItems(input, a.transcript.Width)
	if len(items) == 0 {
		a.commandMenu.SetItems(nil)
		a.commandMenuMeta = commandMenuMeta{}
		return
	}

	selectedTitle := ""
	if selected, ok := a.commandMenu.SelectedItem().(commandMenuItem); ok {
		selectedTitle = selected.title
	}

	listItems := make([]list.Item, 0, len(items))
	selectedIndex := 0
	for index, item := range items {
		listItems = append(listItems, item)
		if selectedTitle != "" && strings.EqualFold(item.title, selectedTitle) {
			selectedIndex = index
		}
	}

	a.commandMenu.SetItems(listItems)
	a.commandMenu.Select(selectedIndex)
	a.commandMenuMeta = meta
	a.resizeCommandMenu()
}

func (a *App) resizeCommandMenu() {
	width := max(24, a.transcript.Width)
	rows := clamp(len(a.commandMenu.Items()), 0, maxCommandMenuRows)
	a.commandMenu.SetSize(max(16, width-4), max(1, rows))
}

func (a App) buildCommandMenuItems(input string, width int) ([]commandMenuItem, commandMenuMeta) {
	if suggestions := a.fileMenuSuggestions(input); len(suggestions) > 0 {
		return suggestions, commandMenuMeta{Title: fileMenuTitle}
	}

	trimmed := strings.TrimSpace(input)
	if isWorkspaceCommandInput(trimmed) {
		replacement := trimmed
		item := commandMenuItem{
			title:       workspaceCommandUsage,
			description: trimMiddle(a.state.CurrentWorkdir, max(24, width-28)),
			highlight:   true,
			replacement: replacement,
		}
		if trimmed == workspaceCommandPrefix {
			start, end, _, _ := tokenRange(input, tokenSelectorFirst)
			item.replacement = workspaceCommandPrefix + " "
			item.useReplaceRange = true
			item.replaceStart = start
			item.replaceEnd = end
		}
		return []commandMenuItem{item}, commandMenuMeta{Title: shellMenuTitle}
	}

	suggestions := a.matchingSlashCommands(trimmed)
	if len(suggestions) == 0 {
		return nil, commandMenuMeta{}
	}

	start, end, _, _ := tokenRange(input, tokenSelectorFirst)
	items := make([]commandMenuItem, 0, len(suggestions))
	for _, suggestion := range suggestions {
		items = append(items, commandMenuItem{
			title:           suggestion.Command.Usage,
			description:     suggestion.Command.Description,
			filter:          suggestion.Command.Usage + " " + suggestion.Command.Description,
			highlight:       suggestion.Match,
			replacement:     suggestion.Command.Usage,
			useReplaceRange: true,
			replaceStart:    start,
			replaceEnd:      end,
		})
	}
	return items, commandMenuMeta{Title: commandMenuTitle}
}

func (a App) fileMenuSuggestions(input string) []commandMenuItem {
	start, end, query, suggestions, ok := a.resolveFileReferenceSuggestions(input)
	if !ok {
		return nil
	}
	if len(suggestions) == 0 && query != "" {
		return nil
	}

	items := make([]commandMenuItem, 0, len(suggestions)+1)
	if query == "" {
		items = append(items, commandMenuItem{
			title:           commandMenuBrowse,
			description:     "open workspace file browser",
			filter:          commandMenuBrowse,
			highlight:       true,
			openFileBrowser: true,
		})
	}

	for index, suggestion := range suggestions {
		entry := "@" + suggestion
		items = append(items, commandMenuItem{
			title:           entry,
			description:     "workspace file reference",
			filter:          entry,
			highlight:       index == 0 && query != "",
			replacement:     entry,
			useReplaceRange: true,
			replaceStart:    start,
			replaceEnd:      end,
		})
	}
	return items
}

func (a App) commandMenuHasSuggestions() bool {
	return len(a.commandMenu.Items()) > 0
}

func (a *App) applySelectedCommandSuggestion() bool {
	if !a.commandMenuHasSuggestions() {
		return false
	}
	item, ok := a.commandMenu.SelectedItem().(commandMenuItem)
	if !ok {
		return false
	}
	if item.openFileBrowser {
		a.openFileBrowser()
		return true
	}

	current := a.input.Value()
	next := current
	if item.useReplaceRange {
		if item.replaceStart < 0 || item.replaceEnd < item.replaceStart || item.replaceEnd > len(current) {
			return false
		}
		next = current[:item.replaceStart] + item.replacement + current[item.replaceEnd:]
	} else if strings.TrimSpace(item.replacement) != "" {
		next = item.replacement
	}

	if next == current {
		return false
	}

	a.input.SetValue(next)
	a.state.InputText = next
	a.normalizeComposerHeight()
	a.applyComponentLayout(false)
	a.refreshCommandMenu()
	return true
}

func (a *App) updateCommandMenuSelection(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !a.commandMenuHasSuggestions() {
		return nil, false
	}

	switch msg.Type {
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		var cmd tea.Cmd
		a.commandMenu, cmd = a.commandMenu.Update(msg)
		return cmd, true
	default:
		return nil, false
	}
}

func (a *App) openFileBrowser() {
	workdir := strings.TrimSpace(a.state.CurrentWorkdir)
	if workdir == "" {
		return
	}

	absolute, err := filepath.Abs(workdir)
	if err == nil {
		a.fileBrowser.CurrentDirectory = absolute
	}
	a.state.ActivePicker = pickerFile
	a.state.StatusText = statusBrowseFile
	a.input.Blur()
	a.applyComponentLayout(true)
}
