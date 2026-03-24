package core

import (
	"strings"
	"testing"
	"time"

	"go-llm-demo/internal/tui/state"
)

func TestCountLines(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{name: "empty", in: "", want: 0},
		{name: "single", in: "hello", want: 1},
		{name: "multi", in: "a\nb\nc", want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countLines(tt.in); got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestToComponentMessagesPreservesFields(t *testing.T) {
	ts := time.Unix(123, 0)
	m := Model{
		chat: state.ChatState{Messages: []state.Message{{
			Role:      "assistant",
			Content:   "hello",
			Timestamp: ts,
			Streaming: true,
		}}},
	}

	got := m.toComponentMessages()
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0].Role != "assistant" || got[0].Content != "hello" || !got[0].Timestamp.Equal(ts) || !got[0].Streaming {
		t.Fatalf("unexpected converted message: %+v", got[0])
	}
}

func TestViewShowsSmallWindowMessage(t *testing.T) {
	m := Model{}
	m.ui.Width = 10
	m.ui.Height = 5

	if got := m.View(); got != "窗口太小" {
		t.Fatalf("expected small window warning, got %q", got)
	}
}

func TestViewRendersHelpPanelInHelpMode(t *testing.T) {
	m := NewModel(fakeChatClient{}, "persona", 6, "config.yaml", "workspace")
	m.ui.Width = 80
	m.ui.Height = 30
	m.ui.Mode = state.ModeHelp

	rendered := m.View()
	if !strings.Contains(rendered, "NeoCode 帮助") {
		t.Fatalf("expected help panel in view, got %q", rendered)
	}
	if !strings.Contains(rendered, "/help") {
		t.Fatalf("expected help commands in view, got %q", rendered)
	}
}
