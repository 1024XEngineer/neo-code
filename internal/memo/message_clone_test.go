package memo

import (
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestCloneProviderMessageDeepCopy(t *testing.T) {
	t.Parallel()

	original := providertypes.Message{
		Role: providertypes.RoleAssistant,
		ToolCalls: []providertypes.ToolCall{
			{ID: "call-1", Name: "bash", Arguments: `{"command":"pwd"}`},
		},
		ToolMetadata: map[string]string{"tool_name": "bash"},
	}

	cloned := cloneProviderMessage(original)
	cloned.ToolCalls[0].Arguments = `{"command":"ls"}`
	cloned.ToolMetadata["tool_name"] = "filesystem"

	if original.ToolCalls[0].Arguments != `{"command":"pwd"}` {
		t.Fatalf("expected tool calls to be deep copied, got %+v", original.ToolCalls)
	}
	if original.ToolMetadata["tool_name"] != "bash" {
		t.Fatalf("expected tool metadata to be deep copied, got %+v", original.ToolMetadata)
	}
}

func TestCloneProviderMessageHandlesEmptyCollections(t *testing.T) {
	t.Parallel()

	cloned := cloneProviderMessage(providertypes.Message{Role: providertypes.RoleUser, Parts: []providertypes.ContentPart{providertypes.NewTextPart("hi")}})
	if cloned.Role != providertypes.RoleUser || renderMemoParts(cloned.Parts) != "hi" {
		t.Fatalf("unexpected cloned message %+v", cloned)
	}
	if cloned.ToolCalls != nil {
		t.Fatalf("expected nil tool calls, got %+v", cloned.ToolCalls)
	}
	if cloned.ToolMetadata != nil {
		t.Fatalf("expected nil tool metadata, got %+v", cloned.ToolMetadata)
	}
}
