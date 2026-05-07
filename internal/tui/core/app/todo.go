package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	agentsession "neo-code/internal/session"
)

type todoFilter string

const (
	todoFilterAll        todoFilter = "all"
	todoFilterPending    todoFilter = "pending"
	todoFilterInProgress todoFilter = "in_progress"
	todoFilterBlocked    todoFilter = "blocked"
	todoFilterCompleted  todoFilter = "completed"
	todoFilterFailed     todoFilter = "failed"
	todoFilterCanceled   todoFilter = "canceled"
)

var orderedTodoStatuses = []todoFilter{
	todoFilterPending,
	todoFilterInProgress,
	todoFilterBlocked,
	todoFilterCompleted,
	todoFilterFailed,
	todoFilterCanceled,
}

var todoStatusRank = map[string]int{
	string(todoFilterPending):    0,
	string(todoFilterInProgress): 1,
	string(todoFilterBlocked):    2,
	string(todoFilterCompleted):  3,
	string(todoFilterFailed):     4,
	string(todoFilterCanceled):   5,
}

const (
	todoCollapsedHeight      = 4
	todoMinExpandedHeight    = 8
	todoDefaultExpandedLimit = 14
	todoMaxExpandedLimit     = 24
	todoHeaderLines          = 1
	todoTitleMaxDefault      = 84
)

type todoViewItem struct {
	ID        string
	Title     string
	Status    string
	Executor  string
	Priority  int
	Owner     string
	UpdatedAt time.Time
}

func parseTodoFilter(input string) (todoFilter, bool) {
	filter := todoFilter(strings.ToLower(strings.TrimSpace(input)))
	switch filter {
	case todoFilterAll,
		todoFilterPending,
		todoFilterInProgress,
		todoFilterBlocked,
		todoFilterCompleted,
		todoFilterFailed,
		todoFilterCanceled:
		return filter, true
	default:
		return "", false
	}
}

func formatTodoOwner(ownerType string, ownerID string) string {
	ownerType = strings.TrimSpace(ownerType)
	ownerID = strings.TrimSpace(ownerID)
	if ownerType == "" && ownerID == "" {
		return "-"
	}
	if ownerType == "" {
		return ownerID
	}
	if ownerID == "" {
		return ownerType
	}
	return ownerType + "/" + ownerID
}

func mapTodoViewItems(items []agentsession.TodoItem) []todoViewItem {
	if len(items) == 0 {
		return nil
	}

	mapped := make([]todoViewItem, 0, len(items))
	for _, item := range items {
		mapped = append(mapped, todoViewItem{
			ID:        strings.TrimSpace(item.ID),
			Title:     strings.TrimSpace(item.Content),
			Status:    strings.TrimSpace(string(item.Status)),
			Executor:  strings.TrimSpace(item.Executor),
			Priority:  item.Priority,
			Owner:     formatTodoOwner(item.OwnerType, item.OwnerID),
			UpdatedAt: item.UpdatedAt,
		})
	}

	sort.SliceStable(mapped, func(i, j int) bool {
		left := mapped[i]
		right := mapped[j]

		leftRank := todoStatusRank[strings.ToLower(left.Status)]
		rightRank := todoStatusRank[strings.ToLower(right.Status)]
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		return left.ID < right.ID
	})

	return mapped
}

func filterTodoItems(items []todoViewItem, filter todoFilter) []todoViewItem {
	if len(items) == 0 {
		return nil
	}
	if filter == todoFilterAll {
		out := make([]todoViewItem, len(items))
		copy(out, items)
		return out
	}

	expected := string(filter)
	out := make([]todoViewItem, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Status), expected) {
			out = append(out, item)
		}
	}
	return out
}

func formatTodoUpdatedAt(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.Format("2006-01-02 15:04:05")
}

func isMarkdownTableSeparatorLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.Trim(trimmed, "|")
	trimmed = strings.ReplaceAll(trimmed, "|", "")
	if trimmed == "" {
		return false
	}
	hasDash := false
	for _, r := range trimmed {
		switch r {
		case '-', ':':
			hasDash = true
		case ' ', '\t':
		default:
			return false
		}
	}
	return hasDash
}

func normalizeTodoTitle(title string, maxLen int) string {
	raw := strings.TrimSpace(title)
	if raw == "" {
		return "(empty)"
	}
	if maxLen <= 0 {
		maxLen = todoTitleMaxDefault
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || isMarkdownTableSeparatorLine(line) {
			continue
		}
		if strings.Contains(line, "|") {
			cells := strings.Split(strings.Trim(line, "|"), "|")
			cleanCells := make([]string, 0, len(cells))
			for _, cell := range cells {
				cell = strings.TrimSpace(cell)
				if cell == "" || isMarkdownTableSeparatorLine(cell) {
					continue
				}
				cleanCells = append(cleanCells, cell)
			}
			if len(cleanCells) > 0 {
				line = strings.Join(cleanCells, " / ")
			}
		}
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			parts = append(parts, line)
		}
	}
	if len(parts) == 0 {
		return "(empty)"
	}
	joined := strings.Join(parts, " | ")
	runes := []rune(joined)
	if len(runes) <= maxLen {
		return joined
	}
	if maxLen < 4 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

