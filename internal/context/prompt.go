package context

import "strings"

func defaultSystemPrompt() string {
	return `You are NeoCode, a local coding agent.

	Be concise and accurate.
	Use tools when necessary.
	When a tool fails, inspect the error and continue safely.
	 Stay within the workspace and avoid destructive behavior unless clearly requested.`
}

func composeSystemPrompt(parts ...string) string {
	sections := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sections = append(sections, part)
	}
	return strings.Join(sections, "\n\n")
}
