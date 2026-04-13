package memo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/memo"
	"neo-code/internal/tools"
)

// newTestService 创建绑定临时目录的 memo.Service 实例。
func newTestService(t *testing.T) *memo.Service {
	t.Helper()
	store := memo.NewFileStore(t.TempDir(), t.TempDir())
	return memo.NewService(store, nil, config.MemoConfig{MaxIndexLines: 200}, nil)
}

func TestRememberToolName(t *testing.T) {
	tool := NewRememberTool(nil)
	if tool.Name() != tools.ToolNameMemoRemember {
		t.Errorf("Name() = %q, want %q", tool.Name(), tools.ToolNameMemoRemember)
	}
}

func TestRememberToolSchema(t *testing.T) {
	tool := NewRememberTool(nil)
	schema := tool.Schema()
	if schema["type"] != "object" {
		t.Errorf("Schema type = %v, want object", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Schema properties is not a map")
	}
	for _, field := range []string{"type", "title", "content"} {
		if _, exists := props[field]; !exists {
			t.Errorf("Schema missing required property %q", field)
		}
	}
}

func TestRememberToolMicroCompactPolicy(t *testing.T) {
	tool := NewRememberTool(nil)
	if tool.MicroCompactPolicy() != tools.MicroCompactPolicyPreserveHistory {
		t.Errorf("MicroCompactPolicy() = %v, want PreserveHistory", tool.MicroCompactPolicy())
	}
}

func TestRememberToolExecuteSuccess(t *testing.T) {
	svc := newTestService(t)
	tool := NewRememberTool(svc)

	args, _ := json.Marshal(rememberInput{
		Type:    "user",
		Title:   "偏好中文注释",
		Content: "用户偏好使用中文注释和 tab 缩进",
	})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error result: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Memory saved") {
		t.Errorf("Content = %q, want saved confirmation", result.Content)
	}
	if !strings.Contains(result.Content, "偏好中文注释") {
		t.Errorf("Content should contain title: %q", result.Content)
	}

	// 验证实际保存（索引只保留 Type/Title/TopicFile，完整信息在 topic 文件中）
	entries, _ := svc.List(context.Background())
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Type != memo.TypeUser {
		t.Errorf("Type = %q, want %q", entries[0].Type, memo.TypeUser)
	}
}

func TestRememberToolExecuteWithKeywords(t *testing.T) {
	svc := newTestService(t)
	tool := NewRememberTool(svc)

	args, _ := json.Marshal(rememberInput{
		Type:     "feedback",
		Title:    "不要 mock 数据库",
		Content:  "集成测试必须连接真实数据库",
		Keywords: []string{"testing", "database"},
	})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error result: %s", result.Content)
	}

	entries, _ := svc.List(context.Background())
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	// Keywords 存储在 topic 文件中，不在索引中
	if entries[0].TopicFile == "" {
		t.Error("TopicFile should be set")
	}
}

func TestRememberToolExecuteInvalidJSON(t *testing.T) {
	tool := NewRememberTool(nil)
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: []byte("not json")})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestRememberToolExecuteNilService(t *testing.T) {
	tool := NewRememberTool(nil)
	args, _ := json.Marshal(rememberInput{
		Type:    "user",
		Title:   "偏好中文注释",
		Content: "用户偏好使用中文注释和 tab 缩进",
	})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err == nil {
		t.Fatal("expected error for nil service")
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if !strings.Contains(result.Content, "service is nil") {
		t.Fatalf("unexpected error content: %q", result.Content)
	}
}

func TestRememberToolExecuteMissingFields(t *testing.T) {
	svc := newTestService(t)
	tool := NewRememberTool(svc)

	tests := []struct {
		name string
		args rememberInput
	}{
		{"empty type", rememberInput{Type: "", Title: "t", Content: "c"}},
		{"empty title", rememberInput{Type: "user", Title: "", Content: "c"}},
		{"empty content", rememberInput{Type: "user", Title: "t", Content: ""}},
		{"whitespace type", rememberInput{Type: "  ", Title: "t", Content: "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, _ := json.Marshal(tt.args)
			result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
			if err == nil {
				t.Error("expected error for missing fields")
			}
			if !result.IsError {
				t.Error("expected error result")
			}
		})
	}
}

func TestRememberToolExecuteInvalidType(t *testing.T) {
	svc := newTestService(t)
	tool := NewRememberTool(svc)

	args, _ := json.Marshal(rememberInput{Type: "invalid", Title: "t", Content: "c"})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err == nil {
		t.Error("expected error for invalid type")
	}
	if !result.IsError {
		t.Error("expected error result")
	}
	if !strings.Contains(result.Content, "invalid type") {
		t.Errorf("Content should mention invalid type: %q", result.Content)
	}
}

func TestRememberToolExecuteAllTypes(t *testing.T) {
	svc := newTestService(t)
	tool := NewRememberTool(svc)

	for _, memoType := range memo.ValidTypes() {
		t.Run(string(memoType), func(t *testing.T) {
			args, _ := json.Marshal(rememberInput{
				Type:    string(memoType),
				Title:   "test " + string(memoType),
				Content: "content for " + string(memoType),
			})
			result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
			if err != nil {
				t.Fatalf("Execute error for type %s: %v", memoType, err)
			}
			if result.IsError {
				t.Errorf("unexpected error for type %s: %s", memoType, result.Content)
			}
		})
	}
}

func TestRememberToolExecuteServiceError(t *testing.T) {
	svc := newTestService(t)
	tool := NewRememberTool(svc)

	args, _ := json.Marshal(rememberInput{Type: "user", Title: "test", Content: "test"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 取消上下文以触发错误

	result, err := tool.Execute(ctx, tools.ToolCallInput{Arguments: args})
	if err == nil {
		t.Error("expected error when context is cancelled")
	}
	if !result.IsError {
		t.Error("expected error result")
	}
}
