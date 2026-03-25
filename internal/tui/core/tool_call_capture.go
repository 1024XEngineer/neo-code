package core

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"go-llm-demo/internal/tui/services"
)

type toolCallCapture struct {
	Call            services.ToolCall
	Start           int
	End             int
	CleanedResponse string
}

func captureToolCallFromAssistantText(content string) (toolCallCapture, bool) {
	candidate, ok := findLastToolCallCandidate(content)
	if !ok {
		return toolCallCapture{}, false
	}

	candidate.CleanedResponse = sanitizeAssistantToolCallContent(content, candidate.Start, candidate.End)
	return candidate, true
}

func findLastToolCallCandidate(content string) (toolCallCapture, bool) {
	var best toolCallCapture
	found := false

	for start := range content {
		if content[start] != '{' {
			continue
		}

		decoder := json.NewDecoder(strings.NewReader(content[start:]))
		decoder.UseNumber()

		var raw map[string]json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			continue
		}

		end := start + int(decoder.InputOffset())
		call, ok := parseToolCallCandidate(raw)
		if !ok || !hasOnlyIgnorableToolCallSuffix(content[end:]) {
			continue
		}

		best = toolCallCapture{
			Call:  call,
			Start: start,
			End:   end,
		}
		found = true
	}

	return best, found
}

func parseToolCallCandidate(raw map[string]json.RawMessage) (services.ToolCall, bool) {
	toolRaw, ok := raw["tool"]
	if !ok {
		return services.ToolCall{}, false
	}

	var toolName string
	if err := json.Unmarshal(toolRaw, &toolName); err != nil {
		return services.ToolCall{}, false
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return services.ToolCall{}, false
	}

	params := map[string]interface{}{}
	if paramsRaw, ok := raw["params"]; ok {
		trimmed := strings.TrimSpace(string(paramsRaw))
		if trimmed != "" && trimmed != "null" {
			if err := json.Unmarshal(paramsRaw, &params); err != nil {
				return services.ToolCall{}, false
			}
		}
	}

	return services.ToolCall{
		Tool:   toolName,
		Params: params,
	}, true
}

func hasOnlyIgnorableToolCallSuffix(suffix string) bool {
	remaining := strings.TrimSpace(suffix)
	for remaining != "" {
		switch {
		case strings.HasPrefix(remaining, "```"):
			remaining = strings.TrimSpace(remaining[3:])
			continue
		case strings.HasPrefix(remaining, "</"):
			tagEnd := strings.Index(remaining, ">")
			if tagEnd <= 2 {
				return false
			}
			tagName := remaining[2:tagEnd]
			if !isIdentifier(tagName) {
				return false
			}
			remaining = strings.TrimSpace(remaining[tagEnd+1:])
			continue
		}

		r, size := utf8.DecodeRuneInString(remaining)
		if size == 0 {
			break
		}
		if strings.ContainsRune("`.,;:!?)]}\uFF0C\u3002\uFF1B\uFF1A\uFF01", r) {
			remaining = strings.TrimSpace(remaining[size:])
			continue
		}
		return false
	}
	return true
}

func sanitizeAssistantToolCallContent(content string, start, end int) string {
	if start < 0 || end < start || end > len(content) {
		return strings.TrimSpace(stripThinkBlocks(content))
	}

	cleaned := content[:start] + content[end:]
	cleaned = stripThinkBlocks(cleaned)
	cleaned = stripFenceOnlyLines(cleaned)
	return collapseBlankLines(cleaned)
}

func stripThinkBlocks(content string) string {
	lower := strings.ToLower(content)
	openTag := "<think>"
	closeTag := "</think>"

	var builder strings.Builder
	searchStart := 0
	for searchStart < len(content) {
		openIdx := strings.Index(lower[searchStart:], openTag)
		if openIdx < 0 {
			builder.WriteString(content[searchStart:])
			break
		}
		openIdx += searchStart
		builder.WriteString(content[searchStart:openIdx])

		closeSearchStart := openIdx + len(openTag)
		closeIdx := strings.Index(lower[closeSearchStart:], closeTag)
		if closeIdx < 0 {
			break
		}
		searchStart = closeSearchStart + closeIdx + len(closeTag)
	}

	return builder.String()
}

func stripFenceOnlyLines(content string) string {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if isFenceOnlyLine(strings.TrimSpace(line)) {
			continue
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
	}
	return strings.Join(kept, "\n")
}

func isFenceOnlyLine(line string) bool {
	if !strings.HasPrefix(line, "```") {
		return false
	}

	lang := strings.TrimSpace(strings.TrimPrefix(line, "```"))
	return lang == "" || isIdentifier(lang)
}

func collapseBlankLines(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) == 0 {
		return ""
	}

	collapsed := make([]string, 0, len(lines))
	prevBlank := false
	for _, line := range lines {
		blank := strings.TrimSpace(line) == ""
		if blank {
			if prevBlank {
				continue
			}
			collapsed = append(collapsed, "")
			prevBlank = true
			continue
		}

		collapsed = append(collapsed, line)
		prevBlank = false
	}

	return strings.TrimSpace(strings.Join(collapsed, "\n"))
}

func isIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}

	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '_', '-', '+', '.':
			continue
		default:
			return false
		}
	}
	return true
}
