package tui

import (
	"testing"
	"time"

	"neocode/internal/runtime"
)

func TestParseContentBlocksWithoutCode(t *testing.T) {
	blocks := parseContentBlocks("line one\nline two")

	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Kind != blockParagraph {
		t.Fatalf("expected paragraph block, got %s", blocks[0].Kind)
	}
	if blocks[0].Text != "line one\nline two" {
		t.Fatalf("unexpected paragraph text: %q", blocks[0].Text)
	}
}

func TestParseContentBlocksSingleCodeBlock(t *testing.T) {
	content := "intro\n```go\nfmt.Println(\"hi\")\n```\noutro"
	blocks := parseContentBlocks(content)

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[1].Kind != blockCode {
		t.Fatalf("expected second block to be code, got %s", blocks[1].Kind)
	}
	if blocks[1].Language != "go" {
		t.Fatalf("expected go language, got %q", blocks[1].Language)
	}
	if blocks[1].Text != "fmt.Println(\"hi\")" {
		t.Fatalf("unexpected code text: %q", blocks[1].Text)
	}
}

func TestParseContentBlocksMultipleAndUnclosedCodeBlocks(t *testing.T) {
	content := "```py\nprint(1)\n```\nbody\n```js\nconsole.log(2)"
	blocks := parseContentBlocks(content)

	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	if blocks[0].Kind != blockCode || blocks[0].Language != "py" {
		t.Fatalf("unexpected first block: %#v", blocks[0])
	}
	if blocks[1].Kind != blockParagraph || blocks[1].Text != "body" {
		t.Fatalf("unexpected second block: %#v", blocks[1])
	}
	if blocks[2].Kind != blockCode || blocks[2].Language != "js" || blocks[2].Text != "console.log(2)" {
		t.Fatalf("unexpected trailing unclosed code block: %#v", blocks[2])
	}
}

func TestFilterSessions(t *testing.T) {
	now := time.Now()
	sessions := []runtime.SessionSummary{
		{ID: "s1", Title: "Alpha work", UpdatedAt: now, MessageCount: 1},
		{ID: "s2", Title: "Beta plan", UpdatedAt: now, MessageCount: 2},
	}
	filtered := filterSessions(
		sessions,
		"s2",
		map[string]string{"s1": "streaming"},
		map[string]activeTool{
			toolStateKey("s2", "call-1"): {
				SessionID: "s2",
			},
		},
		"beta",
	)

	if len(filtered) != 1 {
		t.Fatalf("expected 1 session, got %d", len(filtered))
	}
	if filtered[0].Summary.ID != "s2" {
		t.Fatalf("expected s2, got %s", filtered[0].Summary.ID)
	}
	if !filtered[0].IsActive || !filtered[0].HasRunningTools || !filtered[0].IsBusy {
		t.Fatalf("expected active filtered session to be busy with running tools: %#v", filtered[0])
	}
}

func TestNextCodeBlockIndex(t *testing.T) {
	blocks := []renderedCodeBlock{
		{Index: 0},
		{Index: 1},
		{Index: 2},
	}

	if next := nextCodeBlockIndex(blocks, -1, 1); next != 0 {
		t.Fatalf("expected first block from empty selection, got %d", next)
	}
	if next := nextCodeBlockIndex(blocks, -1, -1); next != 2 {
		t.Fatalf("expected last block from empty reverse selection, got %d", next)
	}
	if next := nextCodeBlockIndex(blocks, 1, 1); next != 2 {
		t.Fatalf("expected next block 2, got %d", next)
	}
	if next := nextCodeBlockIndex(blocks, 1, -1); next != 0 {
		t.Fatalf("expected previous block 0, got %d", next)
	}
	if next := nextCodeBlockIndex(blocks, 2, 1); next != 2 {
		t.Fatalf("expected clamp at last block, got %d", next)
	}
}

func TestCopyTargets(t *testing.T) {
	conversation := renderedConversation{
		Messages: []renderedMessage{
			{MessageIndex: 0, CopyText: "message zero"},
			{MessageIndex: 1, CopyText: "message one"},
		},
		CodeBlocks: []renderedCodeBlock{
			{Index: 0, MessageIndex: 1, Content: "fmt.Println(\"hello\")"},
		},
	}

	messageText, codeText, okMessage, okCode := copyTargets(conversation, 0, 0)
	if !okMessage || !okCode {
		t.Fatalf("expected both message and code copy targets to resolve")
	}
	if messageText != "message one" {
		t.Fatalf("expected message one, got %q", messageText)
	}
	if codeText != "fmt.Println(\"hello\")" {
		t.Fatalf("expected code content, got %q", codeText)
	}
}
