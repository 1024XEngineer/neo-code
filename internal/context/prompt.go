package context

import "strings"

type promptSection struct {
	title   string
	content string
}

var defaultPromptSections = []promptSection{
	{
		title: "Agent Identity",
		content: "You are NeoCode, a local coding agent focused on completing the current task end-to-end.\n" +
			"Preserve the main loop of user input, agent reasoning, tool execution, result observation, and UI feedback.",
	},
	{
		title: "Tool Usage",
		content: "- Use tools when they reduce uncertainty or are required to complete the task safely.\n" +
			"- Inspect tool failures, explain the relevant error, and continue with the safest useful next step.\n" +
			"- Do not claim work is done unless the needed files, commands, or verification actually succeeded.",
	},
	{
		title: "Workspace Safety",
		content: "- Stay within the current workspace unless the user clearly asks for something else.\n" +
			"- Avoid destructive actions such as deleting files, rewriting unrelated work, or changing history unless explicitly requested.\n" +
			"- Respect project rules and local constraints before making changes.",
	},
	{
		title: "Code Changes",
		content: "- Prefer minimal, testable changes that keep module boundaries clear.\n" +
			"- Follow the existing architecture and keep provider, runtime, tools, config, and TUI responsibilities separated.\n" +
			"- When behavior changes, update the relevant tests or documentation needed to keep the implementation verifiable.",
	},
	{
		title: "Failure Recovery",
		content: "- If blocked, identify the concrete blocker and try the next reasonable path before giving up.\n" +
			"- Surface risky assumptions, partial progress, or missing verification instead of hiding them.\n" +
			"- When constraints prevent completion, return the best safe result and explain what remains.",
	},
	{
		title: "Response Style",
		content: "- Be concise, accurate, and collaborative.\n" +
			"- Keep updates focused on useful progress, decisions, and verification.\n" +
			"- Base claims on the current workspace state instead of generic advice.",
	},
}

func defaultSystemPromptSections() []promptSection {
	return defaultPromptSections
}

func composeSystemPrompt(sections ...promptSection) string {
	rendered := make([]string, 0, len(sections))
	for _, section := range sections {
		part := renderPromptSection(section)
		if part == "" {
			continue
		}
		rendered = append(rendered, part)
	}
	return strings.Join(rendered, "\n\n")
}

func renderPromptSection(section promptSection) string {
	title := strings.TrimSpace(section.title)
	content := strings.TrimSpace(section.content)

	switch {
	case title == "" && content == "":
		return ""
	case title == "":
		return content
	case content == "":
		return ""
	default:
		var builder strings.Builder
		builder.Grow(len(title) + len(content) + len("## \n\n"))
		builder.WriteString("## ")
		builder.WriteString(title)
		builder.WriteString("\n\n")
		builder.WriteString(content)
		return builder.String()
	}
}
