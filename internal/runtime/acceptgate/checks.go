package acceptgate

import (
	"strings"

	agentsession "neo-code/internal/session"
)

func checkOutputOnly(lastAssistantText string) CheckResult {
	if strings.TrimSpace(lastAssistantText) != "" {
		return CheckResult{Passed: true, Name: "output_only"}
	}
	return CheckResult{Passed: false, Name: "output_only", Reason: "assistant output is empty"}
}

func checkRequiredTodoFailures(todos []agentsession.TodoItem) CheckResult {
	for _, todo := range todos {
		if !todo.RequiredValue() {
			continue
		}
		if todo.Status == agentsession.TodoStatusFailed {
			return CheckResult{
				Passed: false,
				Name:   "required_todo_failed",
				Reason: "required todo failed: " + strings.TrimSpace(todo.ID),
			}
		}
	}
	return CheckResult{Passed: true, Name: "required_todo_failed"}
}

func checkRequiredTodoConvergence(todos []agentsession.TodoItem) CheckResult {
	for _, todo := range todos {
		if !todo.RequiredValue() {
			continue
		}
		if !todo.Status.IsTerminal() {
			return CheckResult{
				Passed: false,
				Name:   "required_todo_convergence",
				Reason: "required todo is not terminal: " + strings.TrimSpace(todo.ID),
			}
		}
	}
	return CheckResult{Passed: true, Name: "required_todo_convergence"}
}
