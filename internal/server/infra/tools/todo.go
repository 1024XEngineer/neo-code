package tools

import (
	"context"
	"fmt"
	"go-llm-demo/internal/server/domain"
	"strings"
)

// TodoTool 允许 Agent 管理显式任务清单
type TodoTool struct {
	todoSvc domain.TodoService
}

func NewTodoTool(todoSvc domain.TodoService) *TodoTool {
	return &TodoTool{todoSvc: todoSvc}
}

func (t *TodoTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "todo",
		Description: "Manage an explicit todo list: add tasks, update status, list items, remove items, or clear the list.",
		Parameters: []ToolParamSpec{
			{Name: "action", Type: "string", Required: true, Description: "Action type.", Enum: []string{"add", "update", "list", "remove", "clear"}},
			{Name: "content", Type: "string", Description: "Task content, used by add."},
			{Name: "priority", Type: "string", Description: "Task priority used by add.", DefaultValue: "medium", Enum: []string{"high", "medium", "low"}},
			{Name: "id", Type: "string", Description: "Task id, used by update and remove."},
			{Name: "status", Type: "string", Description: "Task status used by update.", Enum: []string{"pending", "in_progress", "completed"}},
		},
	}
}

func (t *TodoTool) Run(params map[string]interface{}) *ToolResult {
	action, errRes := requiredString(params, "action")
	if errRes != nil {
		errRes.ToolName = t.Definition().Name
		return errRes
	}

	ctx := context.Background()

	normalizedAction := strings.ToLower(action)
	actionType, ok := domain.ParseTodoAction(normalizedAction)
	if !ok {
		return &ToolResult{ToolName: t.Definition().Name, Success: false, Error: fmt.Sprintf("unsupported action: %s", action)}
	}

	switch actionType {
	case domain.TodoActionAdd:
		content, errRes := requiredString(params, "content")
		if errRes != nil {
			errRes.ToolName = t.Definition().Name
			return errRes
		}
		priorityStr := strings.ToLower(optionalStringDefault(params, "priority", string(domain.TodoPriorityMedium)))
		priority, ok := domain.ParseTodoPriority(priorityStr)
		if !ok {
			return &ToolResult{ToolName: t.Definition().Name, Success: false, Error: "invalid priority value"}
		}
		todo, err := t.todoSvc.AddTodo(ctx, content, priority)
		if err != nil {
			return &ToolResult{ToolName: t.Definition().Name, Success: false, Error: err.Error()}
		}
		return &ToolResult{ToolName: t.Definition().Name, Success: true, Output: fmt.Sprintf("Added task: %s (%s)", todo.ID, todo.Content)}

	case domain.TodoActionUpdate:
		id, errRes := requiredString(params, "id")
		if errRes != nil {
			errRes.ToolName = t.Definition().Name
			return errRes
		}
		statusStr, errRes := requiredString(params, "status")
		if errRes != nil {
			errRes.ToolName = t.Definition().Name
			return errRes
		}
		status, ok := domain.ParseTodoStatus(strings.ToLower(statusStr))
		if !ok {
			return &ToolResult{ToolName: t.Definition().Name, Success: false, Error: "invalid status value"}
		}
		err := t.todoSvc.UpdateTodoStatus(ctx, id, status)
		if err != nil {
			return &ToolResult{ToolName: t.Definition().Name, Success: false, Error: err.Error()}
		}
		return &ToolResult{ToolName: t.Definition().Name, Success: true, Output: fmt.Sprintf("Task %s status updated to %s", id, status)}

	case domain.TodoActionList:
		todos, err := t.todoSvc.ListTodos(ctx)
		if err != nil {
			return &ToolResult{ToolName: t.Definition().Name, Success: false, Error: err.Error()}
		}
		if len(todos) == 0 {
			return &ToolResult{ToolName: t.Definition().Name, Success: true, Output: "Todo list is empty"}
		}
		var sb strings.Builder
		sb.WriteString("Todo list:\n")
		for _, todo := range todos {
			statusIcon := "[ ]"
			if todo.Status == domain.TodoInProgress {
				statusIcon = "[/]"
			} else if todo.Status == domain.TodoCompleted {
				statusIcon = "[x]"
			}
			sb.WriteString(fmt.Sprintf("%s %s: %s (priority: %s)\n", statusIcon, todo.ID, todo.Content, todo.Priority))
		}
		return &ToolResult{ToolName: t.Definition().Name, Success: true, Output: sb.String()}

	case domain.TodoActionRemove:
		id, errRes := requiredString(params, "id")
		if errRes != nil {
			errRes.ToolName = t.Definition().Name
			return errRes
		}
		err := t.todoSvc.RemoveTodo(ctx, id)
		if err != nil {
			return &ToolResult{ToolName: t.Definition().Name, Success: false, Error: err.Error()}
		}
		return &ToolResult{ToolName: t.Definition().Name, Success: true, Output: fmt.Sprintf("Removed task %s", id)}

	case domain.TodoActionClear:
		err := t.todoSvc.ClearTodos(ctx)
		if err != nil {
			return &ToolResult{ToolName: t.Definition().Name, Success: false, Error: err.Error()}
		}
		return &ToolResult{ToolName: t.Definition().Name, Success: true, Output: "Todo list cleared"}
	}
	return &ToolResult{ToolName: t.Definition().Name, Success: false, Error: fmt.Sprintf("unsupported action: %s", action)}
}

func optionalStringDefault(params map[string]interface{}, key, fallback string) string {
	val, ok := params[key].(string)
	if !ok || strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}
