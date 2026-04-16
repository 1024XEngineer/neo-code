package context

import (
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/tools"
)

// stubMicroCompactSummarizerSource 实现 MicroCompactSummarizerSource，用于测试。
type stubMicroCompactSummarizerSource map[string]tools.ContentSummarizer

func (s stubMicroCompactSummarizerSource) MicroCompactSummarizer(name string) tools.ContentSummarizer {
	return s[name]
}

// TestMicroCompactWithSummarizerProducesSummary 验证注册 summarizer 的工具生成摘要而非清除占位。
func TestMicroCompactWithSummarizerProducesSummary(t *testing.T) {
	t.Parallel()

	bashSummarizer := func(content string, metadata map[string]string, isError bool) string {
		return "[summary] bash: " + content
	}

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old bash result")}},
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

	got := microCompactMessagesWithPolicies(
		messages,
		stubMicroCompactPolicySource{},
		0,
		stubMicroCompactSummarizerSource{"bash": bashSummarizer},
	)

	if renderDisplayParts(got[2].Parts) == microCompactClearedMessage {
		t.Fatalf("expected summarized content for old bash result, got cleared placeholder")
	}
	if !strings.Contains(renderDisplayParts(got[2].Parts), "[summary] bash:") {
		t.Fatalf("expected summary prefix, got %q", renderDisplayParts(got[2].Parts))
	}
	if renderDisplayParts(got[4].Parts) != "recent bash result" {
		t.Fatalf("expected recent bash result retained, got %q", renderDisplayParts(got[4].Parts))
	}
	if renderDisplayParts(got[6].Parts) != "latest bash result" {
		t.Fatalf("expected latest bash result retained, got %q", renderDisplayParts(got[6].Parts))
	}
	// 原始切片不被修改
	if renderDisplayParts(messages[2].Parts) != "old bash result" {
		t.Fatalf("expected original slice unchanged, got %q", renderDisplayParts(messages[2].Parts))
	}
}

// TestMicroCompactWithoutSummarizerFallsBackToClear 验证未注册 summarizer 的工具仍使用清除占位。
func TestMicroCompactWithoutSummarizerFallsBackToClear(t *testing.T) {
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
				{ID: "call-3", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest bash result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
	}

	// 只为 bash 注册 summarizer，read_file 没有
	got := microCompactMessagesWithPolicies(
		messages,
		stubMicroCompactPolicySource{},
		0,
		stubMicroCompactSummarizerSource{
			"bash": func(content string, metadata map[string]string, isError bool) string {
				return "[summary] bash: " + content
			},
		},
	)

	// read_file 没有 summarizer，应回退到清除
	if renderDisplayParts(got[2].Parts) != microCompactClearedMessage {
		t.Fatalf("expected cleared placeholder for read_file without summarizer, got %q", renderDisplayParts(got[2].Parts))
	}
}

// TestMicroCompactMixedSpanWithSummarizer 验证混合工具 span 中部分有摘要、部分清除。
func TestMicroCompactMixedSpanWithSummarizer(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: "{}"},
				{ID: "call-2", Name: "filesystem_read_file", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("bash output")}},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("read output")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent bash")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-4", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-4", Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest bash")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("reply")}},
	}

	got := microCompactMessagesWithPolicies(
		messages,
		stubMicroCompactPolicySource{},
		0,
		stubMicroCompactSummarizerSource{
			"bash": func(content string, metadata map[string]string, isError bool) string {
				return "[summary] " + content
			},
		},
	)

	// call-1 bash 在旧 span，有 summarizer，应生成摘要
	if !strings.Contains(renderDisplayParts(got[2].Parts), "[summary]") {
		t.Fatalf("expected bash summary in old span, got %q", renderDisplayParts(got[2].Parts))
	}
	// call-2 read_file 在旧 span，没有 summarizer，应清除
	if renderDisplayParts(got[3].Parts) != microCompactClearedMessage {
		t.Fatalf("expected read_file cleared in old span, got %q", renderDisplayParts(got[3].Parts))
	}
}

// TestMicroCompactSummarizerReturnsEmptyFallsBackToClear 验证 summarizer 返回空字符串时回退到清除。
func TestMicroCompactSummarizerReturnsEmptyFallsBackToClear(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("older user")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("old result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-2", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("middle result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call-3", Name: "bash", Arguments: "{}"},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call-3", Parts: []providertypes.ContentPart{providertypes.NewTextPart("recent result")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("latest explicit instruction")}},
	}

	got := microCompactMessagesWithPolicies(
		messages,
		stubMicroCompactPolicySource{},
		0,
		stubMicroCompactSummarizerSource{
			"bash": func(content string, metadata map[string]string, isError bool) string {
				return "" // 返回空
			},
		},
	)

	if renderDisplayParts(got[2].Parts) != microCompactClearedMessage {
		t.Fatalf("expected cleared fallback when summarizer returns empty, got %q", renderDisplayParts(got[2].Parts))
	}
}

// TestSummarizeOrClearWithNilSummarizers 验证 nil summarizers 回退到清除。
func TestSummarizeOrClearWithNilSummarizers(t *testing.T) {
	t.Parallel()

	got := summarizeOrClear(
		providertypes.Message{Parts: []providertypes.ContentPart{providertypes.NewTextPart("test")}},
		nil,
		nil,
	)
	if got != microCompactClearedMessage {
		t.Fatalf("expected cleared message for nil summarizers, got %q", got)
	}
}

// TestSummarizeOrClearWithToolNamesLookup 验证 toolNames map 查找工具名。
func TestSummarizeOrClearWithToolNamesLookup(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		toolNames := map[string]string{"call-2": "filesystem_read_file"}
		got := summarizeOrClear(
			providertypes.Message{ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("content")}},
			toolNames,
			stubMicroCompactSummarizerSource{
				"filesystem_read_file": func(content string, metadata map[string]string, isError bool) string {
					return "[summary] " + content
				},
			},
		)
		if !strings.Contains(got, "[summary]") {
			t.Fatalf("expected summary, got %q", got)
		}
	})

	t.Run("not_found_in_tool_names", func(t *testing.T) {
		toolNames := map[string]string{"call-1": "bash"}
		got := summarizeOrClear(
			providertypes.Message{ToolCallID: "unknown-id", Parts: []providertypes.ContentPart{providertypes.NewTextPart("content")}},
			toolNames,
			stubMicroCompactSummarizerSource{},
		)
		if got != microCompactClearedMessage {
			t.Fatalf("expected cleared for unknown tool call id, got %q", got)
		}
	})
}
