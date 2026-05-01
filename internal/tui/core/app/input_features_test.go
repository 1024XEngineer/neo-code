package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestParseInlineImagePathToken(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	app.state.CurrentWorkdir = root

	relative, ok := app.parseInlineImagePathToken(`@image:"charts/sales q1.png"`)
	if !ok {
		t.Fatalf("expected quoted relative token to parse")
	}
	if relative != filepath.Join(root, filepath.FromSlash("charts/sales q1.png")) {
		t.Fatalf("unexpected resolved path: %q", relative)
	}

	absolutePath := filepath.Join(root, "abs.png")
	absolute, ok := app.parseInlineImagePathToken("@image:" + absolutePath)
	if !ok || absolute != absolutePath {
		t.Fatalf("expected absolute token to pass through, got %q ok=%v", absolute, ok)
	}

	if _, ok := app.parseInlineImagePathToken("@image:notes.txt"); ok {
		t.Fatalf("expected non-image token to be rejected")
	}
	app.state.CurrentWorkdir = ""
	if _, ok := app.parseInlineImagePathToken("@image:relative.png"); ok {
		t.Fatalf("expected missing workdir to reject relative token")
	}
	if _, ok := app.parseInlineImagePathToken("not-image-token"); ok {
		t.Fatalf("expected invalid token to be rejected")
	}
}

func TestParseInlineImageReferenceAtBranches(t *testing.T) {
	if _, _, ok := parseInlineImageReferenceAt("x@image:a.png", 1); ok {
		t.Fatalf("expected token without boundary whitespace to be rejected")
	}
	path, end, ok := parseInlineImageReferenceAt(`@image:folder\ with\ space.png next`, 0)
	if !ok {
		t.Fatalf("expected escaped-space token to parse")
	}
	if path != "folder with space.png" || end <= 0 {
		t.Fatalf("unexpected escaped path parse result path=%q end=%d", path, end)
	}
	if _, _, ok := parseInlineImageReferenceAt(`@image:""`, 0); ok {
		t.Fatalf("expected empty quoted token to fail")
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
	originalReadText := readClipboardText
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
		readClipboardText = originalReadText
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
	originalReadText := readClipboardText
	originalSave := saveClipboardImageToTempFile
	defer func() {
		readClipboardImage = originalRead
		readClipboardText = originalReadText
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

func TestAddImageFromClipboardDoesNotFallbackToClipboardPathText(t *testing.T) {
	app, _ := newTestApp(t)
	root := t.TempDir()
	imagePath := filepath.Join(root, "clip.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	originalRead := readClipboardImage
	originalReadText := readClipboardText
	originalSave := saveClipboardImageToTempFile
	defer func() {
		readClipboardImage = originalRead
		readClipboardText = originalReadText
		saveClipboardImageToTempFile = originalSave
	}()

	readClipboardImage = func() ([]byte, error) { return nil, errors.New("image clipboard unavailable") }
	readClipboardText = func() (string, error) { return imagePath, nil }
	saveClipboardImageToTempFile = func(data []byte, prefix string) (string, error) {
		t.Fatalf("expected text clipboard path not to trigger raw image save")
		return "", nil
	}

	if err := app.addImageFromClipboard(); err == nil {
		t.Fatalf("expected addImageFromClipboard to reject text-path fallback")
	}
	if app.getImageAttachmentCount() != 0 {
		t.Fatalf("expected no image attachment when clipboard has path text only")
	}
}

func TestClipboardImagePathFromTextSupportsFileURL(t *testing.T) {
	root := t.TempDir()
	rawPath := filepath.Join(root, "space name.png")
	fileURL := "file:///" + strings.ReplaceAll(rawPath, "\\", "/")
	fileURL = strings.ReplaceAll(fileURL, " ", "%20")

	originalReadText := readClipboardText
	defer func() { readClipboardText = originalReadText }()
	readClipboardText = func() (string, error) { return fileURL, nil }

	path, ok := clipboardImagePathFromText("")
	if !ok {
		t.Fatalf("expected file:// clipboard path to be recognized")
	}
	if filepath.Clean(path) != filepath.Clean(rawPath) {
		t.Fatalf("expected decoded file URL path %q, got %q", rawPath, path)
	}
}

func TestClipboardFileReferencePathsFromText(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "docs", "a.txt")
	second := filepath.Join(root, "README.md")
	if err := os.MkdirAll(filepath.Dir(first), 0o755); err != nil {
		t.Fatalf("mkdir first: %v", err)
	}
	if err := os.WriteFile(first, []byte("a"), 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(second, []byte("b"), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}

	paths, ok := clipboardFileReferencePathsFromText(first+"\n"+second, root)
	if !ok {
		t.Fatalf("expected valid clipboard file paths")
	}
	if len(paths) != 2 {
		t.Fatalf("expected two resolved paths, got %v", paths)
	}
}

func TestClipboardFileReferencePathsFromTextRejectsMixedInput(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "a.txt")
	if err := os.WriteFile(valid, []byte("a"), 0o644); err != nil {
		t.Fatalf("write valid file: %v", err)
	}

	if _, ok := clipboardFileReferencePathsFromText(valid+"\nhello world", root); ok {
		t.Fatalf("expected mixed clipboard content to stay as plain text")
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
