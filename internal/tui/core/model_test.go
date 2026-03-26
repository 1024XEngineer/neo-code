package core

import (
	"context"
	"testing"

	"go-llm-demo/configs"
	"go-llm-demo/internal/tui/state"
)

func TestNewModelAppliesDefaultsAndRuntimeFlags(t *testing.T) {
	restoreCoreGlobals(t)

	client := &fakeChatClient{defaultModelName: "demo-model"}
	t.Setenv(configs.DefaultAPIKeyEnvVar, "secret")
	configs.GlobalAppConfig = nil

	m := NewModel(client, "persona", 0, "config.yaml", "D:/neo-code")

	if m.chat.HistoryTurns != 6 {
		t.Fatalf("expected default history turns 6, got %d", m.chat.HistoryTurns)
	}
	if m.chat.ActiveModel != "demo-model" {
		t.Fatalf("expected default model from client, got %q", m.chat.ActiveModel)
	}
	if !m.chat.APIKeyReady {
		t.Fatal("expected API key readiness to reflect runtime env var")
	}
	if m.chat.WorkspaceRoot != "D:/neo-code" {
		t.Fatalf("expected workspace root to be stored, got %q", m.chat.WorkspaceRoot)
	}
}

func TestNewModelUsesEmptyStatsWhenClientReturnsNil(t *testing.T) {
	restoreCoreGlobals(t)

	client := &fakeChatClient{nilMemoryStats: true}

	m := NewModel(client, "persona", 4, "config.yaml", "D:/neo-code")
	if m.chat.MemoryStats.TotalItems != 0 {
		t.Fatalf("expected zero-value stats, got %+v", m.chat.MemoryStats)
	}
}

func TestAppendLastMessage(t *testing.T) {
	m := Model{}
	m.chat.Messages = []state.Message{{Role: "assistant", Content: "hello", Streaming: true}}

	m.AppendLastMessage(" world")

	if m.chat.Messages[0].Content != "hello world" {
		t.Fatalf("expected appended content, got %q", m.chat.Messages[0].Content)
	}
	if !m.chat.Messages[0].Streaming {
		t.Fatal("expected append helper to leave streaming state untouched")
	}
}

func TestInitReturnsNonNilCmd(t *testing.T) {
	restoreCoreGlobals(t)

	m := NewModel(&fakeChatClient{}, "persona", 4, "config.yaml", "D:/neo-code")
	if cmd := m.Init(); cmd == nil {
		t.Fatal("expected non-nil init cmd")
	}
}

func TestNewModelAddsResumeSummaryMessageWhenSupported(t *testing.T) {
	restoreCoreGlobals(t)

	client := &resumeSummaryClient{
		fakeChatClient: fakeChatClient{defaultModelName: "demo-model"},
		summary:        "已恢复上次工作现场：\n- 当前目标: 修复记忆模块",
	}

	m := NewModel(client, "persona", 4, "config.yaml", "D:/neo-code")
	if len(m.chat.Messages) != 1 {
		t.Fatalf("expected one resume summary message, got %+v", m.chat.Messages)
	}
	if m.chat.Messages[0].Role != "system" || !isResumeSummaryMessage(m.chat.Messages[0].Content) {
		t.Fatalf("expected resume summary system message, got %+v", m.chat.Messages[0])
	}
}

func TestConsumeThinkingChunkHidesCrossChunkThinkBlocks(t *testing.T) {
	m := Model{}

	got := m.consumeThinkingChunk("visible<thi")
	if got != "visible" {
		t.Fatalf("expected visible prefix, got %q", got)
	}

	got = m.consumeThinkingChunk("nk>secret")
	if got != "" {
		t.Fatalf("expected think block content to stay hidden, got %q", got)
	}

	got = m.consumeThinkingChunk(" thoughts</th")
	if got != "" {
		t.Fatalf("expected partial closing tag to stay hidden, got %q", got)
	}

	got = m.consumeThinkingChunk("ink>after")
	if got != "after" {
		t.Fatalf("expected visible suffix after think block, got %q", got)
	}
}

func TestTrimHistoryKeepsSystemMessagesAndLatestTurns(t *testing.T) {
	m := Model{}
	m.chat.Messages = []state.Message{
		{Role: "system", Content: "persona"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u3"},
		{Role: "assistant", Content: "a3"},
	}

	m.TrimHistory(2)

	if len(m.chat.Messages) != 5 {
		t.Fatalf("expected system message plus last two turns, got %d messages", len(m.chat.Messages))
	}
	if m.chat.Messages[0].Role != "system" || m.chat.Messages[0].Content != "persona" {
		t.Fatalf("expected system message to be preserved, got %+v", m.chat.Messages[0])
	}
	if m.chat.Messages[1].Content != "u2" || m.chat.Messages[4].Content != "a3" {
		t.Fatalf("expected only latest turns to remain, got %+v", m.chat.Messages)
	}
}

type resumeSummaryClient struct {
	fakeChatClient
	summary string
}

func (c *resumeSummaryClient) GetWorkingSessionSummary(context.Context) (string, error) {
	return c.summary, nil
}
