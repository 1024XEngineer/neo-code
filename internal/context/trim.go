package context

import "neo-code/internal/provider"

const maxContextTurns = 10

func trimMessages(messages []provider.Message) []provider.Message {
	if len(messages) <= maxContextTurns {
		return append([]provider.Message(nil), messages...)
	}

	type span struct {
		start int
		end   int
	}

	spans := make([]span, 0, len(messages))
	for i := 0; i < len(messages); {
		start := i
		i++

		if messages[start].Role == provider.RoleAssistant && len(messages[start].ToolCalls) > 0 {
			for i < len(messages) && messages[i].Role == provider.RoleTool {
				i++
			}
		}

		spans = append(spans, span{start: start, end: i})
	}

	if len(spans) <= maxContextTurns {
		return append([]provider.Message(nil), messages...)
	}

	start := spans[len(spans)-maxContextTurns].start
	return append([]provider.Message(nil), messages[start:]...)
}
