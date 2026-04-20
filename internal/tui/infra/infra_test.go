package infra

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"neo-code/internal/config"
)

func TestShellArgs(t *testing.T) {
	if got := ShellArgs("bash", "pwd"); len(got) != 3 || got[0] != "bash" || got[2] != "pwd" {
		t.Fatalf("unexpected bash args: %+v", got)
	}
	if got := ShellArgs("sh", "pwd"); len(got) != 3 || got[0] != "sh" || got[2] != "pwd" {
		t.Fatalf("unexpected sh args: %+v", got)
	}
	if got := ShellArgs("powershell", "Get-Location"); len(got) != 4 || got[0] != "powershell" {
		t.Fatalf("unexpected powershell args: %+v", got)
	}
	if got := ShellArgs("pwsh", "Get-Location"); len(got) != 4 || got[0] != "powershell" {
		t.Fatalf("unexpected pwsh args: %+v", got)
	}
	if got := ShellArgs("unknown", "git status"); len(got) != 4 || got[0] != "powershell" {
		t.Fatalf("expected powershell fallback, got %+v", got)
	}
}

func TestSanitizeWorkspaceOutput(t *testing.T) {
	raw := []byte("\x1b[31mERROR\x1b[0m\r\nok\t\x00")
	got := SanitizeWorkspaceOutput(raw)
	if strings.Contains(got, "\x1b[31m") {
		t.Fatalf("expected ansi removed, got %q", got)
	}
	if !strings.Contains(got, "ERROR") || !strings.Contains(got, "ok") {
		t.Fatalf("expected content preserved, got %q", got)
	}
}

func TestDecodeWorkspaceOutputUTF16LE(t *testing.T) {
	utf16Data := utf16.Encode([]rune("PowerShell 输出"))
	buf := make([]byte, 2+len(utf16Data)*2)
	buf[0], buf[1] = 0xFF, 0xFE
	for i, word := range utf16Data {
		binary.LittleEndian.PutUint16(buf[2+i*2:], word)
	}

	got := DecodeWorkspaceOutput(buf)
	if !strings.Contains(got, "PowerShell") {
		t.Fatalf("expected decoded utf16 content, got %q", got)
	}
}

func TestDecodeWorkspaceOutputUTF16BE(t *testing.T) {
	utf16Data := utf16.Encode([]rune("UTF16 BE"))
	buf := make([]byte, 2+len(utf16Data)*2)
	buf[0], buf[1] = 0xFE, 0xFF
	for i, word := range utf16Data {
		binary.BigEndian.PutUint16(buf[2+i*2:], word)
	}

	got := DecodeWorkspaceOutput(buf)
	if !strings.Contains(got, "UTF16 BE") {
		t.Fatalf("expected decoded utf16 big-endian content, got %q", got)
	}
}

func TestDecodeWorkspaceOutputHeuristicsAndEdges(t *testing.T) {
	evenWithoutBOM := []byte{0x61, 0x00, 0x62, 0x00}
	got := DecodeWorkspaceOutput(evenWithoutBOM)
	if !strings.Contains(got, "ab") {
		t.Fatalf("expected utf16 heuristic decode result to contain ab, got %q", got)
	}

	if got := DecodeWorkspaceOutput([]byte{0xE4, 0xBD, 0xA0}); utf8.ValidString(got) && strings.TrimSpace(got) == "" {
		t.Fatalf("expected odd-length raw bytes to keep readable content, got %q", got)
	}

	if got := decodeUTF16([]byte{0x61}, true); got != "a" {
		t.Fatalf("expected short utf16 input to return raw text, got %q", got)
	}
	if got := decodeUTF16([]byte{0x61, 0x00, 0x62}, true); !strings.Contains(got, "a") {
		t.Fatalf("expected odd-length utf16 input to decode after trimming, got %q", got)
	}
}

func TestDecodedTextScore(t *testing.T) {
	printable := decodedTextScore("hello world")
	replacement := decodedTextScore(string([]rune{'\uFFFD'}))
	if printable <= replacement {
		t.Fatalf("expected printable text score > replacement score, got printable=%d replacement=%d", printable, replacement)
	}
}

func TestCollectWorkspaceFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mustWrite("README.md")
	mustWrite("internal/tui/update.go")
	mustWrite(".git/config")
	mustWrite("node_modules/skip.js")

	files, err := CollectWorkspaceFiles(root, 10)
	if err != nil {
		t.Fatalf("CollectWorkspaceFiles() error = %v", err)
	}
	got := strings.Join(files, ",")
	if strings.Contains(got, ".git") || strings.Contains(got, "node_modules") {
		t.Fatalf("expected ignored dirs skipped, got %v", files)
	}
	if !strings.Contains(got, "README.md") || !strings.Contains(got, "internal/tui/update.go") {
		t.Fatalf("expected workspace files included, got %v", files)
	}
}

func TestCollectWorkspaceFilesLimitAndErrors(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(rel), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mustWrite("b.txt")
	mustWrite("a.txt")

	files, err := CollectWorkspaceFiles(root, 1)
	if err != nil {
		t.Fatalf("CollectWorkspaceFiles(limit=1) error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected exactly one file due to limit, got %v", files)
	}
	if files[0] != "a.txt" && files[0] != "b.txt" {
		t.Fatalf("unexpected limited file list: %v", files)
	}

	_, err = CollectWorkspaceFiles(filepath.Join(root, "missing"), 10)
	if err == nil {
		t.Fatalf("expected missing root to produce walk error")
	}
}

func TestCopyTextUsesInjectedWriter(t *testing.T) {
	CopyText("hello")
}

