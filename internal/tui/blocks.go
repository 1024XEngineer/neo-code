package tui

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/mattn/go-runewidth"

	"neocode/internal/provider"
	"neocode/internal/runtime"
)

type blockKind string

const (
	blockParagraph  blockKind = "paragraph"
	blockCode       blockKind = "code"
	blockToolCall   blockKind = "tool_call"
	blockToolResult blockKind = "tool_result"
)

type messageBlock struct {
	Kind     blockKind
	Text     string
	Language string
	ToolCall provider.ToolCall
}

type renderedConversation struct {
	Content    string
	Messages   []renderedMessage
	CodeBlocks []renderedCodeBlock
	LineCount  int
}

type renderedMessage struct {
	MessageIndex int
	StartLine    int
	EndLine      int
	CopyText     string
}

type renderedCodeBlock struct {
	Index        int
	MessageIndex int
	Language     string
	Content      string
	StartLine    int
	EndLine      int
	HeaderLine   int
	CopyX1       int
	CopyX2       int
}

type filteredSession struct {
	Summary         runtime.SessionSummary
	IsActive        bool
	IsBusy          bool
	HasFreshOutput  bool
	HasRunningTools bool
}

func parseMessageBlocks(message provider.Message) []messageBlock {
	blocks := make([]messageBlock, 0, len(message.ToolCalls)+2)
	for _, call := range message.ToolCalls {
		blocks = append(blocks, messageBlock{
			Kind:     blockToolCall,
			Text:     strings.TrimSpace(call.Arguments),
			Language: "json",
			ToolCall: call,
		})
	}

	content := normalizeNewlines(message.Content)
	if strings.TrimSpace(content) == "" {
		if message.Role == provider.RoleTool {
			return append(blocks, messageBlock{Kind: blockToolResult, Text: ""})
		}
		return blocks
	}

	parsed := parseContentBlocks(content)
	if message.Role == provider.RoleTool {
		for _, block := range parsed {
			block.Kind = blockToolResult
			blocks = append(blocks, block)
		}
		return blocks
	}

	return append(blocks, parsed...)
}

func parseContentBlocks(content string) []messageBlock {
	content = normalizeNewlines(content)
	lines := strings.Split(content, "\n")
	blocks := make([]messageBlock, 0, 4)

	var paragraphLines []string
	flushParagraph := func() {
		if len(paragraphLines) == 0 {
			return
		}
		text := strings.Trim(strings.Join(paragraphLines, "\n"), "\n")
		if strings.TrimSpace(text) != "" {
			blocks = append(blocks, messageBlock{
				Kind: blockParagraph,
				Text: text,
			})
		}
		paragraphLines = paragraphLines[:0]
	}

	inCode := false
	codeLanguage := ""
	codeLines := make([]string, 0, 8)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				blocks = append(blocks, messageBlock{
					Kind:     blockCode,
					Text:     strings.TrimRight(strings.Join(codeLines, "\n"), "\n"),
					Language: codeLanguage,
				})
				codeLines = codeLines[:0]
				codeLanguage = ""
				inCode = false
				continue
			}

			flushParagraph()
			inCode = true
			codeLanguage = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			continue
		}

		if inCode {
			codeLines = append(codeLines, line)
			continue
		}

		paragraphLines = append(paragraphLines, line)
	}

	if inCode {
		blocks = append(blocks, messageBlock{
			Kind:     blockCode,
			Text:     strings.TrimRight(strings.Join(codeLines, "\n"), "\n"),
			Language: codeLanguage,
		})
	}

	flushParagraph()
	if len(blocks) == 0 {
		blocks = append(blocks, messageBlock{Kind: blockParagraph, Text: strings.TrimSpace(content)})
	}
	return blocks
}

func normalizeNewlines(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return value
}

func wrapText(value string, width int) []string {
	if width <= 0 {
		return []string{""}
	}

	value = normalizeNewlines(value)
	lines := strings.Split(value, "\n")
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			wrapped = append(wrapped, "")
			continue
		}
		part := runewidth.Wrap(line, width)
		wrapped = append(wrapped, strings.Split(part, "\n")...)
	}
	if len(wrapped) == 0 {
		return []string{""}
	}
	return wrapped
}

func formatToolArguments(arguments string) string {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return "{}"
	}

	var buffer bytes.Buffer
	if json.Valid([]byte(trimmed)) && json.Indent(&buffer, []byte(trimmed), "", "  ") == nil {
		return buffer.String()
	}
	return trimmed
}

func filterSessions(
	sessions []runtime.SessionSummary,
	activeSessionID string,
	streaming map[string]string,
	activeTools map[string]activeTool,
	query string,
) []filteredSession {
	needle := strings.ToLower(strings.TrimSpace(query))
	filtered := make([]filteredSession, 0, len(sessions))

	for _, session := range sessions {
		title := strings.ToLower(session.Title)
		if needle != "" && !strings.Contains(title, needle) {
			continue
		}

		entry := filteredSession{
			Summary:         session,
			IsActive:        session.ID == activeSessionID,
			HasFreshOutput:  strings.TrimSpace(streaming[session.ID]) != "",
			HasRunningTools: hasActiveToolForSession(activeTools, session.ID),
		}
		entry.IsBusy = entry.HasFreshOutput || entry.HasRunningTools
		filtered = append(filtered, entry)
	}

	return filtered
}

func hasActiveToolForSession(activeTools map[string]activeTool, sessionID string) bool {
	for _, toolState := range activeTools {
		if toolState.SessionID == sessionID {
			return true
		}
	}
	return false
}

func nextCodeBlockIndex(blocks []renderedCodeBlock, current, delta int) int {
	if len(blocks) == 0 {
		return -1
	}
	if current < 0 || current >= len(blocks) {
		if delta < 0 {
			return len(blocks) - 1
		}
		return 0
	}

	next := current + delta
	if next < 0 {
		return 0
	}
	if next >= len(blocks) {
		return len(blocks) - 1
	}
	return next
}

func copyTargets(
	conversation renderedConversation,
	selectedMessage int,
	selectedCodeBlock int,
) (messageText string, codeText string, okMessage bool, okCode bool) {
	if selectedCodeBlock >= 0 && selectedCodeBlock < len(conversation.CodeBlocks) {
		codeText = conversation.CodeBlocks[selectedCodeBlock].Content
		okCode = true
		selectedMessage = conversation.CodeBlocks[selectedCodeBlock].MessageIndex
	}

	if selectedMessage >= 0 && selectedMessage < len(conversation.Messages) {
		messageText = conversation.Messages[selectedMessage].CopyText
		okMessage = true
	}
	return
}
