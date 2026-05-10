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

// TestLLMExtractorExtractValidJSON 验证提取器可解析合法 JSON 并收敛字段。
func TestLLMExtractorExtractValidJSON(t *testing.T) {
	generator := &stubTextGenerator{
		response: `[{"type":"user","title":" 偏好 Go 代码风格 ","content":"用户偏好使用 Go 惯用写法。","keywords":["go"," style ","go"]}]`,
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
	if !strings.Contains(generator.prompt, "用户偏好") {
		t.Fatalf("prompt should describe user type, got %q", generator.prompt)
	}
	if !strings.Contains(generator.prompt, "2026-04-13") {
		t.Fatalf("prompt should include absolute local date, got %q", generator.prompt)
	}
}

// TestLLMExtractorExtractSkipsEmptyOrNonUserInputs 验证缺少有效用户输入时不会调用模型。
func TestLLMExtractorExtractSkipsEmptyOrNonUserInputs(t *testing.T) {
	tests := []struct {
		name     string
		messages []providertypes.Message
	}{
		{
			name:     "nil messages",
			messages: nil,
		},
		{
			name: "assistant only",
			messages: []providertypes.Message{
				{Role: providertypes.RoleAssistant, Parts: []providertypes.ContentPart{providertypes.NewTextPart("只有助手消息。")}},
			},
		},
		{
			name: "image only user",
			messages: []providertypes.Message{
				{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewRemoteImagePart("https://example.com/pic.png")}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := &stubTextGenerator{response: `[]`}
			extractor := NewLLMExtractor(generator)
			entries, err := extractor.Extract(context.Background(), tt.messages)
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("len(entries) = %d, want 0", len(entries))
			}
			if generator.calls != 0 {
				t.Fatalf("Generate() calls = %d, want 0", generator.calls)
			}
		})
	}
}

// TestLLMExtractorExtractUsesFullRunMessages 验证提取器使用完整 run 消息并排除 system/tool 噪声。
func TestLLMExtractorExtractUsesFullRunMessages(t *testing.T) {
	generator := &stubTextGenerator{response: `[]`}
	extractor := NewLLMExtractor(generator)

	messages := []providertypes.Message{
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("first")}},
		{Role: providertypes.RoleSystem, Parts: []providertypes.ContentPart{providertypes.NewTextPart("<acceptance_continue>must call todo_write</acceptance_continue>")}},
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
		{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("last")}},
	}

	_, err := extractor.Extract(context.Background(), messages)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(generator.messages) != 4 {
		t.Fatalf("len(generator.messages) = %d, want 4", len(generator.messages))
	}
	for _, message := range generator.messages {
		if message.Role == providertypes.RoleSystem {
			t.Fatalf("system reminder should not enter extraction window: %#v", message)
		}
	}
	if renderMemoParts(generator.messages[0].Parts) != "first" || renderMemoParts(generator.messages[3].Parts) != "last" {
		t.Fatalf("unexpected extraction window: %+v", generator.messages)
	}
}

// TestLLMExtractorExtractDropsIncompleteToolCallSpan 验证不完整的 tool call span 会被剔除。
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

// TestLLMExtractorResolveDecisionUsesShortlist 验证决策阶段会携带 shortlist 并解析 update 结果。
func TestLLMExtractorResolveDecisionUsesShortlist(t *testing.T) {
	generator := &stubTextGenerator{
		response: `{"action":"update","ref":"project:p.md","title":"测试策略","content":"用户要求修改后先跑相关测试。","keywords":["test"]}`,
	}
	extractor := NewLLMExtractor(generator)

	decision, err := extractor.ResolveDecision(
		context.Background(),
		Entry{Type: TypeFeedback, Title: "测试策略", Content: "以后修改完先跑相关测试。"},
		[]ExtractionCandidate{
			{
				Ref:      "project:p.md",
				Scope:    ScopeProject,
				Type:     TypeFeedback,
				Source:   SourceAutoExtract,
				Title:    "测试策略",
				Keywords: []string{"test"},
				Content:  "用户要求修改后先跑测试。",
			},
		},
	)
	if err != nil {
		t.Fatalf("ResolveDecision() error = %v", err)
	}
	if decision.Action != ExtractionActionUpdate || decision.Ref != "project:p.md" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	if decision.Entry.Type != "" {
		t.Fatalf("update decision should not require type, got %+v", decision.Entry)
	}
	if len(generator.messages) != 1 || generator.messages[0].Role != providertypes.RoleUser {
		t.Fatalf("expected one synthetic user message, got %+v", generator.messages)
	}
	if !strings.Contains(generator.prompt, `"ref":"project:p.md"`) {
		t.Fatalf("prompt should include shortlist candidates, got %q", generator.prompt)
	}
	if !strings.Contains(generator.prompt, `source="extractor_auto"`) {
		t.Fatalf("prompt should describe auto-extracted update rule, got %q", generator.prompt)
	}
}

