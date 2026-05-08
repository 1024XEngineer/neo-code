package acceptgate

import (
	"path/filepath"
	"strings"

	agentsession "neo-code/internal/session"
)

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

func evaluateAcceptCheck(input Input, check agentsession.AcceptCheck) CheckResult {
	check.Kind = strings.TrimSpace(check.Kind)
	check.Target = strings.TrimSpace(check.Target)
	switch check.Kind {
	case agentsession.AcceptCheckOutputOnly:
		return checkOutputOnly(input, check)
	case agentsession.AcceptCheckWorkspaceChange:
		return checkWorkspaceChange(input, check)
	case agentsession.AcceptCheckCommandSuccess:
		return checkCommandSuccess(input, check)
	case agentsession.AcceptCheckFileExists:
		return checkFileExists(input, check)
	case agentsession.AcceptCheckContentContains:
		return checkContentContains(input, check)
	case agentsession.AcceptCheckToolFact:
		return checkToolFact(input, check)
	default:
		return CheckResult{
			Passed: false,
			Name:   checkName(check),
			Kind:   check.Kind,
			Target: check.Target,
			Reason: "unknown required accept check kind",
		}
	}
}

func checkOutputOnly(input Input, check agentsession.AcceptCheck) CheckResult {
	if strings.TrimSpace(input.LastAssistantText) != "" {
		return pass(check)
	}
	return fail(check, "assistant output is empty")
}

func checkWorkspaceChange(input Input, check agentsession.AcceptCheck) CheckResult {
	if len(input.Facts.Files.Written) > 0 {
		return pass(check)
	}
	for _, item := range input.Facts.Files.Exists {
		switch strings.TrimSpace(item.Source) {
		case "filesystem_write_file", "filesystem_write_file_noop", "filesystem_edit", "bash", "workspace_write":
			return pass(check)
		}
	}
	return fail(check, "missing workspace change evidence")
}

func checkCommandSuccess(input Input, check agentsession.AcceptCheck) CheckResult {
	target := normalizeCommand(check.Target)
	if target == "" {
		return fail(check, "command target is empty")
	}
	for _, fact := range input.Facts.Commands.Executed {
		if !fact.Succeeded {
			continue
		}
		if commandMatches(normalizeCommand(fact.Command), target, check.Match) {
			return pass(check)
		}
	}
	return fail(check, "missing successful command evidence")
}

func checkFileExists(input Input, check agentsession.AcceptCheck) CheckResult {
	target := normalizePath(check.Target)
	if target == "" {
		return fail(check, "file target is empty")
	}
	for _, fact := range input.Facts.Files.Exists {
		if normalizePath(fact.Path) == target {
			return pass(check)
		}
	}
	for _, fact := range input.Facts.Files.Written {
		if normalizePath(fact.Path) == target {
			return pass(check)
		}
	}
	return fail(check, "missing file existence evidence")
}

func checkContentContains(input Input, check agentsession.AcceptCheck) CheckResult {
	target := normalizePath(check.Target)
	if target == "" {
		return fail(check, "content target is empty")
	}
	for _, fact := range input.Facts.Files.ContentMatch {
		if normalizePath(fact.Path) != target || !fact.VerificationPassed {
			continue
		}
		if expected := strings.TrimSpace(check.Params["contains"]); expected != "" {
			if !containsString(fact.ExpectedContains, expected) {
				continue
			}
		}
		return pass(check)
	}
	return fail(check, "missing content match evidence")
}

func checkToolFact(input Input, check agentsession.AcceptCheck) CheckResult {
	scope := strings.TrimSpace(firstNonEmpty(check.Params["scope"], check.Target))
	tool := strings.TrimSpace(check.Params["tool"])
	for _, fact := range input.Facts.Verification.Passed {
		if tool != "" && !strings.EqualFold(strings.TrimSpace(fact.Tool), tool) {
			continue
		}
		if scope != "" && strings.TrimSpace(fact.Scope) != scope {
			continue
		}
		return pass(check)
	}
	return fail(check, "missing tool verification fact")
}

func pass(check agentsession.AcceptCheck) CheckResult {
	return CheckResult{Passed: true, Name: checkName(check), Kind: check.Kind, Target: check.Target}
}

func fail(check agentsession.AcceptCheck, reason string) CheckResult {
	return CheckResult{Passed: false, Name: checkName(check), Kind: check.Kind, Target: check.Target, Reason: reason}
}

func checkName(check agentsession.AcceptCheck) string {
	if id := strings.TrimSpace(check.ID); id != "" {
		return id
	}
	if kind := strings.TrimSpace(check.Kind); kind != "" {
		return kind
	}
	return "accept_check"
}

func commandMatches(actual, target, mode string) bool {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "exact":
		return actual == target
	case "prefix":
		return strings.HasPrefix(actual, target)
	case "contains", "normalized_contains", "":
		return actual == target || strings.Contains(actual, target)
	default:
		return actual == target || strings.Contains(actual, target)
	}
}

func normalizeCommand(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	fields := strings.Fields(value)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if strings.Contains(field, "=") && !strings.Contains(field, "/") && !strings.Contains(field, "\\") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(field), "$env:") {
			continue
		}
		out = append(out, field)
	}
	return strings.ToLower(strings.Join(out, " "))
}

func normalizePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	cleaned = strings.TrimPrefix(cleaned, "./")
	return strings.ToLower(cleaned)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