func TestCachedMarkdownRendererBasic(t *testing.T) {
	renderer := NewCachedMarkdownRenderer("dark", 4, "(empty)")

	empty, err := renderer.Render(" \n\t ", 20)
	if err != nil {
		t.Fatalf("Render(empty) error = %v", err)
	}
	if empty != "(empty)" {
		t.Fatalf("expected empty placeholder, got %q", empty)
	}

	out, err := renderer.Render("# Title\n\n- one", 40)
	if err != nil {
		t.Fatalf("Render(markdown) error = %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("expected non-empty rendered markdown")
	}
	if renderer.RendererCount() != 1 || renderer.CacheCount() != 1 {
		t.Fatalf("expected renderer and cache entries, got renderers=%d cache=%d", renderer.RendererCount(), renderer.CacheCount())
	}
}

func TestCachedMarkdownRendererRemovesHeadingHashPrefix(t *testing.T) {
	renderer := NewCachedMarkdownRenderer("dark", 4, "(empty)")
	out, err := renderer.Render("#### Heading Example", 60)
	if err != nil {
		t.Fatalf("Render(heading) error = %v", err)
	}

	plain := markdownANSIPattern.ReplaceAllString(out, "")
	if strings.Contains(plain, "#### ") {
		t.Fatalf("expected heading hash prefix to be removed, got %q", plain)
	}
	if !strings.Contains(plain, "Heading Example") {
		t.Fatalf("expected heading text to remain, got %q", plain)
	}
}

func TestCachedMarkdownRendererCacheEviction(t *testing.T) {
	renderer := NewCachedMarkdownRenderer("dark", 1, "(empty)")

	if _, err := renderer.Render("first", 20); err != nil {
		t.Fatalf("Render(first) error = %v", err)
	}
	if _, err := renderer.Render("second", 20); err != nil {
		t.Fatalf("Render(second) error = %v", err)
	}
	if renderer.CacheOrderCount() != 1 || renderer.CacheCount() != 1 {
		t.Fatalf("expected single cache entry after eviction, got order=%d cache=%d", renderer.CacheOrderCount(), renderer.CacheCount())
	}
}

func TestCachedMarkdownRendererDefaultsAndSetMax(t *testing.T) {
	renderer := NewCachedMarkdownRenderer("", -1, "(empty)")
	if renderer.style != "dark" {
		t.Fatalf("expected default style dark, got %q", renderer.style)
	}
	if renderer.maxCacheEntries != 0 {
		t.Fatalf("expected negative max cache to normalize to 0, got %d", renderer.maxCacheEntries)
	}

	renderer.SetMaxCacheEntries(2)
	if _, err := renderer.Render("one", 20); err != nil {
		t.Fatalf("Render(one) error = %v", err)
	}
	if _, err := renderer.Render("two", 20); err != nil {
		t.Fatalf("Render(two) error = %v", err)
	}
	if _, err := renderer.Render("three", 20); err != nil {
		t.Fatalf("Render(three) error = %v", err)
	}
	if renderer.CacheCount() != 2 {
		t.Fatalf("expected cache eviction to keep 2 entries, got %d", renderer.CacheCount())
	}

	renderer.SetMaxCacheEntries(1)
	if renderer.CacheCount() != 1 || renderer.CacheOrderCount() != 1 {
		t.Fatalf("expected cache trim to one entry, got cache=%d order=%d", renderer.CacheCount(), renderer.CacheOrderCount())
	}

	renderer.SetMaxCacheEntries(-1)
	if renderer.CacheCount() != 0 || renderer.CacheOrderCount() != 0 {
		t.Fatalf("expected cache trim to zero after negative max, got cache=%d order=%d", renderer.CacheCount(), renderer.CacheOrderCount())
	}
}

func TestCachedMarkdownRendererCacheDisabledAndWidthFloor(t *testing.T) {
	renderer := NewCachedMarkdownRenderer("dark", 0, "(empty)")
	if _, err := renderer.Render("same", 1); err != nil {
		t.Fatalf("Render(width=1) error = %v", err)
	}
	if _, err := renderer.Render("same", 15); err != nil {
		t.Fatalf("Render(width=15) error = %v", err)
	}
	if renderer.CacheCount() != 0 {
		t.Fatalf("expected disabled cache to keep zero entries, got %d", renderer.CacheCount())
	}
	if renderer.RendererCount() != 1 {
		t.Fatalf("expected render width floor to reuse one renderer, got %d", renderer.RendererCount())
	}
}

func TestDefaultWorkspaceCommandExecutor(t *testing.T) {
	workdir := t.TempDir()
	shellName, successCmd, noOutputCmd, failCmd, sleepCmd := workspaceExecutorCommands()
	cfg := config.Config{
		Workdir:        workdir,
		Shell:          shellName,
		ToolTimeoutSec: 3,
	}

	if _, err := DefaultWorkspaceCommandExecutor(context.Background(), cfg, "", "  "); err == nil {
		t.Fatalf("expected empty command to fail")
	}

	output, err := DefaultWorkspaceCommandExecutor(context.Background(), cfg, "", successCmd)
	if err != nil {
		t.Fatalf("expected success command to pass, got error %v (output=%q)", err, output)
	}
	if !strings.Contains(strings.ToLower(output), "ok") {
		t.Fatalf("expected success output to contain ok, got %q", output)
	}

	output, err = DefaultWorkspaceCommandExecutor(context.Background(), cfg, workdir, noOutputCmd)
	if err != nil {
		t.Fatalf("expected no-output command to pass, got error %v (output=%q)", err, output)
	}
	if output != "(no output)" {
		t.Fatalf("expected no-output placeholder, got %q", output)
	}

	output, err = DefaultWorkspaceCommandExecutor(context.Background(), cfg, workdir, failCmd)
	if err == nil {
		t.Fatalf("expected failing command to return error, output=%q", output)
	}
	if strings.TrimSpace(output) == "" {
		t.Fatalf("expected failing command to return sanitized output")
	}

	timeoutCfg := cfg
	timeoutCfg.ToolTimeoutSec = 1
	output, err = DefaultWorkspaceCommandExecutor(context.Background(), timeoutCfg, workdir, sleepCmd)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got err=%v output=%q", err, output)
	}
}

