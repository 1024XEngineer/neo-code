package context

import (
	"strings"
	"testing"
	"unicode/utf8"

	"neo-code/internal/context/internalcompact"
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
		"test",
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
			"content",
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
			"content",
			toolNames,
			stubMicroCompactSummarizerSource{},
		)
		if got != microCompactClearedMessage {
			t.Fatalf("expected cleared for unknown tool call id, got %q", got)
		}
	})
}

// TestSummarizeOrClearSanitizesSummary 验证摘要回灌前会执行控制字符净化与长度裁剪。
func TestSummarizeOrClearSanitizesSummary(t *testing.T) {
	t.Parallel()

	raw := strings.Repeat("x", microCompactSummaryMaxRunes+50) + "\n\t\x07"
	got := summarizeOrClear(
		providertypes.Message{ToolCallID: "call-1"},
		"ignored",
		map[string]string{"call-1": "bash"},
		stubMicroCompactSummarizerSource{
			"bash": func(content string, metadata map[string]string, isError bool) string {
				return raw
			},
		},
	)

	if strings.ContainsAny(got, "\n\t\a") {
		t.Fatalf("expected control characters removed, got %q", got)
	}
	if utf8.RuneCountInString(got) > microCompactSummaryMaxRunes+3 {
		t.Fatalf("expected summary capped, got %d runes", utf8.RuneCountInString(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncated summary suffix, got %q", got)
	}
}

// TestSummarizeOrClearSanitizationEmptyFallback 验证净化后为空时会回退清理占位。
func TestSummarizeOrClearSanitizationEmptyFallback(t *testing.T) {
	t.Parallel()

	got := summarizeOrClear(
		providertypes.Message{ToolCallID: "call-1"},
		"ignored",
		map[string]string{"call-1": "bash"},
		stubMicroCompactSummarizerSource{
			"bash": func(content string, metadata map[string]string, isError bool) string {
				return "\n\t\x07 "
			},
		},
	)

	if got != microCompactClearedMessage {
		t.Fatalf("expected cleared fallback when sanitized summary is empty, got %q", got)
	}
}

// TestIsToolCallSpanBoundaries 验证 span 边界异常时返回 false。
func TestIsToolCallSpanBoundaries(t *testing.T) {
	t.Parallel()

	messages := []providertypes.Message{
		{Role: providertypes.RoleAssistant, ToolCalls: []providertypes.ToolCall{{ID: "c1", Name: "bash"}}},
	}

	if isToolCallSpan(messages, internalcompact.MessageSpan{Start: -1, End: 0}) {
		t.Fatal("expected false for negative start")
	}
	if isToolCallSpan(messages, internalcompact.MessageSpan{Start: 2, End: 3}) {
		t.Fatal("expected false for out-of-range start")
	}
}

// TestCompactableToolCallIDsEmptyInput 验证空 tool call 输入时返回 nil。
func TestCompactableToolCallIDsEmptyInput(t *testing.T) {
	t.Parallel()

	ids, names := compactableToolCallIDs(nil, nil)
	if ids != nil || names != nil {
		t.Fatalf("expected nil maps for empty input, got ids=%v names=%v", ids, names)
	}
}

// TestHasCompactableToolMessage 验证工具块可压缩消息探测逻辑。
func TestHasCompactableToolMessage(t *testing.T) {
	t.Parallel()

	span := internalcompact.MessageSpan{Start: 0, End: 3}
	ids := map[string]struct{}{"call-1": {}}

	t.Run("true_when_matching_tool_message_exists", func(t *testing.T) {
		messages := []providertypes.Message{
			{Role: providertypes.RoleAssistant, ToolCalls: []providertypes.ToolCall{{ID: "call-1", Name: "bash"}}},
			{Role: providertypes.RoleTool, ToolCallID: "call-1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("output")}},
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("u")}},
		}
		if !hasCompactableToolMessage(messages, span, ids) {
			t.Fatal("expected compactable tool message to be found")
		}
	})

	t.Run("false_when_tool_messages_are_not_compactable", func(t *testing.T) {
		messages := []providertypes.Message{
			{Role: providertypes.RoleAssistant, ToolCalls: []providertypes.ToolCall{{ID: "call-1", Name: "bash"}}},
			{Role: providertypes.RoleTool, ToolCallID: "call-1", IsError: true, Parts: []providertypes.ContentPart{providertypes.NewTextPart("error")}},
			{Role: providertypes.RoleTool, ToolCallID: "call-2", Parts: []providertypes.ContentPart{providertypes.NewTextPart("other")}},
		}
		if hasCompactableToolMessage(messages, span, ids) {
			t.Fatal("expected no compactable tool message")
		}
	})
}
