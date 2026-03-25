package components

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHelpContainsKeyCommands(t *testing.T) {
	rendered := RenderHelp(80)

	for _, want := range []string{
		"NeoCode Help",
		"/help",
		"/provider <name>",
		"/memory-list",
		"/project-memory",
		"/remember <text>",
		"/resume",
		"/memory-mode",
		"Press Esc or /help to close",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected help to contain %q, got %q", want, rendered)
		}
	}
}

func TestInputBoxRenderChangesFooterByGeneratingState(t *testing.T) {
	idle := InputBox{Body: "body", Generating: false}.Render()
	if !strings.Contains(idle, "Ctrl+V: paste") {
		t.Fatalf("expected idle footer to mention paste, got %q", idle)
	}
	if !strings.Contains(idle, "click [Copy]: copy") {
		t.Fatalf("expected idle footer to mention copy action, got %q", idle)
	}

	busy := InputBox{Body: "body", Generating: true}.Render()
	if strings.Contains(busy, "Ctrl+V: paste") {
		t.Fatalf("expected generating footer to omit paste hint, got %q", busy)
	}
	if !strings.Contains(busy, "F5/F8: send") {
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

	for _, want := range []string{"You [1]:", "Neo [2]:", "[System]", "hello", "world", "note"} {
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

func TestMessageListRenderLayoutIncludesCopyRegions(t *testing.T) {
	layout := MessageList{
		Width:    60,
		Messages: []Message{{Role: "assistant", Content: "```go\nfmt.Println(1)\n```", Timestamp: time.Unix(1, 0)}},
	}.RenderLayout()

	if !strings.Contains(layout.Content, "[Copy] go") {
		t.Fatalf("expected copy action in layout, got %q", layout.Content)
	}
	if len(layout.Regions) != 1 {
		t.Fatalf("expected one clickable region, got %d", len(layout.Regions))
	}
	region := layout.Regions[0]
	if region.Kind != "copy" || region.StartRow != 1 || region.StartCol != 1 || region.EndCol != len(CopyActionLabel()) {
		t.Fatalf("unexpected region: %+v", region)
	}
	if region.CodeBlock.Code != "fmt.Println(1)" {
		t.Fatalf("expected copied code, got %+v", region.CodeBlock)
	}
}
