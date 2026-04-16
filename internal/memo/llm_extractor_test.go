package memo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
)

type stubTextGenerator struct {
	response string
	err      error
	calls    int
	prompt   string
	messages []providertypes.Message
}

func (s *stubTextGenerator) Generate(
	ctx context.Context,
	prompt string,
	messages []providertypes.Message,
) (string, error) {
	s.calls++
	s.prompt = prompt
	s.messages = append([]providertypes.Message(nil), messages...)
	if s.err != nil {
		return "", s.err
	}
	return s.response, nil
}

// TestLLMExtractorExtractValidJSON 验证提取器可以解析合法 JSON 并收敛字段。
func TestLLMExtractorExtractValidJSON(t *testing.T) {
	generator := &stubTextGenerator{
		response: `[{"type":"user","title":" 偏好 Go 代码风格 ","content":"用户偏好使用 Go 惯用写法。","keywords":["go","  style ","go"]}]`,
	}
	extractor := NewLLMExtractor(generator)
	extractor.now = func() time.Time {
		return time.Date(2026, 4, 13, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	}

	entries, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("以后默认按 Go 惯用风格写。")}},
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("收到。")}},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Type != TypeUser {
		t.Fatalf("Type = %q, want %q", entries[0].Type, TypeUser)
	}
	if entries[0].Title != "偏好 Go 代码风格" {
		t.Fatalf("Title = %q", entries[0].Title)
	}
	if entries[0].Content != "用户偏好使用 Go 惯用写法。" {
		t.Fatalf("Content = %q", entries[0].Content)
	}
	if len(entries[0].Keywords) != 2 || entries[0].Keywords[1] != "style" {
		t.Fatalf("Keywords = %#v", entries[0].Keywords)
	}
	if entries[0].Source != SourceAutoExtract {
		t.Fatalf("Source = %q, want %q", entries[0].Source, SourceAutoExtract)
	}
	if generator.calls != 1 {
		t.Fatalf("Generate() calls = %d, want 1", generator.calls)
	}
	if !strings.Contains(generator.prompt, "user: 用户偏好") {
		t.Fatalf("prompt should describe user type, got %q", generator.prompt)
	}
	if !strings.Contains(generator.prompt, "当前本地日期：2026-04-13") {
		t.Fatalf("prompt should include absolute local date, got %q", generator.prompt)
	}
	if !strings.Contains(generator.prompt, "必须先转换为绝对日期") {
		t.Fatalf("prompt should require absolute dates, got %q", generator.prompt)
	}
}

// TestLLMExtractorExtractEmptyResult 验证空数组响应会返回零条记忆。
func TestLLMExtractorExtractEmptyResult(t *testing.T) {
	extractor := NewLLMExtractor(&stubTextGenerator{response: `[]`})

	entries, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("这轮没有需要记住的内容。")}},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(entries))
	}
}

// TestLLMExtractorExtractNoUserMessage 验证没有用户消息时不会调用模型。
func TestLLMExtractorExtractNoUserMessage(t *testing.T) {
	generator := &stubTextGenerator{response: `[]`}
	extractor := NewLLMExtractor(generator)

	entries, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("只有助手消息。")}},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(entries))
	}
	if generator.calls != 0 {
		t.Fatalf("Generate() calls = %d, want 0", generator.calls)
	}
}

// TestLLMExtractorExtractNoMessages 验证空消息输入直接返回空结果。
func TestLLMExtractorExtractNoMessages(t *testing.T) {
	extractor := NewLLMExtractor(&stubTextGenerator{response: `[]`})

	entries, err := extractor.Extract(context.Background(), nil)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(entries))
	}
}

// TestLLMExtractorExtractInvalidJSON 验证无效 JSON 会返回错误。
func TestLLMExtractorExtractInvalidJSON(t *testing.T) {
	extractor := NewLLMExtractor(&stubTextGenerator{response: `[{invalid json}]`})

	_, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住这个。")}},
	})
	if err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

// TestLLMExtractorExtractToleratesWrappedJSON 验证 JSON 前后噪声不会影响解析。
func TestLLMExtractorExtractToleratesWrappedJSON(t *testing.T) {
	extractor := NewLLMExtractor(&stubTextGenerator{
		response: "分析如下：\n[{\"type\":\"feedback\",\"title\":\"以后先跑测试\",\"content\":\"用户要求修改后先跑测试。\"}]\n以上完毕。",
	})

	entries, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("以后改完先跑测试。")}},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Type != TypeFeedback {
		t.Fatalf("entries = %#v", entries)
	}
}

