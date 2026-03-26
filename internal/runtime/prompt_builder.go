package runtime

import (
	"fmt"

	"neocode/internal/provider"
)

// PromptBuilder assembles provider-facing messages for a session.
type PromptBuilder struct {
	workdir string
}

// NewPromptBuilder constructs a prompt builder.
func NewPromptBuilder(workdir string) *PromptBuilder {
	return &PromptBuilder{workdir: workdir}
}

// Build returns the system prompt plus session history.
func (b *PromptBuilder) Build(session Session) []provider.Message {
	messages := make([]provider.Message, 0, len(session.Messages)+1)
	messages = append(messages, provider.Message{
		Role: provider.RoleSystem,
		Content: fmt.Sprintf(
			"You are NeoCode, a local coding agent inside a terminal UI. "+
				"Use tools when they materially help. Keep answers concise and grounded in tool results. "+
				"The active workdir is %s. Never assume files outside that workdir are accessible.",
			b.workdir,
		),
	})
	messages = append(messages, session.Messages...)
	return messages
}
