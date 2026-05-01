package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	agentsession "neo-code/internal/session"
	agentruntime "neo-code/internal/tui/services"
)

func TestParseTodoFilter(t *testing.T) {
	cases := []struct {
		input string
		want  todoFilter
		ok    bool
	}{
		{input: "", ok: false},
		{input: "all", want: todoFilterAll, ok: true},
		{input: "pending", want: todoFilterPending, ok: true},
		{input: "in_progress", want: todoFilterInProgress, ok: true},
		{input: "blocked", want: todoFilterBlocked, ok: true},
		{input: "completed", want: todoFilterCompleted, ok: true},
		{input: "failed", want: todoFilterFailed, ok: true},
		{input: "canceled", want: todoFilterCanceled, ok: true},
		{input: "unknown", ok: false},
	}
	for _, tc := range cases {
		got, ok := parseTodoFilter(tc.input)
		if ok != tc.ok {
			t.Fatalf("parseTodoFilter(%q) ok=%v, want %v", tc.input, ok, tc.ok)
		}
		if got != tc.want {
			t.Fatalf("parseTodoFilter(%q) got=%q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestMapTodoViewItemsSortOrder(t *testing.T) {
	now := time.Now()
	input := []agentsession.TodoItem{
		{ID: "todo-c", Content: "C", Status: agentsession.TodoStatusCompleted, Priority: 1, UpdatedAt: now.Add(-1 * time.Minute)},
		{ID: "todo-a", Content: "A", Status: agentsession.TodoStatusPending, Priority: 2, UpdatedAt: now},
		{ID: "todo-b", Content: "B", Status: agentsession.TodoStatusPending, Priority: 3, UpdatedAt: now.Add(-2 * time.Minute)},
		{ID: "todo-d", Content: "D", Status: agentsession.TodoStatusCompleted, Priority: 5, UpdatedAt: now},
	}

	got := mapTodoViewItems(input)
	if len(got) != len(input) {
		t.Fatalf("expected %d items, got %d", len(input), len(got))
	}

	// status -> priority -> updated_at -> id
	wantOrder := []string{"todo-b", "todo-a", "todo-d", "todo-c"}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Fatalf("order[%d] expected %s, got %s", i, id, got[i].ID)
		}
	}
}

func TestFilterTodoItems(t *testing.T) {
	items := []todoViewItem{
		{ID: "a", Status: "pending"},
		{ID: "b", Status: "completed"},
	}
	all := filterTodoItems(items, todoFilterAll)
	if len(all) != 2 {
		t.Fatalf("expected all size 2, got %d", len(all))
	}
	pending := filterTodoItems(items, todoFilterPending)
	if len(pending) != 1 || pending[0].ID != "a" {
		t.Fatalf("expected only pending item a, got %#v", pending)
	}
}

func TestFormatTodoOwner(t *testing.T) {
	if got := formatTodoOwner("", ""); got != "-" {
		t.Fatalf("expected -, got %q", got)
	}
	if got := formatTodoOwner("", "neo"); got != "neo" {
		t.Fatalf("expected owner id, got %q", got)
	}
	if got := formatTodoOwner("agent", ""); got != "agent" {
		t.Fatalf("expected owner type, got %q", got)
	}
	if got := formatTodoOwner("agent", "neo"); got != "agent/neo" {
		t.Fatalf("expected owner composite, got %q", got)
	}
}

func TestMapTodoViewItemsTieBreakByID(t *testing.T) {
	now := time.Now()
	items := []agentsession.TodoItem{
		{ID: "b", Content: "B", Status: agentsession.TodoStatusPending, Priority: 1, UpdatedAt: now},
		{ID: "a", Content: "A", Status: agentsession.TodoStatusPending, Priority: 1, UpdatedAt: now},
	}
	got := mapTodoViewItems(items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("expected id tie-break sort, got %#v", got)
	}
}

func TestMapTodoViewItemsSortByUpdatedAt(t *testing.T) {
	now := time.Now()
	items := []agentsession.TodoItem{
		{ID: "older", Content: "Older", Status: agentsession.TodoStatusPending, Priority: 1, UpdatedAt: now.Add(-1 * time.Minute)},
		{ID: "newer", Content: "Newer", Status: agentsession.TodoStatusPending, Priority: 1, UpdatedAt: now},
	}
	got := mapTodoViewItems(items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	if got[0].ID != "newer" || got[1].ID != "older" {
		t.Fatalf("expected updated_at desc order, got %#v", got)
	}
}

func TestClampTodoSelection(t *testing.T) {
	if got := clampTodoSelection(-1, 3); got != 0 {
		t.Fatalf("expected clamp to 0, got %d", got)
	}
	if got := clampTodoSelection(5, 3); got != 2 {
		t.Fatalf("expected clamp to 2, got %d", got)
	}
	if got := clampTodoSelection(1, 3); got != 1 {
		t.Fatalf("expected unchanged index 1, got %d", got)
	}
	if got := clampTodoSelection(1, 0); got != 0 {
		t.Fatalf("expected empty length to clamp 0, got %d", got)
	}
}

func TestTodoPreviewHeight(t *testing.T) {
	app, _ := newTestApp(t)
	if got := app.todoPreviewHeight(); got != 0 {
		t.Fatalf("expected hidden todo panel height 0, got %d", got)
	}

	app.todoPanelVisible = true
	if got := app.todoPreviewHeight(); got != 8 {
		t.Fatalf("expected empty visible todo panel height 8, got %d", got)
	}

	app.todoItems = make([]todoViewItem, 30)
	dynamicLimit := (app.height - headerBarHeight) / 2
	if dynamicLimit < todoDefaultExpandedLimit {
		dynamicLimit = todoDefaultExpandedLimit
	}
	if dynamicLimit > todoMaxExpandedLimit {
		dynamicLimit = todoMaxExpandedLimit
	}
	if got := app.todoPreviewHeight(); got != dynamicLimit {
		t.Fatalf("expected clamped todo panel height %d, got %d", dynamicLimit, got)
	}

	app.todoCollapsed = true
	if got := app.todoPreviewHeight(); got != todoCollapsedHeight {
		t.Fatalf("expected collapsed todo panel height %d, got %d", todoCollapsedHeight, got)
	}
}

func TestRenderTodoPreviewAndEmptyRebuild(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoPanelVisible = true
	app.todoFilter = todoFilterPending
	app.todo.Width = 100
	app.todo.Height = 10

	app.rebuildTodo()
	if !strings.Contains(app.todo.View(), "No todos for filter") {
		t.Fatalf("expected empty todo text, got %q", app.todo.View())
	}
	rendered := app.renderTodoPreview(100)
	if !strings.Contains(rendered, todoTitle) {
		t.Fatalf("expected todo title in panel render")
	}

	app.todoCollapsed = true
	rendered = app.renderTodoPreview(100)
	if !strings.Contains(rendered, "Collapsed") {
		t.Fatalf("expected collapsed summary in panel render")
	}
}

func TestSetTodoFilterAndRebuild(t *testing.T) {
	app, _ := newTestApp(t)
	app.todo.Width = 100
	app.todo.Height = 10
	app.todoItems = []todoViewItem{
		{ID: "todo-1", Title: "first", Status: "pending"},
		{ID: "todo-2", Title: "second", Status: "completed"},
	}

	app.setTodoFilter(todoFilterPending)
	if app.todoFilter != todoFilterPending {
		t.Fatalf("expected pending filter, got %q", app.todoFilter)
	}
	if !app.todoPanelVisible {
		t.Fatalf("expected todo panel visible")
	}
	if !strings.Contains(app.todo.View(), "todo-1") || strings.Contains(app.todo.View(), "todo-2") {
		t.Fatalf("expected rendered todo content to respect filter, got %q", app.todo.View())
	}
}

func TestSetAndToggleTodoCollapsed(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoPanelVisible = false
	app.todoItems = []todoViewItem{
		{ID: "todo-1", Title: "Plan", Status: "pending", Priority: 2, Owner: "agent/neo", UpdatedAt: time.Now()},
	}
	app.setTodoCollapsed(true)
	if !app.todoPanelVisible || !app.todoCollapsed {
		t.Fatalf("expected setTodoCollapsed to show panel and collapse it")
	}
	if collapsed := app.toggleTodoCollapsed(); collapsed {
		t.Fatalf("expected toggle to expand panel")
	}
	if app.todoCollapsed {
		t.Fatalf("expected expanded after toggle")
	}
}

func TestOpenSelectedTodoDetail(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoItems = []todoViewItem{
		{ID: "todo-1", Title: "Plan", Status: "pending", Executor: "subagent", Priority: 2, Owner: "agent/neo", UpdatedAt: time.Now()},
	}
	app.todoPanelVisible = true
	app.todoSelectedIndex = 0
	app.openSelectedTodoDetail()

	if len(app.activeMessages) == 0 {
		t.Fatalf("expected detail message appended")
	}
	last := app.activeMessages[len(app.activeMessages)-1]
	if !strings.Contains(messageText(last), "[Todo] todo-1") {
		t.Fatalf("expected todo detail in transcript, got %q", messageText(last))
	}
	if !strings.Contains(messageText(last), "executor: subagent") {
		t.Fatalf("expected executor field in todo detail, got %q", messageText(last))
	}
}

func TestMapTodoViewItemsCarriesExecutor(t *testing.T) {
	now := time.Now()
	items := []agentsession.TodoItem{
		{
			ID:        "todo-e",
			Content:   "executor",
			Status:    agentsession.TodoStatusPending,
			Executor:  agentsession.TodoExecutorSubAgent,
			UpdatedAt: now,
		},
	}
	got := mapTodoViewItems(items)
	if len(got) != 1 {
		t.Fatalf("expected one mapped item, got %d", len(got))
	}
	if got[0].Executor != agentsession.TodoExecutorSubAgent {
		t.Fatalf("mapped executor = %q, want %q", got[0].Executor, agentsession.TodoExecutorSubAgent)
	}
}

func TestHandleImmediateSlashCommandTodoIsNotHandled(t *testing.T) {
	app, _ := newTestApp(t)
	handled, cmd := app.handleImmediateSlashCommand("/todo")
	if handled || cmd != nil {
		t.Fatalf("expected /todo to not be treated as immediate command")
	}
}

func TestTodoPanelKeyInteractionsInUpdate(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoItems = []todoViewItem{
		{ID: "todo-1", Title: "A", Status: "pending"},
		{ID: "todo-2", Title: "B", Status: "pending"},
	}
	app.todoPanelVisible = true
	app.todo.Width = 100
	app.todo.Height = 10
	app.rebuildTodo()
	app.focus = panelTodo

	model, _ := app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app = model.(App)
	if app.todoSelectedIndex != 1 {
		t.Fatalf("expected selection move to index 1, got %d", app.todoSelectedIndex)
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if len(app.activeMessages) == 0 {
		t.Fatalf("expected enter to open todo detail message")
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = model.(App)
	if !app.todoCollapsed {
		t.Fatalf("expected c key to collapse panel")
	}

	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if app.todoCollapsed {
		t.Fatalf("expected enter to expand collapsed todo panel")
	}
}

func TestHandleTodoMouseHeaderTogglesCollapse(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoPanelVisible = true
	app.todoItems = []todoViewItem{{ID: "todo-1", Title: "A", Status: "pending"}}
	app.todoFilter = todoFilterAll
	app.applyComponentLayout(true)
	app.rebuildTodo()

	x, y, _, _ := app.todoBounds()
	handled := app.handleTodoMouse(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if !handled {
		t.Fatalf("expected header click to be handled")
	}
	if !app.todoCollapsed {
		t.Fatalf("expected header click to collapse todo panel")
	}

	x, y, _, _ = app.todoBounds()
	handled = app.handleTodoMouse(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if !handled {
		t.Fatalf("expected second header click to be handled")
	}
	if app.todoCollapsed {
		t.Fatalf("expected second header click to expand todo panel")
	}
}

func TestHandleTodoMouseSelectsItem(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoPanelVisible = true
	app.todoFilter = todoFilterAll
	app.todoItems = []todoViewItem{
		{ID: "todo-1", Title: "A", Status: "pending"},
		{ID: "todo-2", Title: "B", Status: "pending"},
		{ID: "todo-3", Title: "C", Status: "pending"},
	}
	app.layoutCached = false
	app.applyComponentLayout(true)
	app.rebuildTodo()

	x, y, _, _ := app.todoBounds()
	handled := app.handleTodoMouse(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 4,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if !handled {
		t.Fatalf("expected body click to be handled")
	}
	if app.todoSelectedIndex != 1 {
		t.Fatalf("expected click to select second todo item, got %d", app.todoSelectedIndex)
	}
}

func TestHandleTodoMouseWheelMovesByStep(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoPanelVisible = true
	app.todoFilter = todoFilterAll
	for i := 0; i < 20; i++ {
		app.todoItems = append(app.todoItems, todoViewItem{
			ID:       fmt.Sprintf("todo-%02d", i),
			Title:    "task",
			Status:   "pending",
			Priority: 1,
		})
	}
	app.applyComponentLayout(true)
	app.rebuildTodo()

	x, y, _, _ := app.todoBounds()
	handled := app.handleTodoMouse(tea.MouseMsg{
		X:      x + 1,
		Y:      y + 4,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	if !handled {
		t.Fatalf("expected wheel-down in todo panel to be handled")
	}
	want := mouseWheelStepLines
	if want > len(app.visibleTodoItems())-1 {
		want = len(app.visibleTodoItems()) - 1
	}
	if app.todoSelectedIndex != want {
		t.Fatalf("expected wheel-down to move by %d, got %d", want, app.todoSelectedIndex)
	}
}

func TestUpdateInputTodoCommandFallsBackToLocalCommand(t *testing.T) {
	app, _ := newTestApp(t)
	app.focus = panelInput
	app.input.SetValue("/todo collapse")
	app.state.InputText = "/todo collapse"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if cmd == nil {
		t.Fatalf("expected /todo collapse to fall back to local slash command execution")
	}
	if app.todoCollapsed {
		t.Fatalf("did not expect /todo collapse to toggle todo panel via slash command")
	}
	if app.state.StatusText != statusApplyingCommand {
		t.Fatalf("expected applying command status, got %q", app.state.StatusText)
	}
}

func TestMoveTodoSelectionNoVisibleItems(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoPanelVisible = true
	app.todo.Width = 100
	app.todo.Height = 10
	app.todoItems = nil
	app.todoSelectedIndex = 5

	app.moveTodoSelection(1)
	if app.todoSelectedIndex != 5 {
		t.Fatalf("expected no selection change when no visible items, got %d", app.todoSelectedIndex)
	}
}

func TestMoveTodoSelectionWhenCollapsed(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoItems = []todoViewItem{{ID: "todo-1", Status: "pending"}}
	app.todoPanelVisible = true
	app.todoCollapsed = true
	app.todoSelectedIndex = 0

	app.moveTodoSelection(1)
	if app.todoSelectedIndex != 0 {
		t.Fatalf("expected collapsed panel to ignore selection movement")
	}
}

func TestMoveTodoSelectionScrollsViewportForLongList(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoPanelVisible = true
	app.todoCollapsed = false
	app.todo.Width = 120
	app.todo.Height = 3
	app.todoFilter = todoFilterAll
	for i := 0; i < 12; i++ {
		app.todoItems = append(app.todoItems, todoViewItem{
			ID:       fmt.Sprintf("todo-%02d", i),
			Title:    "task",
			Status:   "pending",
			Priority: 1,
		})
	}
	app.rebuildTodo()
	if app.todo.YOffset != 0 {
		t.Fatalf("expected initial offset 0, got %d", app.todo.YOffset)
	}

	for i := 0; i < 8; i++ {
		app.moveTodoSelection(1)
	}
	if app.todoSelectedIndex < 8 {
		t.Fatalf("expected selection moved down, got %d", app.todoSelectedIndex)
	}
	if app.todo.YOffset == 0 {
		t.Fatalf("expected viewport offset to advance for long list")
	}
}

func TestEnsureTodoSelectionVisibleScrollsUpBranch(t *testing.T) {
	app, _ := newTestApp(t)
	app.todo.Height = 3
	app.todoSelectedIndex = 0
	app.todo.SetContent(strings.Repeat("line\n", 30))
	app.todo.SetYOffset(6)
	if app.todo.YOffset != 6 {
		t.Fatalf("expected precondition y offset 6, got %d", app.todo.YOffset)
	}

	app.ensureTodoSelectionVisible(10)
	if app.todo.YOffset >= 6 {
		t.Fatalf("expected y offset to move up, got %d", app.todo.YOffset)
	}
}

func TestRefreshTodosFromSession(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.ActiveSessionID = "session-1"
	runtime.loadSessions = map[string]agentsession.Session{
		"session-1": {
			ID:    "session-1",
			Title: "S1",
			Todos: []agentsession.TodoItem{
				{ID: "todo-1", Content: "Todo 1", Status: agentsession.TodoStatusPending, Priority: 1, UpdatedAt: time.Now()},
			},
		},
	}

	if err := app.refreshTodosFromSession("session-1"); err != nil {
		t.Fatalf("refreshTodosFromSession error = %v", err)
	}
	if len(app.todoItems) != 1 || app.todoItems[0].ID != "todo-1" {
		t.Fatalf("expected todo items synced, got %#v", app.todoItems)
	}
}

func TestRuntimeEventTodoHandlers(t *testing.T) {
	app, runtime := newTestApp(t)
	app.state.ActiveSessionID = "session-1"
	runtime.loadSessions = map[string]agentsession.Session{
		"session-1": {
			ID:    "session-1",
			Title: "S1",
			Todos: []agentsession.TodoItem{
				{ID: "todo-1", Content: "Todo 1", Status: agentsession.TodoStatusPending, Priority: 1, UpdatedAt: time.Now()},
			},
		},
	}

	handled := runtimeEventTodoUpdatedHandler(&app, agentruntime.RuntimeEvent{
		SessionID: "session-1",
		Payload:   agentruntime.TodoEventPayload{Action: "set_status"},
	})
	if handled {
		t.Fatalf("expected todo updated handler to return false")
	}
	if len(app.todoItems) != 1 {
		t.Fatalf("expected todo refresh on updated event")
	}

	handled = runtimeEventTodoConflictHandler(&app, agentruntime.RuntimeEvent{
		SessionID: "session-1",
		Payload:   map[string]any{"reason": "conflict"},
	})
	if handled {
		t.Fatalf("expected todo conflict handler to return false")
	}
	if len(app.activities) == 0 {
		t.Fatalf("expected conflict activity entry")
	}

	before := len(app.activities)
	handled = runtimeEventTodoUpdatedHandler(&app, agentruntime.RuntimeEvent{
		SessionID: "session-2",
		Payload:   agentruntime.TodoEventPayload{Action: "ignored"},
	})
	if handled {
		t.Fatalf("expected ignored session event to return false")
	}
	if len(app.activities) != before {
		t.Fatalf("expected no activity for foreign session")
	}
}

func TestParseTodoEventPayload(t *testing.T) {
	got, ok := parseTodoEventPayload(agentruntime.TodoEventPayload{Action: "a", Reason: "b"})
	if !ok || got.Action != "a" || got.Reason != "b" {
		t.Fatalf("unexpected struct parse result: %#v ok=%v", got, ok)
	}

	payload := &agentruntime.TodoEventPayload{Action: "x", Reason: "y"}
	got, ok = parseTodoEventPayload(payload)
	if !ok || got.Action != "x" || got.Reason != "y" {
		t.Fatalf("unexpected pointer parse result: %#v ok=%v", got, ok)
	}
	var nilPayload *agentruntime.TodoEventPayload
	got, ok = parseTodoEventPayload(nilPayload)
	if ok || got.Action != "" || got.Reason != "" || len(got.Items) != 0 ||
		got.Summary.RequiredTotal != 0 || got.Summary.RequiredCompleted != 0 || got.Summary.RequiredOpen != 0 {
		t.Fatalf("expected nil pointer payload to fail parse, got %#v ok=%v", got, ok)
	}

	got, ok = parseTodoEventPayload(map[string]any{"action": "plan", "reason": "conflict"})
	if !ok || got.Action != "plan" || got.Reason != "conflict" {
		t.Fatalf("unexpected map parse result: %#v ok=%v", got, ok)
	}

	got, ok = parseTodoEventPayload(map[string]any{
		"action":             "set_status",
		"required_total":     3.0,
		"required_completed": 1,
		"required_open":      2,
		"todos": []any{
			map[string]any{
				"id":             "todo-1",
				"content":        "fix bug",
				"status":         "pending",
				"required":       true,
				"blocked_reason": "internal_dependency",
			},
		},
	})
	if !ok {
		t.Fatalf("expected map with summary fields to parse")
	}
	if got.Summary.RequiredTotal != 3 || got.Summary.RequiredCompleted != 1 || got.Summary.RequiredOpen != 2 {
		t.Fatalf("unexpected required summary parse result: %#v", got)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "todo-1" || !got.Items[0].Required {
		t.Fatalf("unexpected todo snapshot parse result: %#v", got)
	}

	got, ok = parseTodoEventPayload(fmt.Errorf("invalid"))
	if ok || got.Action != "" || got.Reason != "" || len(got.Items) != 0 ||
		got.Summary.RequiredTotal != 0 || got.Summary.RequiredCompleted != 0 || got.Summary.RequiredOpen != 0 {
		t.Fatalf("expected invalid payload to fail parse, got %#v ok=%v", got, ok)
	}
}

func TestOpenSelectedTodoDetailWithoutSelection(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoItems = nil
	app.openSelectedTodoDetail()
	if app.state.StatusText != "No todo selected" {
		t.Fatalf("expected no-selection status, got %q", app.state.StatusText)
	}
}

func TestOpenSelectedTodoDetailWhenCollapsed(t *testing.T) {
	app, _ := newTestApp(t)
	app.todoItems = []todoViewItem{{ID: "todo-1", Status: "pending"}}
	app.todoCollapsed = true
	app.openSelectedTodoDetail()
	if app.state.StatusText != "Todo list is collapsed" {
		t.Fatalf("expected collapsed status, got %q", app.state.StatusText)
	}
}

func TestTodoRuntimeEventsRegistered(t *testing.T) {
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventTodoUpdated]; !ok {
		t.Fatalf("expected todo_updated handler to be registered")
	}
	if _, ok := runtimeEventHandlerRegistry[agentruntime.EventTodoConflict]; !ok {
		t.Fatalf("expected todo_conflict handler to be registered")
	}
}

func TestRuntimeEventTodoSnapshotUpdatedHandler(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.ActiveSessionID = "session-1"

	handled := runtimeEventTodoSnapshotUpdatedHandler(&app, agentruntime.RuntimeEvent{
		SessionID: "session-1",
		Payload: map[string]any{
			"action": "snapshot",
			"summary": map[string]any{
				"required_total":     2.0,
				"required_completed": 1.0,
				"required_open":      1.0,
			},
			"items": []any{
				map[string]any{
					"id":       "todo-1",
					"content":  "sync todo",
					"status":   "pending",
					"required": true,
				},
			},
		},
	})
	if handled {
		t.Fatalf("expected todo snapshot handler to return false")
	}
	if len(app.todoItems) != 1 || app.todoItems[0].ID != "todo-1" {
		t.Fatalf("todo items = %+v", app.todoItems)
	}
	if app.state.StatusText != "Todo: 1/2 completed" {
		t.Fatalf("status text = %q", app.state.StatusText)
	}
}

func TestParseTodoEventPayloadCoversCoerceHelpers(t *testing.T) {
	got, ok := parseTodoEventPayload(map[string]any{
		"Action": "update",
		"Reason": "sync",
		"summary": map[string]any{
			"total":              4.0,
			"required_total":     2.0,
			"required_completed": 1.0,
			"required_failed":    1.0,
			"required_open":      0.0,
		},
		"items": []any{
			map[string]any{
				"id":         "todo-1",
				"content":    "A",
				"status":     "completed",
				"required":   "true",
				"revision":   float64(3),
				"artifacts":  []any{" a.md ", "", "b.md"},
				"owner":      "x",
				"not_parsed": 1,
			},
		},
	})
	if !ok {
		t.Fatalf("expected payload to parse")
	}
	if got.Action != "update" || got.Reason != "sync" {
		t.Fatalf("action/reason = %q/%q", got.Action, got.Reason)
	}
	if len(got.Items) != 1 || !got.Items[0].Required || got.Items[0].Revision != 3 {
		t.Fatalf("items = %+v", got.Items)
	}
	if len(got.Items[0].Artifacts) != 2 || got.Items[0].Artifacts[0] != "a.md" {
		t.Fatalf("artifacts = %#v", got.Items[0].Artifacts)
	}
	if got.Summary.RequiredFailed != 1 || got.Summary.Total != 4 {
		t.Fatalf("summary = %+v", got.Summary)
	}
}