// TestLLMExtractorExtractFiltersInvalidEntries 验证非法类型和空字段会被过滤。
func TestLLMExtractorExtractFiltersInvalidEntries(t *testing.T) {
	extractor := NewLLMExtractor(&stubTextGenerator{
		response: `[
			{"type":"invalid","title":"bad","content":"bad"},
			{"type":"project","title":" ","content":"missing title"},
			{"type":"reference","title":"文档入口","content":"查看 docs/runtime-provider-event-flow.md"}
		]`,
	})

	entries, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("参考文档在 docs/runtime-provider-event-flow.md。")}},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Type != TypeReference {
		t.Fatalf("entries = %#v", entries)
	}
}

// TestLLMExtractorExtractCancelledContext 验证已取消上下文会中止提取。
func TestLLMExtractorExtractCancelledContext(t *testing.T) {
	generator := &stubTextGenerator{response: `[]`}
	extractor := NewLLMExtractor(generator)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := extractor.Extract(ctx, []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住这个。")}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Extract() error = %v, want context.Canceled", err)
	}
	if generator.calls != 0 {
		t.Fatalf("Generate() calls = %d, want 0", generator.calls)
	}
}

// TestLLMExtractorExtractUsesRecentNonToolMessages 验证只取最近 10 条非 tool 消息。
func TestLLMExtractorExtractUsesRecentNonToolMessages(t *testing.T) {
	generator := &stubTextGenerator{response: `[]`}
	extractor := NewLLMExtractor(generator)

	messages := make([]providertypes.Message, 0, 16)
	for index := 0; index < 12; index++ {
		messages = append(messages, providertypes.Message{
			Role:  providertypes.RoleUser,
			Parts: []providertypes.ContentPart{providertypes.NewTextPart("user-" + string(rune('a'+index)))},
		})
		if index%3 == 0 {
			messages = append(messages, providertypes.Message{
				Role:  providertypes.RoleTool,
				Parts: []providertypes.ContentPart{providertypes.NewTextPart("tool-" + string(rune('a'+index)))},
			})
		}
	}

	_, err := extractor.Extract(context.Background(), messages)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(generator.messages) != 10 {
		t.Fatalf("len(generator.messages) = %d, want 10", len(generator.messages))
	}
	for _, message := range generator.messages {
		if message.Role == providertypes.RoleTool {
			t.Fatalf("unexpected tool message in extraction context: %#v", message)
		}
	}
	if renderMemoParts(generator.messages[0].Parts) != "user-c" ||
		renderMemoParts(generator.messages[9].Parts) != "user-l" {
		t.Fatalf("unexpected recent window: first=%q last=%q",
			renderMemoParts(generator.messages[0].Parts),
			renderMemoParts(generator.messages[9].Parts))
	}
}

func TestLLMExtractorExtractDropsIncompleteToolCallSpan(t *testing.T) {
	generator := &stubTextGenerator{response: `[]`}
	extractor := NewLLMExtractor(generator)

	_, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("first")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call_1", Name: "filesystem_read_file", Arguments: `{}`},
			},
		},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("second")}},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(generator.messages) != 2 {
		t.Fatalf("len(generator.messages) = %d, want 2", len(generator.messages))
	}
	for _, message := range generator.messages {
		if len(message.ToolCalls) > 0 {
			t.Fatalf("unexpected tool call message in extraction window: %#v", message)
		}
	}
}

func TestLLMExtractorExtractKeepsProjectedToolCallSpan(t *testing.T) {
	generator := &stubTextGenerator{response: `[]`}
	extractor := NewLLMExtractor(generator)

	_, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("remember this")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call_1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
			},
		},
		{
			Role:         providertypes.RoleTool,
			ToolCallID:   "call_1",
			Parts:        []providertypes.ContentPart{providertypes.NewTextPart("README body")},
			ToolMetadata: map[string]string{"tool_name": "filesystem_read_file", "path": "README.md"},
		},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(generator.messages) != 3 {
		t.Fatalf("len(generator.messages) = %d, want 3", len(generator.messages))
	}
	if generator.messages[1].Role != providertypes.RoleAssistant || len(generator.messages[1].ToolCalls) != 1 {
		t.Fatalf("expected assistant tool call span to be preserved, got %#v", generator.messages[1])
	}
	toolMessage := generator.messages[2]
	if toolMessage.Role != providertypes.RoleTool {
		t.Fatalf("expected projected tool message, got %#v", toolMessage)
	}
	toolText := renderMemoParts(toolMessage.Parts)
	if !strings.Contains(toolText, "tool result") || !strings.Contains(toolText, "tool: filesystem_read_file") {
		t.Fatalf("expected projected tool text, got %q", toolText)
	}
	if toolMessage.ToolMetadata != nil {
		t.Fatalf("expected projected tool metadata to be cleared, got %#v", toolMessage.ToolMetadata)
	}
}

