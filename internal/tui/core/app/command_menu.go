package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	agentsession "neo-code/internal/session"
	tuicomponents "neo-code/internal/tui/components"
	tuiutils "neo-code/internal/tui/core/utils"
	tuistate "neo-code/internal/tui/state"
)

const (
	maxCommandMenuRows = 6
)

type commandMenuItem struct {
	title           string
	description     string
	filter          string
	highlight       bool
	replacement     string
	useReplaceRange bool
	replaceStart    int
	replaceEnd      int
}

func (c commandMenuItem) Title() string {
	return c.title
}

func (c commandMenuItem) Description() string {
	return c.description
}

func (c commandMenuItem) FilterValue() string {
	base := strings.TrimSpace(c.filter)
	if base != "" {
		return strings.ToLower(base)
	}
	return strings.ToLower(c.title + " " + c.description)
}

type commandMenuDelegate struct {
	styles styles
}

func (d commandMenuDelegate) Height() int {
	return 1
}

func (d commandMenuDelegate) Spacing() int {
	return 0
}

func (d commandMenuDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d commandMenuDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	entry, ok := item.(commandMenuItem)
	if !ok {
		return
	}
	fmt.Fprint(w, tuicomponents.RenderCommandMenuRow(tuicomponents.CommandMenuRowData{
		Title:            entry.title,
		Description:      entry.description,
		Highlight:        entry.highlight,
		Selected:         index == m.Index(),
		Width:            m.Width(),
		UsageStyle:       d.styles.commandUsage,
		UsageMatchStyle:  d.styles.commandUsageMatch,
		DescriptionStyle: d.styles.commandDesc,
	}))
}

type sessionItem struct {
	Summary agentsession.Summary
	Active  bool
}

func (s sessionItem) Title() string {
	return normalizeSessionTitleSingleLine(s.Summary.Title)
}

func (s sessionItem) Description() string {
	return s.Summary.UpdatedAt.Format("01-02 15:04")
}

func (s sessionItem) FilterValue() string {
	return strings.ToLower(normalizeSessionTitleSingleLine(s.Summary.Title))
}

type selectionItem struct {
	id          string
	name        string
	description string
}

func (s selectionItem) Title() string {
	return s.name
}

func (s selectionItem) Description() string {
	return s.description
}

func (s selectionItem) FilterValue() string {
	return strings.ToLower(s.id + " " + s.name + " " + s.description)
}

type pickerSelectionDelegate struct {
	mainStyle         lipgloss.Style
	mainSelectedStyle lipgloss.Style
	subStyle          lipgloss.Style
	subSelectedStyle  lipgloss.Style
	rowStyle          lipgloss.Style
	railStyle         lipgloss.Style
	railSelectedStyle lipgloss.Style
	gap               string
}

// newPickerSelectionDelegate 构建选择器行渲染所需的稳定样式，避免每次 Render 重建样式对象。
func newPickerSelectionDelegate() pickerSelectionDelegate {
	return pickerSelectionDelegate{
		mainStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(warmSilver)),
		mainSelectedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(lightText)).
			Bold(true),
		subStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(charcoal)),
		subSelectedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(lightText2)),
		rowStyle: lipgloss.NewStyle(),
		railStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(charcoal)).
			Width(1),
		railSelectedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(lightText)).
			Width(1),
		gap: lipgloss.NewStyle().Width(1).Render(" "),
	}
}

func (d pickerSelectionDelegate) Height() int {
	return 2
}

func (d pickerSelectionDelegate) Spacing() int {
	return 0
}

func (d pickerSelectionDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d pickerSelectionDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	title, subtitle := pickerItemText(item)
	selected := index == m.Index()

	rowWidth := max(12, m.Width())
	contentWidth := max(8, rowWidth-2) // left rail + spacing

	titleWidth := max(4, contentWidth)
	title = tuiutils.TrimMiddle(strings.TrimSpace(title), titleWidth)
	subtitle = tuiutils.TrimMiddle(strings.TrimSpace(subtitle), contentWidth)
	if subtitle == "" {
		subtitle = " "
	}

	mainStyle := d.mainStyle
	subStyle := d.subStyle
	rowStyle := d.rowStyle.Width(contentWidth)
	railStyle := d.railStyle
	railGlyph := " "

	if selected {
		mainStyle = d.mainSelectedStyle
		subStyle = d.subSelectedStyle
		railStyle = d.railSelectedStyle
		railGlyph = "|"
	}

	mainLine := mainStyle.Width(titleWidth).Render(title)
	subLine := subStyle.Width(contentWidth).Render(subtitle)
	row := rowStyle.Render(mainLine + "\n" + subLine)

	lines := strings.Split(row, "\n")
	for i := range lines {
		lines[i] = railStyle.Render(railGlyph) + d.gap + lines[i]
	}
	fmt.Fprint(w, strings.Join(lines, "\n"))
}

