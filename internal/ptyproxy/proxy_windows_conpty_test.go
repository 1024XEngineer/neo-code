//go:build windows

package ptyproxy

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

func TestBuildWindowsEnvironmentBlockSortedAndTerminated(t *testing.T) {
	block, err := buildWindowsEnvironmentBlock([]string{
		"z=last",
		"A=first",
		"m=middle",
	})
	if err != nil {
		t.Fatalf("buildWindowsEnvironmentBlock() error = %v", err)
	}
	if len(block) < 2 || block[len(block)-1] != 0 || block[len(block)-2] != 0 {
		t.Fatalf("environment block should end with double NUL, got %#v", block)
	}

	decoded := decodeWindowsEnvBlock(block)
	if len(decoded) != 3 {
		t.Fatalf("decoded env len = %d, want 3", len(decoded))
	}
	if decoded[0] != "A=first" || decoded[1] != "m=middle" || decoded[2] != "z=last" {
		t.Fatalf("decoded env order = %#v", decoded)
	}
}

func TestBuildWindowsEnvironmentBlockRejectsNUL(t *testing.T) {
	if _, err := buildWindowsEnvironmentBlock([]string{"A=ok", "B=bad\x00value"}); err == nil {
		t.Fatal("expected error when env contains NUL character")
	}
}

func TestWindowsConPTYShellResizeDelegatesAPI(t *testing.T) {
	originalResize := windowsResizePseudoConsole
	defer func() {
		windowsResizePseudoConsole = originalResize
	}()

	called := false
	windowsResizePseudoConsole = func(console windows.Handle, size windows.Coord) error {
		called = true
		if console != windows.Handle(42) {
			t.Fatalf("console handle = %v, want 42", console)
		}
		if size.X != 100 || size.Y != 40 {
			t.Fatalf("size = (%d,%d), want (100,40)", size.X, size.Y)
		}
		return nil
	}

	shell := &windowsConPTYShell{console: windows.Handle(42)}
	if err := shell.Resize(100, 40); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}
	if !called {
		t.Fatal("expected Resize() to call windowsResizePseudoConsole")
	}
}

func TestWindowsConPTYShellWriteScreenTextDelegatesAPI(t *testing.T) {
	originalWriteConsole := windowsWriteConsole
	defer func() {
		windowsWriteConsole = originalWriteConsole
	}()

	called := false
	windowsWriteConsole = func(console windows.Handle, buf *uint16, towrite uint32, written *uint32, reserved *byte) error {
		called = true
		if console != windows.Handle(88) {
			t.Fatalf("console handle = %v, want 88", console)
		}
		if towrite == 0 {
			t.Fatal("towrite should be > 0")
		}
		if written != nil {
			*written = towrite
		}
		return nil
	}

	shell := &windowsConPTYShell{console: windows.Handle(88)}
	if err := shell.WriteScreenText("\r\n[NeoCode Diagnosis]\r\nroot cause: typo\r\n"); err != nil {
		t.Fatalf("WriteScreenText() error = %v", err)
	}
	if !called {
		t.Fatal("expected WriteScreenText() to call windowsWriteConsole")
	}
}

func TestWindowsConPTYShellCloseReleasesResources(t *testing.T) {
	originalClose := windowsClosePseudoConsole
	defer func() {
		windowsClosePseudoConsole = originalClose
	}()

	var closedConsole windows.Handle
	windowsClosePseudoConsole = func(console windows.Handle) {
		closedConsole = console
	}

	inReader, inWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create input pipe: %v", err)
	}
	outReader, outWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create output pipe: %v", err)
	}
	defer inReader.Close()
	defer outWriter.Close()

	shell := &windowsConPTYShell{
		console:      windows.Handle(77),
		inputWriter:  inWriter,
		outputReader: outReader,
	}
	if err := shell.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if closedConsole != windows.Handle(77) {
		t.Fatalf("closed console = %v, want 77", closedConsole)
	}
	if shell.inputWriter != nil || shell.outputReader != nil {
		t.Fatalf("shell pipes should be released, got input=%v output=%v", shell.inputWriter, shell.outputReader)
	}
}

func TestCreateWindowsInheritablePipeHandles(t *testing.T) {
	reader, writer, err := createWindowsInheritablePipeHandles()
	if err != nil {
		t.Fatalf("createWindowsInheritablePipeHandles() error = %v", err)
	}
	if reader == 0 || writer == 0 {
		t.Fatalf("pipe handles should be non-zero, got reader=%v writer=%v", reader, writer)
	}

	closeWindowsHandle(&reader)
	closeWindowsHandle(&writer)
	if reader != 0 || writer != 0 {
		t.Fatalf("pipe handles should be reset to zero after close, got reader=%v writer=%v", reader, writer)
	}
}

func TestSanitizeWindowsInputConsoleModeDisablesVTInputLineAndEcho(t *testing.T) {
	original := uint32(0x0001 | windowsEnableVirtualTerminalInput | windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT)
	sanitized := sanitizeWindowsInputConsoleMode(original)
	if sanitized&windowsEnableVirtualTerminalInput != 0 {
		t.Fatalf("VT input bit should be cleared, got mode %#x", sanitized)
	}
	if sanitized&windows.ENABLE_LINE_INPUT != 0 {
		t.Fatalf("line input bit should be cleared, got mode %#x", sanitized)
	}
	if sanitized&windows.ENABLE_ECHO_INPUT != 0 {
		t.Fatalf("echo input bit should be cleared, got mode %#x", sanitized)
	}
	if sanitized&0x0001 == 0 {
		t.Fatalf("unrelated input mode bits should be preserved, got mode %#x", sanitized)
	}
}

