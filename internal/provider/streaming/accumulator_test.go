package streaming

import (
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestAccumulatorNilReceiver(t *testing.T) {
	var acc *Accumulator

	if acc.MessageDone() {
		t.Fatalf("nil accumulator should not be done")
	}

	message, err := acc.BuildMessage()
	if err != nil {
		t.Fatalf("BuildMessage() error = %v", err)
	}
	if message.Role != providertypes.RoleAssistant {
		t.Fatalf("message role = %q, want %q", message.Role, providertypes.RoleAssistant)
	}
	if len(message.Parts) != 0 {
		t.Fatalf("nil accumulator should not produce parts, got %d", len(message.Parts))
	}

	acc.AccumulateText("ignored")
	acc.AccumulateToolCallStart(0, "id", "name")
	acc.AccumulateToolCallDelta(0, "id", "{}")
	acc.MarkMessageDone()
}

func TestAccumulatorBuildMessageWithTextAndToolCalls(t *testing.T) {
	acc := NewAccumulator()
	acc.AccumulateText("hello ")
	acc.AccumulateText("world")

	acc.AccumulateToolCallStart(2, "call-2", "bash")
	acc.AccumulateToolCallDelta(2, "", "{\"cmd\":\"echo ")
	acc.AccumulateToolCallDelta(2, "", "ok\"}")
	acc.AccumulateToolCallStart(0, "call-0", "read_file")
	acc.AccumulateToolCallDelta(0, "call-0", "{\"path\":\"README.md\"}")

	acc.MarkMessageDone()
	if !acc.MessageDone() {
		t.Fatalf("expected message done")
	}

	message, err := acc.BuildMessage()
	if err != nil {
		t.Fatalf("BuildMessage() error = %v", err)
	}

	if len(message.Parts) != 1 || message.Parts[0].Text != "hello world" {
		t.Fatalf("unexpected message parts: %#v", message.Parts)
	}
	if len(message.ToolCalls) != 2 {
		t.Fatalf("tool calls len = %d, want 2", len(message.ToolCalls))
	}
	if message.ToolCalls[0].ID != "call-0" || message.ToolCalls[0].Name != "read_file" {
		t.Fatalf("unexpected first tool call: %#v", message.ToolCalls[0])
	}
	if message.ToolCalls[1].ID != "call-2" || message.ToolCalls[1].Name != "bash" {
		t.Fatalf("unexpected second tool call: %#v", message.ToolCalls[1])
	}
	if message.ToolCalls[1].Arguments != "{\"cmd\":\"echo ok\"}" {
		t.Fatalf("unexpected accumulated arguments: %q", message.ToolCalls[1].Arguments)
	}
}

func TestAccumulatorBuildMessageErrorBranches(t *testing.T) {
	t.Run("missing id", func(t *testing.T) {
		acc := NewAccumulator()
		acc.AccumulateToolCallStart(1, "", "bash")
		_, err := acc.BuildMessage()
		if err == nil || !strings.Contains(err.Error(), "without id") {
			t.Fatalf("expected missing id error, got %v", err)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		acc := NewAccumulator()
		acc.AccumulateToolCallDelta(1, "call-1", "{}")
		_, err := acc.BuildMessage()
		if err == nil || !strings.Contains(err.Error(), "without name") {
			t.Fatalf("expected missing name error, got %v", err)
		}
	})
}
