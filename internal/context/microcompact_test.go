package context

import (
	"testing"

	"neo-code/internal/provider"
	"neo-code/internal/tools"
)

type stubMicroCompactPolicySource map[string]tools.MicroCompactPolicy

func (s stubMicroCompactPolicySource) MicroCompactPolicy(name string) tools.MicroCompactPolicy {
	if policy, ok := s[name]; ok {
		return policy
	}
	return tools.MicroCompactPolicyCompact
}

func TestMicroCompactMessagesClearsOlderCompactableToolResults(t *testing.T) {
	t.Parallel()

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "older user"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-1", Content: "old read result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-2", Content: "recent bash result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-3", Content: "latest webfetch result"},
		{Role: provider.RoleUser, Content: "latest explicit instruction"},
		{Role: provider.RoleAssistant, Content: "current working reply"},
	}

	got := microCompactMessages(messages)
	if len(got) != len(messages) {
		t.Fatalf("expected message count to stay unchanged, got %d want %d", len(got), len(messages))
	}
	if got[2].Content != microCompactClearedMessage {
		t.Fatalf("expected oldest compactable tool result to be cleared, got %q", got[2].Content)
	}
	if got[4].Content != "recent bash result" {
		t.Fatalf("expected recent compactable tool result to be retained, got %q", got[4].Content)
	}
	if got[6].Content != "latest webfetch result" {
		t.Fatalf("expected latest compactable tool result to be retained, got %q", got[6].Content)
	}
	if messages[2].Content != "old read result" {
		t.Fatalf("expected original slice to remain unchanged, got %q", messages[2].Content)
	}
}

func TestMicroCompactMessagesHandlesEmptyAndInvalidSpanInputs(t *testing.T) {
	t.Parallel()

	if got := microCompactMessages(nil); got != nil {
		t.Fatalf("expected nil input to remain nil, got %+v", got)
	}

	assistantOnly := []provider.Message{
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "", Name: "bash", Arguments: "{}"},
			},
		},
	}
	got := microCompactMessagesWithPolicies(assistantOnly, stubMicroCompactPolicySource{})
	if len(got) != 1 || len(got[0].ToolCalls) != 1 {
		t.Fatalf("expected invalid tool call id path to keep message untouched, got %+v", got)
	}
}

func TestMicroCompactMessagesKeepsProtectedTailUntouched(t *testing.T) {
	t.Parallel()

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "older user"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-0", Name: "filesystem_grep", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-0", Content: "old grep result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-1", Content: "recent read result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-2", Content: "recent bash result"},
		{Role: provider.RoleUser, Content: "latest explicit instruction"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-3", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-3", Content: "tail bash result"},
	}

	got := microCompactMessages(messages)
	if got[2].Content != microCompactClearedMessage {
		t.Fatalf("expected old tool result before protected tail to be cleared, got %q", got[2].Content)
	}
	if got[4].Content != "recent read result" {
		t.Fatalf("expected recent tool result before protected tail to remain, got %q", got[4].Content)
	}
	if got[6].Content != "recent bash result" {
		t.Fatalf("expected second recent tool result before protected tail to remain, got %q", got[6].Content)
	}
	if got[9].Content != "tail bash result" {
		t.Fatalf("expected protected tail tool result to remain, got %q", got[9].Content)
	}
}

func TestMicroCompactMessagesKeepsPreservedToolsErrorsAndOrphans(t *testing.T) {
	t.Parallel()

	messages := []provider.Message{
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "custom_tool", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-1", Content: "custom result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-2", Name: "filesystem_edit", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-2", Content: "edit failed", IsError: true},
		{Role: provider.RoleTool, ToolCallID: "orphan", Content: "orphan result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-3", Name: "filesystem_write_file", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-3", Content: microCompactClearedMessage},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-4", Name: "filesystem_grep", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-4", Content: ""},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{
		"custom_tool": tools.MicroCompactPolicyPreserveHistory,
	})
	if got[1].Content != "custom result" {
		t.Fatalf("expected preserved tool result to remain, got %q", got[1].Content)
	}
	if got[3].Content != "edit failed" {
		t.Fatalf("expected error tool result to remain, got %q", got[3].Content)
	}
	if got[4].Content != "orphan result" {
		t.Fatalf("expected orphan tool result to remain, got %q", got[4].Content)
	}
	if got[6].Content != microCompactClearedMessage {
		t.Fatalf("expected already cleared content to remain unchanged, got %q", got[6].Content)
	}
	if got[8].Content != "" {
		t.Fatalf("expected empty tool result to remain empty, got %q", got[8].Content)
	}
}

