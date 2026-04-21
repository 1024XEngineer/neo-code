package context

import (
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
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

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old read result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest webfetch result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("current working reply")}},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{}, 2, nil, nil)
	if len(got) != len(messages) {
		t.Fatalf("expected message count to stay unchanged, got %d want %d", len(got), len(messages))
	}
	if renderDisplayParts(got[2].Parts) != microCompactClearedMessage {
		t.Fatalf("expected oldest compactable tool result to be cleared, got %q", renderDisplayParts(got[2].Parts))
	}
	if renderDisplayParts(got[4].Parts) != "recent bash result" {
		t.Fatalf("expected recent compactable tool result to be retained, got %q", renderDisplayParts(got[4].Parts))
	}
	if renderDisplayParts(got[6].Parts) != "latest webfetch result" {
		t.Fatalf("expected latest compactable tool result to be retained, got %q", renderDisplayParts(got[6].Parts))
	}
	if renderDisplayParts(messages[2].Parts) != "old read result" {
		t.Fatalf("expected original slice to remain unchanged, got %q", renderDisplayParts(messages[2].Parts))
	}
}

func TestMicroCompactMessagesHandlesEmptyAndInvalidSpanInputs(t *testing.T) {
	t.Parallel()

	if got := microCompactMessages(nil); got != nil {
		t.Fatalf("expected nil input to remain nil, got %+v", got)
	}

	assistantOnly := []providertypes.Message{
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "", Name: "bash", Arguments: "{}"},
			},
		},
	}
	got := microCompactMessagesWithPolicies(assistantOnly, stubMicroCompactPolicySource{}, 0, nil, nil)
	if len(got) != 1 || len(got[0].ToolCalls) != 1 {
		t.Fatalf("expected invalid tool call id path to keep message untouched, got %+v", got)
	}
}

func TestMicroCompactMessagesKeepsProtectedTailUntouched(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-0", Name: "filesystem_grep", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-0", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old grep result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent read result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("tail bash result")}},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{}, 2, nil, nil)
	if renderDisplayParts(got[2].Parts) != microCompactClearedMessage {
		t.Fatalf("expected old tool result before protected tail to be cleared, got %q", renderDisplayParts(got[2].Parts))
	}
	if renderDisplayParts(got[4].Parts) != "recent read result" {
		t.Fatalf("expected recent tool result before protected tail to remain, got %q", renderDisplayParts(got[4].Parts))
	}
	if renderDisplayParts(got[6].Parts) != "recent bash result" {
		t.Fatalf("expected second recent tool result before protected tail to remain, got %q", renderDisplayParts(got[6].Parts))
	}
	if renderDisplayParts(got[9].Parts) != "tail bash result" {
		t.Fatalf("expected protected tail tool result to remain, got %q", renderDisplayParts(got[9].Parts))
	}
}

func TestMicroCompactMessagesKeepsPreservedToolsErrorsAndOrphans(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "custom_tool", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("custom result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "filesystem_edit", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("edit failed")}, IsError: true},
		{Role: providertypes.RoleTool, ToolCallID: "orphan", Parts: []providertypes.ContentPart{providertypes.NewTextPart("orphan result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "filesystem_write_file", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart(microCompactClearedMessage)}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-4", Name: "filesystem_grep", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-4", Parts: []providertypes.ContentPart{providertypes.NewTextPart("")}},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{
		"custom_tool": tools.MicroCompactPolicyPreserveHistory,
	}, 2, nil, nil)
	if renderDisplayParts(got[1].Parts) != "custom result" {
		t.Fatalf("expected preserved tool result to remain, got %q", renderDisplayParts(got[1].Parts))
	}
	if renderDisplayParts(got[3].Parts) != "edit failed" {
		t.Fatalf("expected error tool result to remain, got %q", renderDisplayParts(got[3].Parts))
	}
	if renderDisplayParts(got[4].Parts) != "orphan result" {
		t.Fatalf("expected orphan tool result to remain, got %q", renderDisplayParts(got[4].Parts))
	}
	if renderDisplayParts(got[6].Parts) != microCompactClearedMessage {
		t.Fatalf("expected already cleared content to remain unchanged, got %q", renderDisplayParts(got[6].Parts))
	}
	if renderDisplayParts(got[8].Parts) != "" {
		t.Fatalf("expected empty tool result to remain empty, got %q", renderDisplayParts(got[8].Parts))
	}
}

func TestMicroCompactMessagesClearsOnlyNonPreservedResultsInMixedToolSpan(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
				{ID: "call-2", Name: "custom_tool", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("read result")}},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("custom result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-4", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-4", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest webfetch result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("current reply")}},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{
		"custom_tool": tools.MicroCompactPolicyPreserveHistory,
	}, 2, nil, nil)
	if renderDisplayParts(got[2].Parts) != microCompactClearedMessage {
		t.Fatalf("expected default compactable tool result to be cleared, got %q", renderDisplayParts(got[2].Parts))
	}
	if renderDisplayParts(got[3].Parts) != "custom result" {
		t.Fatalf("expected preserved tool result in mixed span to remain, got %q", renderDisplayParts(got[3].Parts))
	}
	if len(got[1].ToolCalls) != 2 {
		t.Fatalf("expected assistant tool call metadata to remain intact, got %+v", got[1].ToolCalls)
	}
}

