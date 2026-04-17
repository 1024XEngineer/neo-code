package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
)

func TestTokenAndReferenceParsing(t *testing.T) {
	start, end, token, ok := tokenRange("  @file/path", tokenSelectorFirst)
	if !ok || start != 2 || end != len("  @file/path") || token != "@file/path" {
		t.Fatalf("unexpected first token parse: start=%d end=%d token=%q ok=%v", start, end, token, ok)
	}

	start, end, token, ok = tokenRange("hello @image:/tmp/a.png", tokenSelectorLast)
	if !ok || token != "@image:/tmp/a.png" || start <= 0 || end <= start {
		t.Fatalf("unexpected last token parse: start=%d end=%d token=%q ok=%v", start, end, token, ok)
	}

	_, _, _, ok = currentReferenceToken("hello world")
	if ok {
		t.Fatalf("expected non-reference token to be rejected")
	}

	_, _, token, ok = currentReferenceToken("x @a/b.txt")
	if !ok || token != "@a/b.txt" {
		t.Fatalf("expected file reference token, got token=%q ok=%v", token, ok)
	}

	if !isImageReferenceInput("@image:/tmp/p.png") {
		t.Fatalf("expected image reference input recognized")
	}
	if got := extractImageReference("@image:/tmp/p.png"); got != "/tmp/p.png" {
		t.Fatalf("unexpected image reference extraction: %q", got)
	}
}

