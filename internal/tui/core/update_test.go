package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go-llm-demo/internal/server/domain"
	"go-llm-demo/internal/tui/infra"
)

type fakeChatClient struct{}

func (fakeChatClient) Chat(context.Context, []infra.Message, string) (<-chan string, error) {
	return nil, errors.New("not implemented")
}

func (fakeChatClient) GetMemoryStats(context.Context) (*infra.MemoryStats, error) {
	return &infra.MemoryStats{}, nil
}

func (fakeChatClient) ClearMemory(context.Context) error {
	return nil
}

func (fakeChatClient) ClearSessionMemory(context.Context) error {
	return nil
}

func (fakeChatClient) DefaultModel() string {
	return "test-model"
}

func (fakeChatClient) GetTodoList(context.Context) ([]domain.Todo, error) {
	return nil, nil
}

func TestBuildMessagesSkipsEmptyAssistantPlaceholder(t *testing.T) {
	m := Model{
		messages: []Message{
			{Role: "system", Content: "persona"},
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: ""},
		},
	}

	got := m.buildMessages()
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Role != "system" || got[1].Role != "user" {
		t.Fatalf("unexpected message order: %+v", got)
	}
	if got[1].Content != "hello" {
		t.Fatalf("expected user message to be preserved, got %+v", got[1])
	}
}

func TestStreamErrorReplacesTrailingPlaceholder(t *testing.T) {
	m := Model{
		historyTurns: 6,
		messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: ""},
		},
	}

	updated, _ := m.Update(StreamErrorMsg{Err: errors.New("boom")})
	got := updated.(Model)
	if len(got.messages) != 2 {
		t.Fatalf("expected placeholder replacement without extra message, got %d messages", len(got.messages))
	}
	if got.messages[1].Content != "错误: boom" {
		t.Fatalf("expected trailing placeholder to become error, got %q", got.messages[1].Content)
	}
}

func TestClearContextDoesNotReinjectStalePersonaMessage(t *testing.T) {
	m := Model{
		client:      fakeChatClient{},
		persona:     "stale persona",
		apiKeyReady: true,
		messages: []Message{
			{Role: "system", Content: "stale persona"},
			{Role: "user", Content: "hello"},
		},
	}

	updated, _ := m.handleCommand("/clear-context")
	got := updated.(Model)
	if len(got.messages) != 1 {
		t.Fatalf("expected only confirmation message after clear-context, got %d messages", len(got.messages))
	}
	if got.messages[0].Role != "assistant" {
		t.Fatalf("expected confirmation assistant message, got %+v", got.messages[0])
	}
}

func TestBuildMessagesSkipsTransientToolStatusMessage(t *testing.T) {
	m := Model{
		messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "system", Content: "[TOOL_STATUS] tool=read file=README.md"},
			{Role: "assistant", Content: "ok"},
		},
	}

	got := m.buildMessages()
	if len(got) != 2 {
		t.Fatalf("expected 2 messages after filtering tool status, got %d", len(got))
	}
	for _, msg := range got {
		if msg.Role == "system" && strings.HasPrefix(msg.Content, "[TOOL_STATUS]") {
			t.Fatalf("transient tool status should not be included in model context: %+v", msg)
		}
	}
}

func TestBuildMessagesKeepsOnlyRecentToolContextMessages(t *testing.T) {
	m := Model{maxToolContextMessages: 3}
	m.messages = append(m.messages, Message{Role: "user", Content: "step 1"})
	for i := 1; i <= 5; i++ {
		m.messages = append(m.messages, Message{Role: "system", Content: "[TOOL_CONTEXT]\ntool=read\nsuccess=true\noutput:\nchunk " + string(rune('0'+i))})
	}
	m.messages = append(m.messages, Message{Role: "assistant", Content: "done"})

	got := m.buildMessages()
	toolCtxCount := 0
	for _, msg := range got {
		if msg.Role == "system" && strings.HasPrefix(msg.Content, "[TOOL_CONTEXT]") {
			toolCtxCount++
		}
	}
	if toolCtxCount != 5 {
		t.Fatalf("expected 5 tool context messages, got %d", toolCtxCount)
	}

	joined := ""
	for _, msg := range got {
		joined += msg.Content + "\n"
	}
	if strings.Contains(joined, "chunk 1") || strings.Contains(joined, "chunk 2") {
		t.Fatalf("old tool context should be evicted, got context: %s", joined)
	}
	if !strings.Contains(joined, "chunk 3") || !strings.Contains(joined, "chunk 4") || !strings.Contains(joined, "chunk 5") {
		t.Fatalf("newest tool context messages should be kept, got context: %s", joined)
	}
}

func TestWorkspaceCommandShowsWorkspaceRoot(t *testing.T) {
	m := Model{
		client:        fakeChatClient{},
		apiKeyReady:   true,
		workspaceRoot: `F:/Qiniu/test1`,
	}

	updated, _ := m.handleCommand("/pwd")
	got := updated.(Model)
	if len(got.messages) != 1 {
		t.Fatalf("expected exactly 1 message, got %d", len(got.messages))
	}
	if got.messages[0].Role != "assistant" {
		t.Fatalf("expected assistant message, got %+v", got.messages[0])
	}
	if !strings.Contains(got.messages[0].Content, `F:/Qiniu/test1`) {
		t.Fatalf("expected workspace path in response, got %q", got.messages[0].Content)
	}
}
