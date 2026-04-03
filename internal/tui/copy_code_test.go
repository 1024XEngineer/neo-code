package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/provider"
)

func TestExtractFencedCodeBlocks(t *testing.T) {
	content := "before\n```go\nfmt.Println(1)\n```\nmid\n```bash\necho hi\n```\nafter"
	blocks := extractFencedCodeBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 code blocks, got %d", len(blocks))
	}
	if blocks[0] != "fmt.Println(1)" {
		t.Fatalf("expected first code block to strip language tag, got %q", blocks[0])
	}
	if blocks[1] != "echo hi" {
		t.Fatalf("expected second code block to strip language tag, got %q", blocks[1])
	}
}

func TestExtractFencedCodeBlocksWithoutLanguageKeepsFirstLine(t *testing.T) {
	content := "before\n```\nSELECT\nFROM users;\n```\nafter"
	blocks := extractFencedCodeBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 code block, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0], "SELECT") || !strings.Contains(blocks[0], "FROM users;") {
		t.Fatalf("expected full code block content, got %q", blocks[0])
	}
}

func TestExtractFencedCodeBlocksFromIndentedMarkdown(t *testing.T) {
	content := "说明：\n\n    package main\n    import \"fmt\"\n\n结尾。"
	blocks := extractFencedCodeBlocks(content)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 code block from indented markdown, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0], "package main") || !strings.Contains(blocks[0], "import \"fmt\"") {
		t.Fatalf("expected extracted indented code block, got %q", blocks[0])
	}
}

func TestParseCopyCodeButtonID(t *testing.T) {
	id, startCol, endCol, ok := parseCopyCodeButton("[Copy code #12]")
	if !ok || id != 12 {
		t.Fatalf("expected id=12 parse success, got id=%d ok=%v", id, ok)
	}
	if startCol != 0 || endCol <= startCol {
		t.Fatalf("expected valid button range, got start=%d end=%d", startCol, endCol)
	}

	if _, _, _, ok := parseCopyCodeButton("no button"); ok {
		t.Fatalf("expected parse failure for non-button line")
	}
}

func TestRenderMessageBlockWithCopyAddsButtons(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	rendered, bindings := app.renderMessageBlockWithCopy(providerMessage(roleAssistant, "```go\nfmt.Println(1)\n```"), 80, 1)
	if !strings.Contains(rendered, "[Copy code #1]") {
		t.Fatalf("expected copy button in rendered message, got %q", rendered)
	}
	plain := stripANSI(rendered)
	if strings.Index(plain, "[Copy code #1]") > strings.Index(plain, "fmt.Println(1)") {
		t.Fatalf("expected copy button to render above code block, got %q", plain)
	}
	if len(bindings) != 1 || bindings[0].ID != 1 || bindings[0].Code != "fmt.Println(1)" {
		t.Fatalf("unexpected bindings: %+v", bindings)
	}
}

func TestRenderMessageBlockWithCopyPreservesCodeIndentation(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	content := "```go\nfunc main() {\n\tif true {\n\t\tprintln(\"ok\")\n\t}\n}\n```"
	_, bindings := app.renderMessageBlockWithCopy(providerMessage(roleAssistant, content), 80, 1)
	if len(bindings) != 1 {
		t.Fatalf("expected one copy binding, got %+v", bindings)
	}
	if !strings.Contains(bindings[0].Code, "\tif true {") || !strings.Contains(bindings[0].Code, "\t\tprintln(\"ok\")") {
		t.Fatalf("expected indentation preserved in copied code, got %q", bindings[0].Code)
	}
}

func TestRenderMessageBlockWithCopyAddsButtonsForIndentedCode(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	content := "说明：\n\n    package main\n    import \"fmt\""
	rendered, bindings := app.renderMessageBlockWithCopy(providerMessage(roleAssistant, content), 80, 1)
	if !strings.Contains(stripANSI(rendered), "[Copy code #1]") {
		t.Fatalf("expected copy button for indented markdown code, got %q", rendered)
	}
	if len(bindings) != 1 || !strings.Contains(bindings[0].Code, "package main") {
		t.Fatalf("unexpected bindings for indented markdown code: %+v", bindings)
	}
}