func TestMicroCompactMessagesTreatsNewToolsAsCompactableByDefault(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "repo_search", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old repo search result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest webfetch result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{}, 2, nil, nil)
	if renderDisplayParts(got[2].Parts) != microCompactClearedMessage {
		t.Fatalf("expected new tool result to be compacted by default, got %q", renderDisplayParts(got[2].Parts))
	}
}

func TestMicroCompactMessagesSkipsEmptyRecentSpansWhenCountingRetainedBudget(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("older read result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "filesystem_grep", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("middle grep result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "filesystem_edit", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("near edit result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-4", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-4", Parts: []providertypes.ContentPart{providertypes.NewTextPart("")}, IsError: true},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-5", Name: "webfetch", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-5", Parts: []providertypes.ContentPart{providertypes.NewTextPart("")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("current reply")}},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{}, 2, nil, nil)
	if renderDisplayParts(got[2].Parts) != microCompactClearedMessage {
		t.Fatalf("expected oldest valid tool result to be cleared, got %q", renderDisplayParts(got[2].Parts))
	}
	if renderDisplayParts(got[4].Parts) != "middle grep result" {
		t.Fatalf("expected middle valid tool result to remain, got %q", renderDisplayParts(got[4].Parts))
	}
	if renderDisplayParts(got[6].Parts) != "near edit result" {
		t.Fatalf("expected nearer valid tool result to remain, got %q", renderDisplayParts(got[6].Parts))
	}
	if renderDisplayParts(got[8].Parts) != "" {
		t.Fatalf("expected error/empty tool result to remain unchanged, got %q", renderDisplayParts(got[8].Parts))
	}
	if renderDisplayParts(got[10].Parts) != "" {
		t.Fatalf("expected empty recent tool result to remain unchanged, got %q", renderDisplayParts(got[10].Parts))
	}
}

func TestMicroCompactMessagesSkipsToolMessagesWhenCompactableIDsMissing(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleTool, ToolCallID: "orphan", Parts: []providertypes.ContentPart{providertypes.NewTextPart("orphan result")}},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{}, 0, nil, nil)
	if renderDisplayParts(got[0].Parts) != "orphan result" {
		t.Fatalf("expected orphan tool result to remain, got %q", renderDisplayParts(got[0].Parts))
	}
}

// TestMicroCompactPinnedResultNotCompacted 验证被 pin checker 钉住的工具结果不会被压缩。
func TestMicroCompactPinnedResultNotCompacted(t *testing.T) {
	t.Parallel()

	stubPin := stubMicroCompactPinChecker{
		"filesystem_write_file": map[string]bool{"README.md": true},
	}

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_write_file", Arguments: `{"path":"README.md"}`},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("README content")}, ToolMetadata: map[string]string{"path": "/project/README.md"}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest bash result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("current reply")}},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{}, 1, nil, stubPin)
	if renderDisplayParts(got[2].Parts) != "README content" {
		t.Fatalf("expected pinned README result to be preserved, got %q", renderDisplayParts(got[2].Parts))
	}
}

// TestMicroCompactMixedPinnedAndNonPinned 验证同一 span 中钉住和非钉住结果混合时仅压缩非钉住的。
func TestMicroCompactMixedPinnedAndNonPinned(t *testing.T) {
	t.Parallel()

	stubPin := stubMicroCompactPinChecker{
		"filesystem_write_file": map[string]bool{"README.md": true},
	}

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "filesystem_write_file", Arguments: `{"path":"README.md"}`},
				{ID: "call-2", Name: "filesystem_write_file", Arguments: `{"path":"main.go"}`},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("README content")}, ToolMetadata: map[string]string{"path": "/project/README.md"}},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("main.go content")}, ToolMetadata: map[string]string{"path": "/project/main.go"}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("reply")}},
	}

	got := microCompactMessagesWithPolicies(messages, stubMicroCompactPolicySource{}, 1, nil, stubPin)
	if renderDisplayParts(got[2].Parts) != "README content" {
		t.Fatalf("expected pinned README result preserved, got %q", renderDisplayParts(got[2].Parts))
	}
	if renderDisplayParts(got[3].Parts) != microCompactClearedMessage {
		t.Fatalf("expected non-pinned main.go result to be cleared, got %q", renderDisplayParts(got[3].Parts))
	}
}

// stubMicroCompactPinChecker 实现 MicroCompactPinChecker，用于测试。
type stubMicroCompactPinChecker map[string]map[string]bool

func (s stubMicroCompactPinChecker) ShouldPin(toolName string, metadata map[string]string) bool {
	paths, ok := s[toolName]
	if !ok {
		return false
	}
	path := metadata["path"]
	if path == "" {
		path = metadata["relative_path"]
	}
	for pinnedPath, shouldPin := range paths {
		if shouldPin && strings.Contains(path, pinnedPath) {
			return true
		}
	}
	return false
}
