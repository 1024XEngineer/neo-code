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

func newTestService(t *testing.T) *memo.Service {
	t.Helper()
	store := memo.NewFileStore(t.TempDir(), t.TempDir())
	return memo.NewService(store, config.MemoConfig{
		MaxEntries:        200,
		MaxIndexBytes:     16 * 1024,
		ExtractTimeoutSec: 15,
	}, nil)
}

func TestRememberToolName(t *testing.T) {
	tool := NewRememberTool(nil)
	if tool.Name() != tools.ToolNameMemoRemember {
		t.Fatalf("Name() = %q, want %q", tool.Name(), tools.ToolNameMemoRemember)
	}
}

func TestRememberToolExecuteSuccess(t *testing.T) {
	svc := newTestService(t)
	tool := NewRememberTool(svc)

	args, _ := json.Marshal(rememberInput{
		Type:     "user",
		Title:    "prefer chinese comments",
		Content:  "prefer chinese comments",
		Keywords: []string{" comments ", "comments", "style"},
	})
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError || !strings.Contains(result.Content, "Memory saved") {
		t.Fatalf("unexpected result: %+v", result)
	}

	entries, err := svc.List(context.Background(), memo.ScopeUser)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Type != memo.TypeUser {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if entries[0].TopicFile == "" {
		t.Fatal("expected TopicFile to be set")
	}
}

func TestRememberToolExecuteRejectsBadInput(t *testing.T) {
	svc := newTestService(t)
	tool := NewRememberTool(svc)

	tests := []rememberInput{
		{Type: "", Title: "t", Content: "c"},
		{Type: "user", Title: "", Content: "c"},
		{Type: "user", Title: "t", Content: ""},
		{Type: "bad", Title: "t", Content: "c"},
	}
	for _, tt := range tests {
		args, _ := json.Marshal(tt)
		result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
		if err == nil || !result.IsError {
			t.Fatalf("expected bad input to fail: %+v / %+v", tt, result)
		}
	}
}

func TestRememberToolExecuteNilService(t *testing.T) {
	tool := NewRememberTool(nil)
	args, _ := json.Marshal(rememberInput{Type: "user", Title: "t", Content: "c"})

	result, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: args})
	if err == nil || !result.IsError {
		t.Fatalf("expected nil service error, got result=%+v err=%v", result, err)
	}
}

func TestRememberToolExecuteInvalidJSON(t *testing.T) {
	tool := NewRememberTool(nil)
	if _, err := tool.Execute(context.Background(), tools.ToolCallInput{Arguments: []byte("not json")}); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}