func TestMicroCompactMessagesClearsOnlyNonPreservedResultsInMixedToolSpan(t *testing.T) {
	t.Parallel()

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "older user"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
				{ID: "call-2", Name: "custom_tool", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-1", Content: "read result"},
		{Role: provider.RoleTool, ToolCallID: "call-2", Content: "custom result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-3", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-3", Content: "recent bash result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-4", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-4", Content: "latest webfetch result"},
		{Role: provider.RoleUser, Content: "latest explicit instruction"},
		{Role: provider.RoleAssistant, Content: "current reply"},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{
		"custom_tool": tools.MicroCompactPolicyPreserveHistory,
	})
	if got[2].Content != microCompactClearedMessage {
		t.Fatalf("expected default compactable tool result to be cleared, got %q", got[2].Content)
	}
	if got[3].Content != "custom result" {
		t.Fatalf("expected preserved tool result in mixed span to remain, got %q", got[3].Content)
	}
	if len(got[1].ToolCalls) != 2 {
		t.Fatalf("expected assistant tool call metadata to remain intact, got %+v", got[1].ToolCalls)
	}
}

func TestMicroCompactMessagesTreatsNewToolsAsCompactableByDefault(t *testing.T) {
	t.Parallel()

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "older user"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "repo_search", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-1", Content: "old repo search result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-2", Content: "recent bash result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-3", Content: "latest webfetch result"},
		{Role: provider.RoleUser, Content: "latest explicit instruction"},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{})
	if got[2].Content != microCompactClearedMessage {
		t.Fatalf("expected new tool result to be compacted by default, got %q", got[2].Content)
	}
}

func TestMicroCompactMessagesSkipsEmptyRecentSpansWhenCountingRetainedBudget(t *testing.T) {
	t.Parallel()

	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "older user"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-1", Content: "older read result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-2", Name: "filesystem_grep", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-2", Content: "middle grep result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-3", Name: "filesystem_edit", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-3", Content: "near edit result"},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-4", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-4", Content: "", IsError: true},
		{
			Role: provider.RoleAssistant,
			ToolCalls: []provider.ToolCall{
				{ID: "call-5", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: provider.RoleTool, ToolCallID: "call-5", Content: ""},
		{Role: provider.RoleUser, Content: "latest explicit instruction"},
		{Role: provider.RoleAssistant, Content: "current reply"},
	}

	got := microCompactMessages(messages)
	if got[2].Content != microCompactClearedMessage {
		t.Fatalf("expected oldest valid tool result to be cleared, got %q", got[2].Content)
	}
	if got[4].Content != "middle grep result" {
		t.Fatalf("expected middle valid tool result to remain, got %q", got[4].Content)
	}
	if got[6].Content != "near edit result" {
		t.Fatalf("expected nearer valid tool result to remain, got %q", got[6].Content)
	}
	if got[8].Content != "" {
		t.Fatalf("expected error/empty tool result to remain unchanged, got %q", got[8].Content)
	}
	if got[10].Content != "" {
		t.Fatalf("expected empty recent tool result to remain unchanged, got %q", got[10].Content)
	}
}

func TestMicroCompactMessagesSkipsToolMessagesWhenCompactableIDsMissing(t *testing.T) {
	t.Parallel()

	messages := []provider.Message{
		{Role: provider.RoleTool, ToolCallID: "orphan", Content: "orphan result"},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{})
	if got[0].Content != "orphan result" {
		t.Fatalf("expected orphan tool result to remain, got %q", got[0].Content)
	}
}