func TestTranscriptMouseClickCopiesCodeBlock(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	originalClipboardWrite := clipboardWriteAll
	t.Cleanup(func() { clipboardWriteAll = originalClipboardWrite })

	copied := ""
	clipboardWriteAll = func(text string) error {
		copied = text
		return nil
	}

	app.width = 128
	app.height = 40
	app.activeMessages = []provider.Message{
		{Role: roleAssistant, Content: "```go\nfmt.Println(1)\n```"},
	}
	app.resizeComponents()
	app.rebuildTranscript()

	x, y, _, _ := app.transcriptBounds()
	lines := strings.Split(stripANSI(app.transcript.View()), "\n")
	targetY := -1
	targetX := -1
	for i, line := range lines {
		col := strings.Index(line, "[Copy code #1]")
		if col >= 0 {
			targetY = i
			targetX = col
			break
		}
	}
	if targetY < 0 || targetX < 0 {
		t.Fatalf("expected visible copy button in transcript view, got %q", app.transcript.View())
	}

	if handled := app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + targetX + 1,
		Y:      y + targetY,
		Button: tea.MouseButtonLeft,
	}); !handled {
		t.Fatalf("expected mouse press on copy button to be handled")
	}
	if copied != "" {
		t.Fatalf("expected press phase not to copy yet, got %q", copied)
	}

	if handled := app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + targetX + 1,
		Y:      y + targetY,
		Action: tea.MouseActionRelease,
		Type:   tea.MouseRelease,
	}); !handled {
		t.Fatalf("expected mouse release on copy button to be handled")
	}

	if copied != "fmt.Println(1)" {
		t.Fatalf("expected copied code block content, got %q", copied)
	}
	if !strings.Contains(app.state.StatusText, "Copied code block #1") {
		t.Fatalf("expected copy success status, got %q", app.state.StatusText)
	}

	if handled := app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + 60,
		Y:      y + targetY,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionRelease,
		Type:   tea.MouseRelease,
	}); handled {
		t.Fatalf("expected release outside copy button text to be ignored")
	}

	if handled := app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + targetX + 1,
		Y:      y + targetY,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
		Type:   tea.MouseMotion,
	}); handled {
		t.Fatalf("expected hover/motion over copy button to be ignored")
	}
}

func TestTranscriptMouseCopyFailureSetsError(t *testing.T) {
	manager := newTestConfigManager(t)
	runtime := newStubRuntime()
	app, err := New(nil, manager, runtime, newTestProviderService(t, manager))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	originalClipboardWrite := clipboardWriteAll
	t.Cleanup(func() { clipboardWriteAll = originalClipboardWrite })

	clipboardWriteAll = func(text string) error {
		return errors.New("clipboard unavailable")
	}

	app.width = 128
	app.height = 40
	app.activeMessages = []provider.Message{
		{Role: roleAssistant, Content: "```txt\nhello\n```"},
	}
	app.resizeComponents()
	app.rebuildTranscript()

	x, y, _, _ := app.transcriptBounds()
	lines := strings.Split(stripANSI(app.transcript.View()), "\n")
	targetY := -1
	targetX := -1
	for i, line := range lines {
		col := strings.Index(line, "[Copy code #1]")
		if col >= 0 {
			targetY = i
			targetX = col
			break
		}
	}
	if targetY < 0 || targetX < 0 {
		t.Fatalf("expected visible copy button in transcript view")
	}

	if handled := app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + targetX + 1,
		Y:      y + targetY,
		Button: tea.MouseButtonLeft,
	}); !handled {
		t.Fatalf("expected mouse press on copy button to be handled")
	}
	if handled := app.handleTranscriptMouse(tea.MouseMsg{
		X:      x + targetX + 1,
		Y:      y + targetY,
		Action: tea.MouseActionRelease,
		Type:   tea.MouseRelease,
	}); !handled {
		t.Fatalf("expected mouse release on copy button to be handled")
	}

	if app.state.StatusText != statusCodeCopyError || app.state.ExecutionError == "" {
		t.Fatalf("expected copy failure status/error, got status=%q err=%q", app.state.StatusText, app.state.ExecutionError)
	}
}

func providerMessage(role, content string) provider.Message {
	return provider.Message{Role: role, Content: content}
}