func TestNormalizeWindowsConPTYInputByteBackspaceCompatibility(t *testing.T) {
	if got := normalizeWindowsConPTYInputByte(0x08); got != 0x7F {
		t.Fatalf("backspace byte normalization = %#x, want %#x", got, byte(0x7F))
	}
	if got := normalizeWindowsConPTYInputByte('a'); got != 'a' {
		t.Fatalf("normal byte normalization = %#x, want %#x", got, byte('a'))
	}
}

func TestSanitizeWindowsOutputConsoleModeEnablesVTWrapAndProcessed(t *testing.T) {
	original := uint32(0)
	sanitized := sanitizeWindowsOutputConsoleMode(original)
	if sanitized&windowsEnableVirtualTerminalProcessing == 0 {
		t.Fatalf("VT output bit should be enabled, got mode %#x", sanitized)
	}
	if sanitized&windows.ENABLE_PROCESSED_OUTPUT == 0 {
		t.Fatalf("processed output bit should be enabled, got mode %#x", sanitized)
	}
	if sanitized&windows.ENABLE_WRAP_AT_EOL_OUTPUT == 0 {
		t.Fatalf("wrap-at-EOL bit should be enabled, got mode %#x", sanitized)
	}
}

func TestResolveWindowsConPTYSizePrefersConsoleInfo(t *testing.T) {
	originalGetMode := windowsGetConsoleMode
	originalGetInfo := windowsGetConsoleInfo
	defer func() {
		windowsGetConsoleMode = originalGetMode
		windowsGetConsoleInfo = originalGetInfo
	}()

	windowsGetConsoleMode = func(handle windows.Handle, mode *uint32) error {
		if mode != nil {
			*mode = 1
		}
		return nil
	}
	windowsGetConsoleInfo = func(handle windows.Handle, info *windows.ConsoleScreenBufferInfo) error {
		if info == nil {
			return errors.New("nil info")
		}
		info.Window.Left = 0
		info.Window.Top = 0
		info.Window.Right = 99
		info.Window.Bottom = 39
		return nil
	}

	size := resolveWindowsConPTYSize()
	if size.X != 100 || size.Y != 40 {
		t.Fatalf("resolveWindowsConPTYSize() = (%d,%d), want (100,40)", size.X, size.Y)
	}
}

func TestResolveWindowsShellCommandPrefersPowerShell(t *testing.T) {
	originalLookPath := windowsLookPath
	defer func() {
		windowsLookPath = originalLookPath
	}()

	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)
	windowsLookPath = func(file string) (string, error) {
		switch file {
		case "pwsh.exe":
			return "", os.ErrNotExist
		case "powershell.exe":
			return `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, nil
		default:
			return "", os.ErrNotExist
		}
	}

	shellPath, args := resolveWindowsShellCommand("")
	if shellPath != `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe` {
		t.Fatalf("shellPath = %q, want powershell path", shellPath)
	}
	if len(args) < 4 || args[0] != "-NoLogo" || args[1] != "-NoExit" || args[2] != "-Command" {
		t.Fatalf("args = %#v, want PowerShell integration bootstrap args", args)
	}
	if !strings.Contains(args[3], "[char]27") || strings.Contains(args[3], "`e]133") {
		t.Fatalf("PowerShell integration should emit ESC via [char]27, got %q", args[3])
	}
	if got := windowsNeoCodeCommandExample(shellPath); got != "./neocode" {
		t.Fatalf("command example = %q, want ./neocode", got)
	}
}

func TestResolveWindowsShellCommandFallsBackToCmd(t *testing.T) {
	originalLookPath := windowsLookPath
	defer func() {
		windowsLookPath = originalLookPath
	}()

	t.Setenv("COMSPEC", `C:\Windows\System32\cmd.exe`)
	windowsLookPath = func(string) (string, error) {
		return "", os.ErrNotExist
	}

	shellPath, args := resolveWindowsShellCommand("")
	if shellPath != `C:\Windows\System32\cmd.exe` {
		t.Fatalf("shellPath = %q, want COMSPEC cmd", shellPath)
	}
	if len(args) != 3 || args[0] != "/Q" || args[1] != "/K" {
		t.Fatalf("args = %#v, want /Q /K <integration>", args)
	}
	if args[2] != "chcp 65001>nul & prompt $P$G" {
		t.Fatalf("cmd integration command = %q, want %q", args[2], "chcp 65001>nul & prompt $P$G")
	}
	if strings.Contains(args[2], "$_") {
		t.Fatalf("cmd integration command should not contain forced newline, got %q", args[2])
	}
	if got := windowsNeoCodeCommandExample(shellPath); got != `.\\neocode` {
		t.Fatalf("command example = %q, want .\\\\neocode", got)
	}
}

func TestNormalizeWindowsShellWaitError(t *testing.T) {
	if err := normalizeWindowsShellWaitError(context.Background(), windowsShellExitError{Code: 9009}); err != nil {
		t.Fatalf("interactive shell exit should be ignored, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	waitErr := windowsShellExitError{Code: 1}
	if err := normalizeWindowsShellWaitError(ctx, waitErr); !errors.As(err, &waitErr) {
		t.Fatalf("terminated shell exit should be preserved, got %v", err)
	}
}

func decodeWindowsEnvBlock(block []uint16) []string {
	result := make([]string, 0, len(block)/8)
	current := make([]uint16, 0, 32)
	for _, value := range block {
		if value != 0 {
			current = append(current, value)
			continue
		}
		if len(current) == 0 {
			break
		}
		result = append(result, string(utf16.Decode(current)))
		current = current[:0]
	}
	return result
}
