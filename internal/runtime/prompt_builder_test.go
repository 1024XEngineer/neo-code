package runtime

import (
	"strings"
	"testing"
)

func TestPromptBuilderMentionsEditToolGuidance(t *testing.T) {
	builder := NewPromptBuilder(t.TempDir())

	messages := builder.Build(Session{})
	if len(messages) == 0 {
		t.Fatalf("expected system prompt message")
	}

	content := messages[0].Content
	if !strings.Contains(content, "fs_edit_file") {
		t.Fatalf("expected prompt to mention fs_edit_file, got %q", content)
	}
	if !strings.Contains(content, "fs_write_file") {
		t.Fatalf("expected prompt to mention fs_write_file, got %q", content)
	}
}