func pickerItemText(item list.Item) (string, string) {
	switch entry := item.(type) {
	case selectionItem:
		return strings.TrimSpace(entry.name), strings.TrimSpace(entry.description)
	case sessionItem:
		return normalizeSessionTitleSingleLine(entry.Summary.Title), strings.TrimSpace(entry.Summary.UpdatedAt.Format("01-02 15:04"))
	default:
		var title, subtitle string
		if titled, ok := item.(interface{ Title() string }); ok {
			title = strings.TrimSpace(titled.Title())
		}
		if described, ok := item.(interface{ Description() string }); ok {
			subtitle = strings.TrimSpace(described.Description())
		}
		return title, subtitle
	}
}

func normalizeSessionTitleSingleLine(title string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
}

type sessionDelegate struct {
	styles styles
}

func (d sessionDelegate) Height() int {
	return 3
}

func (d sessionDelegate) Spacing() int {
	return 1
}

func (d sessionDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d sessionDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	session, ok := item.(sessionItem)
	if !ok {
		return
	}
	fmt.Fprint(w, tuicomponents.RenderSessionRow(tuicomponents.SessionRowData{
		Title:           normalizeSessionTitleSingleLine(session.Summary.Title),
		UpdatedAtLabel:  session.Summary.UpdatedAt.Format("01-02 15:04"),
		Active:          session.Active,
		Selected:        index == m.Index(),
		Width:           m.Width(),
		RowStyle:        d.styles.sessionRow,
		RowActiveStyle:  d.styles.sessionRowActive,
		RowFocusStyle:   d.styles.sessionRowFocused,
		MetaStyle:       d.styles.sessionMeta,
		MetaActiveStyle: d.styles.sessionMetaActive,
		MetaFocusStyle:  d.styles.sessionMetaFocus,
	}))
}

func (a *App) refreshCommandMenu() {
	input := a.input.Value()
	if a.state.ActivePicker != pickerNone {
		a.commandMenu.SetItems(nil)
		a.commandMenuMeta = tuistate.CommandMenuMeta{}
		return
	}

	items, meta := a.buildCommandMenuItems(input, a.transcript.Width)
	if len(items) == 0 {
		hadSuggestions := len(a.commandMenu.Items()) > 0
		a.commandMenu.SetItems(nil)
		a.commandMenuMeta = tuistate.CommandMenuMeta{}
		if hadSuggestions {
			a.resizeCommandMenu()
			a.applyComponentLayout(false)
		}
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
	a.applyComponentLayout(true)
}

func (a *App) resizeCommandMenu() {
	width := max(24, a.transcript.Width)
	rows := tuiutils.Clamp(len(a.commandMenu.Items()), 0, maxCommandMenuRows)
	a.commandMenu.SetSize(max(16, width-4), max(1, rows))
}

func (a App) buildCommandMenuItems(input string, _ int) ([]commandMenuItem, tuistate.CommandMenuMeta) {
	leadingTrimmed := strings.TrimLeft(input, " \t\r\n")

	// 1. 优先检查 Slash 命令
	if strings.HasPrefix(leadingTrimmed, slashPrefix) {
		suggestions := a.matchingSlashCommands(leadingTrimmed)
		if len(suggestions) > 0 {
			start, end := slashInputReplaceRange(input)
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
			return items, tuistate.CommandMenuMeta{Title: commandMenuTitle}
		}
	}

	// 2. 检查文件建议 (如果 Slash 命令不匹配)
	if suggestions := a.fileMenuSuggestions(input); len(suggestions) > 0 {
		return suggestions, tuistate.CommandMenuMeta{Title: fileMenuTitle}
	}

	return nil, tuistate.CommandMenuMeta{}
}

func slashInputReplaceRange(input string) (start int, end int) {
	start = 0
	for start < len(input) {
		switch input[start] {
		case ' ', '\t', '\r', '\n':
			start++
		default:
			return start, len(input)
		}
	}
	return 0, len(input)
}

func (a App) fileMenuSuggestions(input string) []commandMenuItem {
	start, end, query, suggestions, ok := a.resolveFileReferenceSuggestions(input)
	if !ok {
		return nil
	}
	if len(suggestions) == 0 && query != "" {
		return nil
	}

	items := make([]commandMenuItem, 0, len(suggestions))
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
		return nil, false // 让按键继续传递
	}

	switch msg.Type {
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		var cmd tea.Cmd
		a.commandMenu, cmd = a.commandMenu.Update(msg)
		return cmd, true
	default:
		return nil, false // 非导航键，让它们继续传递
	}
}