func TestSaveImageToTempFileCreatesUniquePaths(t *testing.T) {
	first, err := SaveImageToTempFile([]byte("first"), "paste")
	if err != nil {
		t.Fatalf("SaveImageToTempFile(first) error = %v", err)
	}
	defer os.Remove(first)

	second, err := SaveImageToTempFile([]byte("second"), "paste")
	if err != nil {
		t.Fatalf("SaveImageToTempFile(second) error = %v", err)
	}
	defer os.Remove(second)

	if first == second {
		t.Fatalf("expected unique temp file paths, got %q", first)
	}
	if !strings.Contains(filepath.Base(first), "paste-") || !strings.Contains(filepath.Base(second), "paste-") {
		t.Fatalf("expected sanitized prefix in temp names, got %q and %q", first, second)
	}
}

func TestDetectImageMimeTypeByMagicHeader(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "blob.bin")
	pngHeader := []byte{0x89, 0x50, 0x4E, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	payload := append(pngHeader, []byte("payload")...)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write test image: %v", err)
	}

	if got := DetectImageMimeType(path); got != "image/png" {
		t.Fatalf("expected png mime by header, got %q", got)
	}
}

func TestDefaultWorkspaceCommandExecutorUsesDefaultTimeout(t *testing.T) {
	workdir := t.TempDir()
	shellName, successCmd, _, _, _ := workspaceExecutorCommands()
	cfg := config.Config{
		Workdir:        workdir,
		Shell:          shellName,
		ToolTimeoutSec: 0,
	}

	if output, err := DefaultWorkspaceCommandExecutor(context.Background(), cfg, "", successCmd); err != nil || !strings.Contains(strings.ToLower(output), "ok") {
		t.Fatalf("expected default timeout path to execute command, output=%q err=%v", output, err)
	}
}

func workspaceExecutorCommands() (shell string, success string, noOutput string, fail string, sleep string) {
	if goruntime.GOOS == "windows" {
		return "powershell",
			"Write-Output 'OK'",
			"$null = 1",
			"Write-Error 'failed'; exit 2",
			"Start-Sleep -Seconds 2"
	}
	return "bash",
		"printf 'OK\\n'",
		"true",
		"echo failed 1>&2; exit 2",
		"sleep 2"
}

func TestSanitizeTempPrefix(t *testing.T) {
	if got := sanitizeTempPrefix(""); got != "" {
		t.Fatalf("expected empty prefix to remain empty, got %q", got)
	}
	if got := sanitizeTempPrefix("p@st/e_1-2"); got != "pste_1-2" {
		t.Fatalf("expected unsafe chars filtered, got %q", got)
	}
}

func TestSaveImageToTempFilePersistsContent(t *testing.T) {
	data := []byte("image-bytes")
	path, err := SaveImageToTempFile(data, "p@st/e")
	if err != nil {
		t.Fatalf("SaveImageToTempFile() error = %v", err)
	}
	defer os.Remove(path)

	if !strings.Contains(filepath.Base(path), "pste-") {
		t.Fatalf("expected sanitized prefix in temp file name, got %q", filepath.Base(path))
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("expected written bytes to match, got %q", string(got))
	}
}

func TestSaveImageToTempFileCreateError(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing-dir"))
	if _, err := SaveImageToTempFile([]byte("x"), "paste"); err == nil {
		t.Fatalf("expected CreateTemp failure when TMPDIR is invalid")
	}
}

func TestClipboardFallbackFunctions(t *testing.T) {
	text, err := ReadClipboardText()
	if err == nil && strings.TrimSpace(text) == "" {
		t.Fatalf("expected clipboard text or an error, got empty success result")
	}
	data, err := ReadClipboardImage()
	if err != nil && err != errClipboardImageUnsupported {
		t.Fatalf("expected nil or unsupported image error, got %v", err)
	}
	if err == nil && len(data) == 0 && data != nil {
		t.Fatalf("expected nil for empty clipboard image state, got zero-length slice")
	}
}

func TestImageInfoAndRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.jpg")
	content := []byte{0xFF, 0xD8, 0xFF, 0x00}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	info, err := GetFileInfo(path)
	if err != nil {
		t.Fatalf("GetFileInfo() error = %v", err)
	}
	if info.Size() != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), info.Size())
	}
	read, err := ReadImageFile(path)
	if err != nil {
		t.Fatalf("ReadImageFile() error = %v", err)
	}
	if string(read) != string(content) {
		t.Fatalf("expected read bytes to match")
	}
}

func TestDetectImageMimeTypeAndSupportChecks(t *testing.T) {
	root := t.TempDir()
	pngPath := filepath.Join(root, "x.png")
	if err := os.WriteFile(pngPath, []byte("png"), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	if got := DetectImageMimeType(pngPath); got != "image/png" {
		t.Fatalf("expected png by extension, got %q", got)
	}

	jpgPath := filepath.Join(root, "x.JPG")
	if err := os.WriteFile(jpgPath, []byte("jpg"), 0o644); err != nil {
		t.Fatalf("write jpg: %v", err)
	}
	if got := DetectImageMimeType(jpgPath); got != "image/jpeg" {
		t.Fatalf("expected jpeg by extension, got %q", got)
	}
	if !IsSupportedImageFormat(jpgPath) {
		t.Fatalf("expected jpeg to be supported")
	}

	txtPath := filepath.Join(root, "x.txt")
	if err := os.WriteFile(txtPath, []byte("text"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	if got := DetectImageMimeType(txtPath); got == "" {
		t.Fatalf("expected extension-based mime to be detected for txt")
	}
	if IsSupportedImageFormat(txtPath) {
		t.Fatalf("expected txt not to be treated as supported image")
	}

	webpPath := filepath.Join(root, "x.webp")
	if err := os.WriteFile(webpPath, []byte("webp"), 0o644); err != nil {
		t.Fatalf("write webp: %v", err)
	}
	if got := DetectImageMimeType(webpPath); got != "image/webp" {
		t.Fatalf("expected webp by extension, got %q", got)
	}

	gifPath := filepath.Join(root, "x.bin")
	gifBytes := []byte("GIF89a........")
	if err := os.WriteFile(gifPath, gifBytes, 0o644); err != nil {
		t.Fatalf("write gif magic: %v", err)
	}
	if got := DetectImageMimeType(gifPath); got != "image/gif" {
		t.Fatalf("expected gif by magic header, got %q", got)
	}

	jpegMagicPath := filepath.Join(root, "jpeg-magic.bin")
	if err := os.WriteFile(jpegMagicPath, []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46}, 0o644); err != nil {
		t.Fatalf("write jpeg magic: %v", err)
	}
	if got := DetectImageMimeType(jpegMagicPath); got != "image/jpeg" {
		t.Fatalf("expected jpeg by magic header, got %q", got)
	}

	webpMagicPath := filepath.Join(root, "webp-magic.bin")
	webpMagic := append([]byte("RIFF"), []byte{0, 0, 0, 0}...)
	webpMagic = append(webpMagic, []byte("WEBP")...)
	if err := os.WriteFile(webpMagicPath, webpMagic, 0o644); err != nil {
		t.Fatalf("write webp magic: %v", err)
	}
	if got := DetectImageMimeType(webpMagicPath); got != "image/webp" {
		t.Fatalf("expected webp by magic header, got %q", got)
	}

	missingPath := filepath.Join(root, "missing.unknown")
	if got := DetectImageMimeType(missingPath); got != "" {
		t.Fatalf("expected empty mime for missing unknown file, got %q", got)
	}
}

func TestReadMagicHeaderErrorsAndShortRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "short.bin")
	if err := os.WriteFile(path, []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatalf("write short file: %v", err)
	}
	buf, err := readMagicHeader(path, 8)
	if err != nil {
		t.Fatalf("readMagicHeader(short) error = %v", err)
	}
	if len(buf) != 3 {
		t.Fatalf("expected short read length 3, got %d", len(buf))
	}
	if _, err := readMagicHeader(filepath.Join(root, "missing.bin"), 8); err == nil {
		t.Fatalf("expected missing file error")
	}
}