func formatTodoStatusLabel(status string) string {
	normalized := strings.ToUpper(strings.TrimSpace(status))
	switch normalized {
	case "PENDING":
		return "PENDING"
	case "IN_PROGRESS":
		return "ACTIVE"
	case "BLOCKED":
		return "BLOCKED"
	case "COMPLETED":
		return "DONE"
	case "FAILED":
		return "FAILED"
	case "CANCELED":
		return "CANCELED"
	default:
		if normalized == "" {
			return "UNKNOWN"
		}
		return normalized
	}
}

func (a App) todoStatusStyle(status string) lipgloss.Style {
	normalized := strings.ToUpper(strings.TrimSpace(status))
	switch normalized {
	case "IN_PROGRESS", "COMPLETED":
		return a.styles.badgeSuccess
	case "BLOCKED":
		return a.styles.badgeWarning
	case "FAILED", "CANCELED":
		return a.styles.badgeError
	default:
		return a.styles.badgeMuted
	}
}

func clampTodoSelection(index int, length int) int {
	if length <= 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}

func (a *App) visibleTodoItems() []todoViewItem {
	return filterTodoItems(a.todoItems, a.todoFilter)
}

func (a *App) setTodoFilter(filter todoFilter) {
	a.todoFilter = filter
	a.todoSelectedIndex = 0
	a.todoCollapsed = false
	if len(a.todoItems) > 0 {
		a.todoPanelVisible = true
	}
	a.rebuildTodo()
}

func (a *App) syncTodos(items []agentsession.TodoItem) {
	a.todoItems = mapTodoViewItems(items)
	if len(a.todoItems) > 0 {
		a.todoPanelVisible = true
	}
	visible := a.visibleTodoItems()
	a.todoSelectedIndex = clampTodoSelection(a.todoSelectedIndex, len(visible))
	a.rebuildTodo()
}

func (a *App) clearTodos() {
	a.todoItems = nil
	a.todoSelectedIndex = 0
	a.todoPanelVisible = false
	a.todoCollapsed = false
	a.rebuildTodo()
}

func (a *App) setTodoCollapsed(collapsed bool) {
	a.todoCollapsed = collapsed
	if len(a.todoItems) > 0 {
		a.todoPanelVisible = true
	}
	if collapsed {
		a.todo.SetYOffset(0)
	}
	a.rebuildTodo()
}

func (a *App) toggleTodoCollapsed() bool {
	next := !a.todoCollapsed
	a.setTodoCollapsed(next)
	return next
}

func (a App) todoPreviewHeight() int {
	if !a.todoPanelVisible {
		return 0
	}
	if a.todoCollapsed {
		return todoCollapsedHeight
	}
	visible := len(a.visibleTodoItems())
	desired := todoMinExpandedHeight
	if visible > 0 {
		// one table header line + one hint line
		desired = visible + 4
	}

	maxHeight := todoDefaultExpandedLimit
	if a.height > 0 {
		dynamicLimit := (a.height - headerBarHeight) / 2
		if dynamicLimit > maxHeight {
			maxHeight = dynamicLimit
		}
	}
	maxHeight = min(todoMaxExpandedLimit, maxHeight)

	return max(todoMinExpandedHeight, min(maxHeight, desired))
}

func (a App) renderTodoPreview(width int) string {
	if !a.todoPanelVisible {
		return ""
	}

	mode := "expanded"
	if a.todoCollapsed {
		mode = "collapsed"
	}
	visible := a.visibleTodoItems()
	subtitle := fmt.Sprintf("%s | Filter: %s | Showing: %d/%d", mode, a.todoFilter, len(visible), len(a.todoItems))
	if len(visible) > 0 {
		current := clampTodoSelection(a.todoSelectedIndex, len(visible)) + 1
		subtitle = fmt.Sprintf("%s | Selected: %d", subtitle, current)
	}
	body := a.todo.View()
	if a.todoCollapsed {
		body = fmt.Sprintf(
			"Collapsed (%d visible / %d total)\nUse Enter or c to expand.",
			len(visible),
			len(a.todoItems),
		)
	}
	return a.renderPanel(
		todoTitle,
		subtitle,
		body,
		width,
		a.todoPreviewHeight(),
		a.focus == panelTodo,
	)
}

