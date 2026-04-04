package context

import (
	stdcontext "context"
	"fmt"
	"testing"

	"neo-code/internal/provider"
)

func TestDefaultBuilderBuildDeepClonesToolCalls(t *testing.T) {
	t.Parallel()

	builder := NewBuilder()
	input := BuildInput{
		Messages: []provider.Message{
			{
				Role:    provider.RoleAssistant,
				Content: "call tool",
				ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "filesystem_edit", Arguments: `{"path":"main.go"}`},
				},
			},
		},
		Metadata: testMetadata(t.TempDir()),
	}

	got, err := builder.Build(stdcontext.Background(), input)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(got.Messages) != 1 || len(got.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected cloned assistant tool call message, got %+v", got.Messages)
	}

	got.Messages[0].Content = "mutated"
	got.Messages[0].ToolCalls[0].Arguments = `{"path":"mutated.go"}`

	if input.Messages[0].Content != "call tool" {
		t.Fatalf("expected input content to stay unchanged, got %q", input.Messages[0].Content)
	}
	if input.Messages[0].ToolCalls[0].Arguments != `{"path":"main.go"}` {
		t.Fatalf("expected input tool calls to stay unchanged, got %+v", input.Messages[0].ToolCalls)
	}
}

func TestTrimMessagesPreservesMultipleToolResults(t *testing.T) {
	t.Parallel()

	messages := make([]provider.Message, 0, maxContextTurns+6)
	for i := 0; i < maxContextTurns-1; i++ {
		messages = append(messages, provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("u-%d", i)})
	}
	messages = append(messages,
		provider.Message{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"a.go"}`},
				{ID: "call-2", Name: "filesystem_read_file", Arguments: `{"path":"b.go"}`},
			},
		},
		provider.Message{Role: provider.RoleUser, Content: "interleaved"},
		provider.Message{Role: provider.RoleTool, ToolCallID: "call-1", Content: "tool-1"},
		provider.Message{Role: provider.RoleAssistant, Content: "thinking"},
		provider.Message{Role: provider.RoleTool, ToolCallID: "call-2", Content: "tool-2"},
	)

	trimmed := trimMessages(messages)

	var foundAssistant bool
	foundTools := map[string]bool{}
	for _, message := range trimmed {
		if message.Role == provider.RoleAssistant && len(message.ToolCalls) == 2 {
			foundAssistant = true
		}
		if message.Role == provider.RoleTool {
			foundTools[message.ToolCallID] = true
		}
	}

	if !foundAssistant {
		t.Fatalf("expected assistant tool call message to remain, got %+v", trimmed)
	}
	if !foundTools["call-1"] || !foundTools["call-2"] {
		t.Fatalf("expected both tool results to remain, got %+v", trimmed)
	}
}

func TestTrimMessagesDropsDanglingToolResultsWhenToolCallFallsOutOfWindow(t *testing.T) {
	t.Parallel()

	messages := make([]provider.Message, 0, maxContextTurns+6)
	for i := 0; i < 4; i++ {
		messages = append(messages, provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("old-%d", i)})
	}
	messages = append(messages, provider.Message{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{
			{ID: "call-1", Name: "filesystem_edit", Arguments: `{"path":"main.go"}`},
		},
	})
	for i := 0; i < 6; i++ {
		messages = append(messages, provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("mid-%d", i)})
	}
	messages = append(messages, provider.Message{Role: provider.RoleTool, ToolCallID: "call-1", Content: "tool-result"})
	for i := 0; i < 4; i++ {
		messages = append(messages, provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("tail-%d", i)})
	}

	trimmed := trimMessages(messages)

	for _, message := range trimmed {
		if message.Role == provider.RoleTool && message.ToolCallID == "call-1" {
			t.Fatalf("expected dangling tool result to be removed, got %+v", trimmed)
		}
		if message.Role == provider.RoleAssistant && len(message.ToolCalls) > 0 {
			t.Fatalf("expected matching assistant tool call to be removed with the tool result, got %+v", trimmed)
		}
	}
}

func TestRetainMessageClosureBackfillsAssistantAndSiblingToolResults(t *testing.T) {
	t.Parallel()

	messages := []provider.Message{
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: `{"path":"a.go"}`},
				{ID: "call-2", Name: "filesystem_read_file", Arguments: `{"path":"b.go"}`},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-1", Content: "tool-1"},
		{Role: provider.RoleTool, ToolCallID: "call-2", Content: "tool-2"},
	}

	index := buildMessageRelationIndex(messages)
	retained := retainMessageClosure(messages, index, []int{1})

	if len(retained) != 3 {
		t.Fatalf("expected closure to retain assistant and sibling tools, got %+v", retained)
	}
	if retained[0].Role != provider.RoleAssistant {
		t.Fatalf("expected assistant to be restored first, got %+v", retained)
	}
	if retained[1].ToolCallID != "call-1" || retained[2].ToolCallID != "call-2" {
		t.Fatalf("expected both tool results to remain, got %+v", retained)
	}
}

func TestTrimMessagesHandlesSparseAndOrphanInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []provider.Message
		wantLen  int
		wantRole string
	}{
		{
			name:    "nil input stays nil",
			input:   nil,
			wantLen: 0,
		},
		{
			name: "single user message survives",
			input: []provider.Message{
				{Role: provider.RoleUser, Content: "hello"},
			},
			wantLen:  1,
			wantRole: provider.RoleUser,
		},
		{
			name: "orphan tool message stays stable",
			input: []provider.Message{
				{Role: provider.RoleTool, ToolCallID: "missing", Content: "tool-result"},
			},
			wantLen:  1,
			wantRole: provider.RoleTool,
		},
		{
			name: "tool before assistant does not panic",
			input: []provider.Message{
				{Role: provider.RoleTool, ToolCallID: "call-1", Content: "tool-result"},
				{
					Role: provider.RoleAssistant,
					ToolCalls: []provider.ToolCall{
						{ID: "call-1", Name: "filesystem_edit", Arguments: `{"path":"main.go"}`},
					},
				},
			},
			wantLen:  2,
			wantRole: provider.RoleTool,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			trimmed := trimMessages(tt.input)
			if len(trimmed) != tt.wantLen {
				t.Fatalf("expected len %d, got %d (%+v)", tt.wantLen, len(trimmed), trimmed)
			}
			if tt.wantLen > 0 && trimmed[0].Role != tt.wantRole {
				t.Fatalf("expected first role %q, got %+v", tt.wantRole, trimmed)
			}
		})
	}
}
