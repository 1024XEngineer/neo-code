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
	original := clipboardWriteAll
	t.Cleanup(func() { clipboardWriteAll = original })

	captured := ""
	clipboardWriteAll = func(text string) error {
		captured = text
		return nil
	}
	if err := CopyText("hello"); err != nil {
		t.Fatalf("CopyText() error = %v", err)
	}
	if captured != "hello" {
		t.Fatalf("expected captured clipboard text, got %q", captured)
	}
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
		ToolTimeoutSec: 1,
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

	output, err = DefaultWorkspaceCommandExecutor(context.Background(), cfg, workdir, sleepCmd)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got err=%v output=%q", err, output)
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
