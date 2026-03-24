package components

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHelpContainsKeyCommands(t *testing.T) {
	rendered := RenderHelp(80)

	for _, want := range []string{"NeoCode 帮助", "/help", "/provider <name>", "按 Esc 或 /help 关闭"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected help to contain %q, got %q", want, rendered)
		}
	}
}

func TestInputBoxRenderChangesFooterByGeneratingState(t *testing.T) {
	idle := InputBox{Body: "body", Generating: false}.Render()
	if !strings.Contains(idle, "Ctrl+V粘贴") {
		t.Fatalf("expected idle footer to mention paste, got %q", idle)
	}

	busy := InputBox{Body: "body", Generating: true}.Render()
	if strings.Contains(busy, "Ctrl+V粘贴") {
		t.Fatalf("expected generating footer to omit paste hint, got %q", busy)
	}
	if !strings.Contains(busy, "F5/F8发送") {
		t.Fatalf("expected busy footer to keep send hint, got %q", busy)
	}
}

func TestMessageListRenderIncludesRoleSpecificLabels(t *testing.T) {
	rendered := MessageList{
		Width: 60,
		Messages: []Message{
			{Role: "user", Content: "hello", Timestamp: time.Unix(1, 0)},
			{Role: "assistant", Content: "world", Timestamp: time.Unix(2, 0)},
			{Role: "system", Content: "note", Timestamp: time.Unix(3, 0)},
		},
	}.Render()

	for _, want := range []string{"你 [1]:", "Neo [2]:", "[系统]", "hello", "world", "note"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered list to contain %q, got %q", want, rendered)
		}
	}
}

func TestMessageListRenderReturnsEmptyForNoMessages(t *testing.T) {
	if got := (MessageList{Width: 40}).Render(); got != "" {
		t.Fatalf("expected empty render, got %q", got)
	}
}