func (a *App) rebuildTodo() {
	if !a.todoPanelVisible || a.todo.Height <= 0 {
		a.todo.SetContent("")
		a.todo.GotoTop()
		return
	}
	if a.todoCollapsed {
		a.todo.SetContent("")
		a.todo.GotoTop()
		return
	}

	visible := a.visibleTodoItems()
	a.todoSelectedIndex = clampTodoSelection(a.todoSelectedIndex, len(visible))

	lines := []string{a.styles.panelSubtitle.Render("State       Task")}
	if len(visible) == 0 {
		lines = append(lines, fmt.Sprintf("No todos for filter %q.", a.todoFilter))
	} else {
		titleMax := todoTitleMaxDefault
		if a.todo.Width > 0 {
			titleMax = max(20, a.todo.Width-16)
		}
		for i, item := range visible {
			prefix := " "
			if i == a.todoSelectedIndex {
				prefix = ">"
			}
			title := normalizeTodoTitle(item.Title, titleMax)
			statusLabel := fmt.Sprintf("%-9s", formatTodoStatusLabel(item.Status))
			statusStyled := a.todoStatusStyle(item.Status).Render(statusLabel)

			titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(lightText))
			if i == a.todoSelectedIndex {
				titleStyle = titleStyle.
					Bold(true).
					Foreground(lipgloss.Color(selectionFg)).
					Background(lipgloss.Color(selectionBg))
			}

			metaParts := make([]string, 0, 2)
			if id := strings.TrimSpace(item.ID); id != "" {
				metaParts = append(metaParts, "#"+id)
			}
			if item.Priority > 0 {
				metaParts = append(metaParts, fmt.Sprintf("P%d", item.Priority))
			}
			meta := ""
			if len(metaParts) > 0 {
				meta = " " + a.styles.panelSubtitle.Render("("+strings.Join(metaParts, " · ")+")")
			}

			lines = append(lines, fmt.Sprintf(
				"%s %s %s%s",
				prefix,
				statusStyled,
				titleStyle.Render(title),
				meta,
			))
		}

		selected := visible[clampTodoSelection(a.todoSelectedIndex, len(visible))]
		details := make([]string, 0, 5)
		if id := strings.TrimSpace(selected.ID); id != "" {
			details = append(details, "id="+id)
		}
		if selected.Priority > 0 {
			details = append(details, fmt.Sprintf("priority=%d", selected.Priority))
		}
		if exec := strings.TrimSpace(selected.Executor); exec != "" && exec != "-" {
			details = append(details, "executor="+exec)
		}
		if owner := strings.TrimSpace(selected.Owner); owner != "" && owner != "-" {
			details = append(details, "owner="+owner)
		}
		if updated := formatTodoUpdatedAt(selected.UpdatedAt); updated != "-" {
			details = append(details, "updated="+updated)
		}
		if len(details) > 0 {
			lines = append(lines, a.styles.panelSubtitle.Render("Selected: "+strings.Join(details, " · ")))
		}

		lines = append(
			lines,
			a.styles.panelSubtitle.Render(
				fmt.Sprintf(
					"Selected %d/%d · Up/Down move · Enter detail · c collapse",
					a.todoSelectedIndex+1,
					len(visible),
				),
			),
		)
	}

	content := strings.Join(lines, "\n")
	a.todo.SetContent(content)
	a.ensureTodoSelectionVisible(len(visible))
}

func (a *App) moveTodoSelection(delta int) {
	if a.todoCollapsed {
		return
	}
	visible := a.visibleTodoItems()
	if len(visible) == 0 {
		return
	}
	a.todoSelectedIndex = clampTodoSelection(a.todoSelectedIndex+delta, len(visible))
	a.rebuildTodo()
}

func (a *App) ensureTodoSelectionVisible(visibleCount int) {
	if visibleCount <= 0 || a.todo.Height <= 0 {
		a.todo.SetYOffset(0)
		return
	}

	// Row 0 is header, todo rows start at line 1.
	selectedLine := todoHeaderLines + clampTodoSelection(a.todoSelectedIndex, visibleCount)
	top := max(0, a.todo.YOffset)
	bottom := top + max(1, a.todo.Height) - 1

	switch {
	case selectedLine < top:
		a.todo.SetYOffset(selectedLine)
	case selectedLine > bottom:
		a.todo.SetYOffset(selectedLine - max(1, a.todo.Height) + 1)
	}
}

func (a *App) openSelectedTodoDetail() {
	if a.todoCollapsed {
		a.state.StatusText = "Todo list is collapsed"
		return
	}
	visible := a.visibleTodoItems()
	if len(visible) == 0 {
		a.state.StatusText = "No todo selected"
		return
	}
	current := visible[clampTodoSelection(a.todoSelectedIndex, len(visible))]
	lines := []string{
		fmt.Sprintf("[Todo] %s", current.ID),
		fmt.Sprintf("title: %s", current.Title),
		fmt.Sprintf("status: %s", current.Status),
		fmt.Sprintf("executor: %s", fallbackText(current.Executor, "-")),
		fmt.Sprintf("priority: %d", current.Priority),
		fmt.Sprintf("owner: %s", current.Owner),
		fmt.Sprintf("updated_at: %s", formatTodoUpdatedAt(current.UpdatedAt)),
	}
	a.appendInlineMessage(roleSystem, strings.Join(lines, "\n"))
	a.rebuildTranscript()
	a.state.StatusText = fmt.Sprintf("Opened todo %s", current.ID)
}