func TestApplyFileReference(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	app.state.CurrentWorkdir = root
	inside := filepath.Join(root, "docs", "a.md")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(inside, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := app.applyFileReference(inside); err != nil {
		t.Fatalf("applyFileReference() error = %v", err)
	}
	if got := app.input.Value(); !strings.Contains(got, "@docs/a.md") {
		t.Fatalf("expected relative file reference, got %q", got)
	}

	app.input.SetValue("prefix @old/ref")
	if err := app.applyFileReference(inside); err != nil {
		t.Fatalf("applyFileReference replace() error = %v", err)
	}
	if got := app.input.Value(); strings.Contains(got, "@old/ref") || !strings.Contains(got, "@docs/a.md") {
		t.Fatalf("expected active token replaced, got %q", got)
	}
}

func TestApplyFileReferenceBranches(t *testing.T) {
	app, _ := newTestApp(t)
	if err := app.applyFileReference("   "); err == nil {
		t.Fatalf("expected empty file path error")
	}

	root := t.TempDir()
	app.state.CurrentWorkdir = filepath.Join(root, "workdir")
	inside := filepath.Join(app.state.CurrentWorkdir, "a.txt")
	outside := filepath.Join(root, "outside.txt")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatalf("mkdir inside: %v", err)
	}
	if err := os.WriteFile(inside, []byte("a"), 0o644); err != nil {
		t.Fatalf("write inside: %v", err)
	}
	if err := os.WriteFile(outside, []byte("b"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	if err := app.applyFileReference(outside); err != nil {
		t.Fatalf("apply outside reference error: %v", err)
	}
	if !strings.Contains(app.input.Value(), "@") {
		t.Fatalf("expected file reference token to be inserted")
	}
}

func TestImageAttachmentLifecycle(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	imagePath := filepath.Join(root, "test.png")
	if err := os.WriteFile(imagePath, []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	if err := app.addImageAttachment(imagePath); err != nil {
		t.Fatalf("addImageAttachment() error = %v", err)
	}
	if app.getImageAttachmentCount() != 1 {
		t.Fatalf("expected one attachment, got %d", app.getImageAttachmentCount())
	}
	if !app.hasImageAttachments() {
		t.Fatalf("expected hasImageAttachments() true")
	}
	if _, err := app.loadImageAttachmentData(0); err != nil {
		t.Fatalf("loadImageAttachmentData() error = %v", err)
	}

	if err := app.removeImageAttachment(0); err != nil {
		t.Fatalf("removeImageAttachment() error = %v", err)
	}
	if app.getImageAttachmentCount() != 0 {
		t.Fatalf("expected no attachments after remove")
	}
	if err := app.removeImageAttachment(0); err == nil {
		t.Fatalf("expected out-of-range remove error")
	}
}

func TestAddImageAttachmentLimit(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()

	for i := 0; i < maxImageAttachments; i++ {
		path := filepath.Join(root, fmt.Sprintf("%d.png", i))
		if err := os.WriteFile(path, []byte("png"), 0o644); err != nil {
			t.Fatalf("write image: %v", err)
		}
		if err := app.addImageAttachment(path); err != nil {
			t.Fatalf("addImageAttachment(%d) error = %v", i, err)
		}
	}

	over := filepath.Join(root, "over.png")
	if err := os.WriteFile(over, []byte("png"), 0o644); err != nil {
		t.Fatalf("write over image: %v", err)
	}
	if err := app.addImageAttachment(over); err == nil {
		t.Fatalf("expected attachment limit error")
	}
}

func TestApplyImageReference(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	imagePath := filepath.Join(root, "ok.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := app.applyImageReference("@image:" + imagePath); err != nil {
		t.Fatalf("applyImageReference() error = %v", err)
	}
	if app.getImageAttachmentCount() != 1 {
		t.Fatalf("expected one attachment after applyImageReference")
	}
	if err := app.applyImageReference("not-an-image-reference"); err == nil {
		t.Fatalf("expected invalid image reference error")
	}
}

func TestAbsorbInlineImageReferences(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	app.state.CurrentWorkdir = root

	imagePath := filepath.Join(root, "chart.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	normalized, absorbed, err := app.absorbInlineImageReferences("请分析 @image:chart.png 趋势")
	if err != nil {
		t.Fatalf("absorbInlineImageReferences() error = %v", err)
	}
	if absorbed != 1 {
		t.Fatalf("expected one absorbed image, got %d", absorbed)
	}
	if normalized != "请分析  趋势" {
		t.Fatalf("unexpected normalized text: %q", normalized)
	}
	if app.getImageAttachmentCount() != 1 {
		t.Fatalf("expected one pending image attachment, got %d", app.getImageAttachmentCount())
	}
}

func TestAbsorbInlineImageReferencesRequiresExplicitPrefix(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	app.state.CurrentWorkdir = root

	normalized, absorbed, err := app.absorbInlineImageReferences("请分析 @chart.png 趋势")
	if err != nil {
		t.Fatalf("absorbInlineImageReferences() error = %v", err)
	}
	if absorbed != 0 {
		t.Fatalf("expected absorbed image count to be 0, got %d", absorbed)
	}
	if normalized != "请分析 @chart.png 趋势" {
		t.Fatalf("unexpected normalized text: %q", normalized)
	}
	if app.getImageAttachmentCount() != 0 {
		t.Fatalf("expected no pending image attachments")
	}
}

func TestAbsorbInlineImageReferencesKeepsNonImageToken(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	app.state.CurrentWorkdir = root

	normalized, absorbed, err := app.absorbInlineImageReferences("查看 @README.md 内容")
	if err != nil {
		t.Fatalf("absorbInlineImageReferences() error = %v", err)
	}
	if absorbed != 0 {
		t.Fatalf("expected absorbed image count to be 0, got %d", absorbed)
	}
	if normalized != "查看 @README.md 内容" {
		t.Fatalf("unexpected normalized text: %q", normalized)
	}
	if app.getImageAttachmentCount() != 0 {
		t.Fatalf("expected no pending image attachments")
	}
}

func TestAbsorbInlineImageReferencesDoesNotRequireFileExistenceInTUI(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.CurrentWorkdir = t.TempDir()

	normalized, absorbed, err := app.absorbInlineImageReferences("处理 @image:not-exist.png")
	if err != nil {
		t.Fatalf("absorbInlineImageReferences() error = %v", err)
	}
	if absorbed != 1 {
		t.Fatalf("expected one absorbed image, got %d", absorbed)
	}
	if normalized != "处理" {
		t.Fatalf("unexpected normalized text: %q", normalized)
	}
	if app.getImageAttachmentCount() != 1 {
		t.Fatalf("expected one pending attachment")
	}
	if app.getImageAttachments()[0].MimeType != "" {
		t.Fatalf("expected mime type to stay empty before runtime/session validation")
	}
}

func TestAbsorbInlineImageReferencesPreservesWhitespaceLayout(t *testing.T) {
	app, _ := newTestApp(t)
	app.state.CurrentWorkdir = t.TempDir()

	normalized, absorbed, err := app.absorbInlineImageReferences("A  @image:x.png\nB\t @image:y.jpg  C")
	if err != nil {
		t.Fatalf("absorbInlineImageReferences() error = %v", err)
	}
	if absorbed != 2 {
		t.Fatalf("expected absorbed image count to be 2, got %d", absorbed)
	}
	if normalized != "A  \nB\t   C" {
		t.Fatalf("unexpected normalized text: %q", normalized)
	}
	if app.getImageAttachmentCount() != 2 {
		t.Fatalf("expected two pending image attachments")
	}
}

func TestAbsorbInlineImageReferencesSupportsQuotedPathWithSpaces(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	app.state.CurrentWorkdir = root

	normalized, absorbed, err := app.absorbInlineImageReferences(`请分析 @image:"charts/sales q1.png" 趋势`)
	if err != nil {
		t.Fatalf("absorbInlineImageReferences() error = %v", err)
	}
	if absorbed != 1 {
		t.Fatalf("expected absorbed image count to be 1, got %d", absorbed)
	}
	if normalized != "请分析  趋势" {
		t.Fatalf("unexpected normalized text: %q", normalized)
	}
	if app.getImageAttachmentCount() != 1 {
		t.Fatalf("expected one pending image attachment")
	}
	if !strings.HasSuffix(app.getImageAttachments()[0].Path, filepath.FromSlash("charts/sales q1.png")) {
		t.Fatalf("unexpected queued path: %q", app.getImageAttachments()[0].Path)
	}
}

func TestGetAndClearImageAttachments(t *testing.T) {
	app, _ := newTestApp(t)
	app.pendingImageAttachments = []pendingImageAttachment{
		{Name: "a.png", Path: "/tmp/a.png", MimeType: "image/png", Size: 1},
	}
	if len(app.getImageAttachments()) != 1 {
		t.Fatalf("expected one attachment from getter")
	}
	app.clearImageAttachments()
	if len(app.getImageAttachments()) != 0 {
		t.Fatalf("expected no attachments after clear")
	}
}

func TestLoadImageAttachmentDataInvalidIndex(t *testing.T) {
	app, _ := newTestApp(t)
	if _, err := app.loadImageAttachmentData(0); err == nil {
		t.Fatalf("expected invalid attachment index error")
	}
}

func TestAddImageFromClipboardUnsupported(t *testing.T) {
	app, _ := newTestApp(t)
	if err := app.addImageFromClipboard(); err == nil {
		t.Fatalf("expected unsupported clipboard image error")
	}
}

func TestAddImageFromClipboardSuccess(t *testing.T) {
	app, _ := newTestApp(t)
	originalRead := readClipboardImage
	originalSave := saveClipboardImageToTempFile
	readClipboardImage = func() ([]byte, error) {
		return []byte("image-bytes"), nil
	}
	saveClipboardImageToTempFile = func(data []byte, prefix string) (string, error) {
		path := filepath.Join(t.TempDir(), "clipboard.png")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write temp clipboard image: %v", err)
		}
		return path, nil
	}
	defer func() {
		readClipboardImage = originalRead
		saveClipboardImageToTempFile = originalSave
	}()

	if err := app.addImageFromClipboard(); err != nil {
		t.Fatalf("addImageFromClipboard() error = %v", err)
	}
	if app.getImageAttachmentCount() != 1 {
		t.Fatalf("expected one clipboard image attachment")
	}
}

func TestAddImageFromClipboardBranches(t *testing.T) {
	app, _ := newTestApp(t)
	originalRead := readClipboardImage
	originalSave := saveClipboardImageToTempFile
	defer func() {
		readClipboardImage = originalRead
		saveClipboardImageToTempFile = originalSave
	}()

	readClipboardImage = func() ([]byte, error) { return nil, nil }
	if err := app.addImageFromClipboard(); err == nil {
		t.Fatalf("expected no image in clipboard error")
	}

	readClipboardImage = func() ([]byte, error) { return []byte("x"), nil }
	saveClipboardImageToTempFile = func(data []byte, prefix string) (string, error) {
		return "", errors.New("save failed")
	}
	if err := app.addImageFromClipboard(); err == nil {
		t.Fatalf("expected save failure error")
	}
}

func TestExecuteWorkspaceCommand(t *testing.T) {
	app, _ := newTestApp(t)
	original := workspaceCommandExecutor
	workspaceCommandExecutor = func(ctx context.Context, cfg config.Config, workdir string, command string) (string, error) {
		if command != "echo hi" {
			t.Fatalf("unexpected command: %q", command)
		}
		return "ok", nil
	}
	defer func() { workspaceCommandExecutor = original }()

	command, output, err := executeWorkspaceCommand(context.Background(), app.configManager, app.state.CurrentWorkdir, "& echo hi")
	if err != nil {
		t.Fatalf("executeWorkspaceCommand() error = %v", err)
	}
	if command != "echo hi" || output != "ok" {
		t.Fatalf("unexpected execute result command=%q output=%q", command, output)
	}

	if _, _, err := executeWorkspaceCommand(context.Background(), app.configManager, app.state.CurrentWorkdir, "& "); err == nil {
		t.Fatalf("expected invalid workspace command error")
	}
}

func TestDefaultWorkspaceCommandExecutor(t *testing.T) {
	cfg := config.Config{Workdir: t.TempDir(), Shell: "bash", ToolTimeoutSec: 1}
	if _, err := defaultWorkspaceCommandExecutor(context.Background(), cfg, cfg.Workdir, ""); err == nil {
		t.Fatalf("expected empty command to fail")
	}
}

func TestRunWorkspaceCommandCmd(t *testing.T) {
	app, _ := newTestApp(t)
	original := workspaceCommandExecutor
	workspaceCommandExecutor = func(ctx context.Context, cfg config.Config, workdir string, command string) (string, error) {
		return "done", nil
	}
	defer func() { workspaceCommandExecutor = original }()

	cmd := runWorkspaceCommand(app.configManager, app.state.CurrentWorkdir, "& echo hi")
	if cmd == nil {
		t.Fatalf("expected workspace command cmd")
	}
	msg := cmd()
	result, ok := msg.(workspaceCommandResultMsg)
	if !ok {
		t.Fatalf("expected workspaceCommandResultMsg, got %T", msg)
	}
	if result.Command != "echo hi" || result.Output != "done" || result.Err != nil {
		t.Fatalf("unexpected workspace result: %+v", result)
	}
}

func TestUpdateSendWithImageAttachmentsRunsThroughPreparePipeline(t *testing.T) {
	app, runtime := newTestApp(t)
	root := t.TempDir()
	imagePath := filepath.Join(root, "queued.png")
	if err := os.WriteFile(imagePath, []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	if err := app.addImageAttachment(imagePath); err != nil {
		t.Fatalf("addImageAttachment() error = %v", err)
	}
	app.input.SetValue("hello")
	app.state.InputText = "hello"

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	app = model.(App)
	if app.hasImageAttachments() {
		t.Fatalf("expected attachments cleared after send")
	}
	if !app.state.IsAgentRunning {
		t.Fatalf("expected image send to enter running state")
	}
	if app.state.StatusText != statusThinking {
		t.Fatalf("unexpected status text: %q", app.state.StatusText)
	}
	if len(runtime.prepareInputs) != 1 {
		t.Fatalf("expected one prepare input, got %+v", runtime.prepareInputs)
	}
	if len(runtime.prepareInputs[0].Images) != 1 || runtime.prepareInputs[0].Images[0].MimeType != "" {
		t.Fatalf("expected one queued image in prepare input, got %+v", runtime.prepareInputs[0].Images)
	}
	if len(runtime.runInputs) != 1 {
		t.Fatalf("expected one runtime input after prepare, got %+v", runtime.runInputs)
	}
}