func TestLLMExtractorExtractKeepsMetadataOnlyToolCallSpan(t *testing.T) {
	generator := &stubTextGenerator{response: `[]`}
	extractor := NewLLMExtractor(generator)

	_, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("remember this")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call_1", Name: "filesystem_read_file", Arguments: `{"path":"README.md"}`},
			},
		},
		{
			Role:         providertypes.RoleTool,
			ToolCallID:   "call_1",
			ToolMetadata: map[string]string{"tool_name": "filesystem_read_file", "path": "README.md"},
		},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(generator.messages) != 3 {
		t.Fatalf("len(generator.messages) = %d, want 3", len(generator.messages))
	}
	toolMessage := generator.messages[2]
	if toolMessage.Role != providertypes.RoleTool {
		t.Fatalf("expected projected tool message, got %#v", toolMessage)
	}
	toolText := renderMemoParts(toolMessage.Parts)
	if !strings.Contains(toolText, "tool result") ||
		!strings.Contains(toolText, "tool: filesystem_read_file") ||
		!strings.Contains(toolText, "meta.path: README.md") {
		t.Fatalf("expected metadata-only tool text, got %q", toolText)
	}
	if strings.Contains(toolText, "content:\n") {
		t.Fatalf("expected metadata-only projection to omit content section, got %q", toolText)
	}
	if toolMessage.ToolMetadata != nil {
		t.Fatalf("expected projected tool metadata to be cleared, got %#v", toolMessage.ToolMetadata)
	}
}

func TestLLMExtractorExtractSkipsOrphanAndClearedToolMessages(t *testing.T) {
	generator := &stubTextGenerator{response: `[]`}
	extractor := NewLLMExtractor(generator)

	_, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("alpha")}},
		{Role: providertypes.RoleTool, ToolCallID: "orphan", Parts: []providertypes.ContentPart{providertypes.NewTextPart("orphan result")}},
		{
			Role: providertypes.RoleAssistant,
			ToolCalls: []providertypes.ToolCall{
				{ID: "call_1", Name: "bash", Arguments: `{}`},
			},
		},
		{Role: providertypes.RoleTool, ToolCallID: "call_1", Parts: []providertypes.ContentPart{providertypes.NewTextPart("[Old tool result content cleared]")}},
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("beta")}},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(generator.messages) != 2 {
		t.Fatalf("len(generator.messages) = %d, want 2", len(generator.messages))
	}
	for _, message := range generator.messages {
		if message.Role == providertypes.RoleTool || len(message.ToolCalls) > 0 {
			t.Fatalf("unexpected tool-related message in extraction window: %#v", message)
		}
	}
}

func TestLLMExtractorExtractNilGenerator(t *testing.T) {
	var extractor *LLMExtractor
	_, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住这个。")}},
	})
	if err == nil || !strings.Contains(err.Error(), "text generator is nil") {
		t.Fatalf("Extract() error = %v", err)
	}

	extractor = NewLLMExtractor(nil)
	_, err = extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住这个。")}},
	})
	if err == nil || !strings.Contains(err.Error(), "text generator is nil") {
		t.Fatalf("Extract() error = %v", err)
	}
}

func TestLLMExtractorExtractGeneratorFailure(t *testing.T) {
	extractor := NewLLMExtractor(&stubTextGenerator{err: errors.New("upstream failed")})
	_, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住这个。")}},
	})
	if err == nil || !strings.Contains(err.Error(), "upstream failed") {
		t.Fatalf("Extract() error = %v", err)
	}
}

func TestExtractJSONArrayErrors(t *testing.T) {
	if _, err := extractJSONArray("no json here"); err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("expected missing array error, got %v", err)
	}
	if _, err := extractJSONArray(`[{"a":"x"}`); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete array error, got %v", err)
	}
}

func TestLLMExtractorExtractImageOnlyUserMessageSkipsGenerator(t *testing.T) {
	generator := &stubTextGenerator{response: `[]`}
	extractor := NewLLMExtractor(generator)

	entries, err := extractor.Extract(context.Background(), []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewRemoteImagePart("https://example.com/pic.png")}},
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0", len(entries))
	}
	if generator.calls != 0 {
		t.Fatalf("Generate() calls = %d, want 0", generator.calls)
	}
}
