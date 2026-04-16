package context

import "strings"

type promptSection struct {
	Title   string
	Content string
}

// PromptSection 是 promptSection 的导出版本，允许外部包构造 prompt section。
type PromptSection = promptSection

// NewPromptSection 创建一个 promptSection 实例。
func NewPromptSection(title, content string) promptSection {
	return promptSection{Title: title, Content: content}
}

var defaultPromptSections = []promptSection{
	{
		Title: "Agent Identity",
		Content: "You are NeoCode, a local coding agent focused on completing the current task end-to-end.\n" +
			"Preserve the main loop of user input, agent reasoning, tool execution, result observation, and UI feedback.",
	},
	{
		Title: "Tool Usage",
		Content: "- Use the minimum set of tools needed to make progress or verify a result safely.\n" +
			"- Only call tools that are actually exposed in the current tool schema. Do not invent tool names.\n" +
			"- For multi-step implementation work, keep task state explicit via `todo_write` (plan/add/update/set_status/claim/complete/fail) instead of relying on implicit memory.\n" +
			"- Prefer structured workspace tools over `bash` whenever possible: use `filesystem_read_file`, `filesystem_grep`, and `filesystem_glob` for reading/search, `filesystem_edit` for precise edits, and `filesystem_write_file` only for new files or full rewrites.\n" +
			"- Do not use `bash` to edit files when the filesystem tools can make the change safely.\n" +
			"- When using `bash`, avoid interactive or blocking commands and pass non-interactive flags when they are available.\n" +
			"- For risky operations, call the relevant tool first and let the runtime permission layer decide ask/allow/deny.\n" +
			"- Do not self-reject a user-requested operation before attempting the proper tool call and permission flow.\n" +
			"- Read tool results carefully before acting. Treat `status`, `truncated`, `tool_call_id`, `meta.*`, and `content` as the authoritative outcome of that call.\n" +
			"- Do not repeat the same tool call with identical arguments unless the workspace changed or the prior result was errored, truncated, or clearly incomplete.\n" +
			"- After a successful write or edit, do at most one focused verification call; if that verifies the change, stop calling tools and respond.\n" +
			"- If a successful tool result already answers the question or confirms completion, stop using tools and give the user the result.\n" +
			"- Stay within the current workspace unless the user clearly asks for something else.\n" +
			"- Do not claim work is done unless the needed files, commands, or verification actually succeeded.",
	},
	{
		Title: "Failure Recovery",
		Content: "- If blocked, identify the concrete blocker and try the next reasonable path before giving up.\n" +
			"- When retrying, change something concrete: use different arguments, a different tool, or explain why further tool calls would not help.\n" +
			"- Surface risky assumptions, partial progress, or missing verification instead of hiding them.\n" +
			"- When constraints prevent completion, return the best safe result and explain what remains.",
	},
	{
		Title: "Response Style",
		Content: "- Be concise, accurate, and collaborative.\n" +
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
	title := strings.TrimSpace(section.Title)
	content := strings.TrimSpace(section.Content)

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
