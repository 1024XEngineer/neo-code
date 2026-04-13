package memo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"neo-code/internal/memo"
	"neo-code/internal/tools"
)

func TestRecallToolName(t *testing.T) {
	tool := NewRecallTool(nil)
	if tool.Name() != tools.ToolNameMemoRecall {
		t.Errorf("Name() = %q, want %q", tool.Name(), tools.ToolNameMemoRecall)
	}
}

func TestRecallToolSchema(t *testing.T) {
	tool := NewRecallTool(nil)
	schema := tool.Schema()
	if schema["type"] != "object" {
		t.Errorf("Schema type = %v, want object", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Schema properties is not a map")
	}
	if _, exists := props["keyword"]; !exists {
		t.Error("Schema missing 'keyword' property")
	}
}

func TestRecallToolMicroCompactPolicy(t *testing.T) {
	tool := NewRecallTool(nil)
	if tool.MicroCompactPolicy() != tools.MicroCompactPolicyPreserveHistory {
		t.Errorf("MicroCompactPolicy() = %v, want PreserveHistory", tool.MicroCompactPolicy())
	}
}

func TestRecallToolExecuteSuccess(t *testing.T) {
	svc := newTestService(t)
	// 预先写入记忆
	svc.Add(context.Background(), memo.Entry{
		Type:    memo.TypeUser,
		Title:   "偏好中文注释",
		Content: "用户偏好使用中文注释和 tab 缩进",
		Source:  memo.SourceUserManual,
	})

	tool := NewRecallTool(svc)
	args, _ := json.Marshal(recallInput{Keyword: "中文"})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error result: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Found 1 memory") {
		t.Errorf("Content should show match count: %q", result.Content)
	}
	if !strings.Contains(result.Content, "中文注释") {
		t.Errorf("Content should contain topic content: %q", result.Content)
	}
}

func TestRecallToolExecuteNoMatch(t *testing.T) {
	svc := newTestService(t)
	tool := NewRecallTool(svc)

	args, _ := json.Marshal(recallInput{Keyword: "nonexistent"})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Errorf("no match should not be an error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "No memories found") {
		t.Errorf("Content should show no match: %q", result.Content)
	}
}

func TestRecallToolExecuteInvalidJSON(t *testing.T) {
	tool := NewRecallTool(nil)
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: []byte("not json")})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestRecallToolExecuteNilService(t *testing.T) {
	tool := NewRecallTool(nil)
	args, _ := json.Marshal(recallInput{Keyword: "tab"})
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

func TestRecallToolExecuteEmptyKeyword(t *testing.T) {
	svc := newTestService(t)
	tool := NewRecallTool(svc)

	args, _ := json.Marshal(recallInput{Keyword: ""})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err == nil {
		t.Error("expected error for empty keyword")
	}
	if !result.IsError {
		t.Error("expected error result")
	}
}

func TestRecallToolExecuteWhitespaceKeyword(t *testing.T) {
	svc := newTestService(t)
	tool := NewRecallTool(svc)

	args, _ := json.Marshal(recallInput{Keyword: "   "})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err == nil {
		t.Error("expected error for whitespace keyword")
	}
	if !result.IsError {
		t.Error("expected error result")
	}
}

func TestRecallToolExecuteMultipleResults(t *testing.T) {
	svc := newTestService(t)
	svc.Add(context.Background(), memo.Entry{Type: memo.TypeUser, Title: "偏好 tab", Content: "tab content", Source: memo.SourceUserManual})
	svc.Add(context.Background(), memo.Entry{Type: memo.TypeFeedback, Title: "反馈 tab 问题", Content: "feedback content", Source: memo.SourceUserManual})

	tool := NewRecallTool(svc)
	args, _ := json.Marshal(recallInput{Keyword: "tab"})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Found 2 memory") {
		t.Errorf("Content should show 2 matches: %q", result.Content)
	}
}

func TestRecallToolDescription(t *testing.T) {
	tool := NewRecallTool(nil)
	desc := tool.Description()
	if !strings.Contains(desc, "memory") {
		t.Errorf("Description should mention 'memory': %q", desc)
	}
}

func TestRecallToolExecuteAppliesOutputLimit(t *testing.T) {
	svc := newTestService(t)
	if err := svc.Add(context.Background(), memo.Entry{
		Type:    memo.TypeReference,
		Title:   "超长记忆",
		Content: strings.Repeat("x", tools.DefaultOutputLimitBytes+1024),
		Source:  memo.SourceUserManual,
	}); err != nil {
		t.Fatalf("seed memo entry: %v", err)
	}

	tool := NewRecallTool(svc)
	args, _ := json.Marshal(recallInput{Keyword: "超长"})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success result, got error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "...[truncated]") {
		t.Fatalf("expected truncated suffix, got content length %d", len(result.Content))
	}
	truncated, ok := result.Metadata["truncated"].(bool)
	if !ok || !truncated {
		t.Fatalf("expected metadata truncated=true, got %+v", result.Metadata)
	}
}