// TestLLMExtractorResolveDecisionSupportsCreateAndSkip 验证单条决策支持 create 与 skip。
func TestLLMExtractorResolveDecisionSupportsCreateAndSkip(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		extractor := NewLLMExtractor(&stubTextGenerator{
			response: `{"action":"create","type":"project","title":"上线计划","content":"项目将在 2026-05-12 上线。","keywords":["release"]}`,
		})
		decision, err := extractor.ResolveDecision(
			context.Background(),
			Entry{Type: TypeProject, Title: "上线计划", Content: "项目将在下周上线。"},
			[]ExtractionCandidate{{Ref: "project:old.md", Scope: ScopeProject, Type: TypeProject, Source: SourceUserManual, Title: "旧计划", Content: "历史计划"}},
		)
		if err != nil {
			t.Fatalf("ResolveDecision(create) error = %v", err)
		}
		if decision.Action != ExtractionActionCreate || decision.Entry.Type != TypeProject {
			t.Fatalf("unexpected create decision: %+v", decision)
		}
	})

	t.Run("skip", func(t *testing.T) {
		extractor := NewLLMExtractor(&stubTextGenerator{
			response: `{"action":"skip","ref":"user:u.md"}`,
		})
		decision, err := extractor.ResolveDecision(
			context.Background(),
			Entry{Type: TypeUser, Title: "中文回复", Content: "用户偏好中文回复。"},
			[]ExtractionCandidate{{Ref: "user:u.md", Scope: ScopeUser, Type: TypeUser, Source: SourceUserManual, Title: "中文回复", Content: "用户偏好中文回复。"}},
		)
		if err != nil {
			t.Fatalf("ResolveDecision(skip) error = %v", err)
		}
		if decision.Action != ExtractionActionSkip || decision.Ref != "user:u.md" {
			t.Fatalf("unexpected skip decision: %+v", decision)
		}
	})
}

// TestLLMExtractorErrors 验证取消、空生成器和上游错误会正确透传。
func TestLLMExtractorErrors(t *testing.T) {
	t.Run("canceled context", func(t *testing.T) {
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
	})

	t.Run("nil generator", func(t *testing.T) {
		extractor := NewLLMExtractor(nil)
		_, err := extractor.Extract(context.Background(), []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住这个。")}},
		})
		if err == nil || !strings.Contains(err.Error(), "text generator is nil") {
			t.Fatalf("Extract() error = %v", err)
		}
	})

	t.Run("generator failure", func(t *testing.T) {
		extractor := NewLLMExtractor(&stubTextGenerator{err: errors.New("upstream failed")})
		_, err := extractor.Extract(context.Background(), []providertypes.Message{
			{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("记住这个。")}},
		})
		if err == nil || !strings.Contains(err.Error(), "upstream failed") {
			t.Fatalf("Extract() error = %v", err)
		}
	})
}

// TestJSONPayloadExtractors 验证数组与对象提取器的错误分支。
func TestJSONPayloadExtractors(t *testing.T) {
	if _, err := extractJSONArray("no json here"); err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("expected missing array error, got %v", err)
	}
	if _, err := extractJSONArray(`[{"a":"x"}`); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete array error, got %v", err)
	}
	if _, err := extractJSONObject("no json here"); err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("expected missing object error, got %v", err)
	}
	if _, err := extractJSONObject(`{"a":"x"`); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete object error, got %v", err)
	}
}
