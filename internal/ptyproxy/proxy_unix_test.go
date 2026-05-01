//go:build !windows

package ptyproxy

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/term"
)

func assertNoBareLineFeed(t *testing.T, text string) {
	t.Helper()
	for index := 0; index < len(text); index++ {
		if text[index] == '\n' && (index == 0 || text[index-1] != '\r') {
			t.Fatalf("output contains bare LF at index %d: %q", index, text)
		}
	}
}

func TestBuildShellCommandInjectsDiagEnv(t *testing.T) {
	t.Setenv(DiagSocketEnv, "/tmp/old.sock")
	command := buildShellCommand("/bin/bash", ManualShellOptions{
		Workdir: "/tmp",
	}, "/tmp/new.sock")

	if command.Path != "/bin/bash" {
		t.Fatalf("command.Path = %q, want %q", command.Path, "/bin/bash")
	}
	if command.Dir != "/tmp" {
		t.Fatalf("command.Dir = %q, want %q", command.Dir, "/tmp")
	}

	var entries []string
	for _, item := range command.Env {
		if strings.HasPrefix(item, DiagSocketEnv+"=") {
			entries = append(entries, item)
		}
	}
	if len(entries) != 1 {
		t.Fatalf("diag env entries len = %d, want 1", len(entries))
	}
	if entries[0] != DiagSocketEnv+"=/tmp/new.sock" {
		t.Fatalf("diag env entry = %q, want %q", entries[0], DiagSocketEnv+"=/tmp/new.sock")
	}
}

func TestSendDiagnoseSignal(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "diag.sock")
	listener, resolvedPath, err := listenDiagSocket(socketPath)
	if err != nil {
		t.Fatalf("listenDiagSocket() error = %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(resolvedPath)
	})

	payloadCh := make(chan string, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			payloadCh <- "accept-error:" + acceptErr.Error()
			return
		}
		defer connection.Close()
		buffer := make([]byte, 128)
		n, readErr := connection.Read(buffer)
		if readErr != nil {
			payloadCh <- "read-error:" + readErr.Error()
			return
		}
		payloadCh <- string(buffer[:n])
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := SendDiagnoseSignal(ctx, resolvedPath); err != nil {
		t.Fatalf("SendDiagnoseSignal() error = %v", err)
	}

	select {
	case payload := <-payloadCh:
		if payload != diagSignalPayload {
			t.Fatalf("payload = %q, want %q", payload, diagSignalPayload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for payload")
	}
}

func TestSendDiagnoseSignalWaitsForServerCompletion(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "diag-wait.sock")
	listener, resolvedPath, err := listenDiagSocket(socketPath)
	if err != nil {
		t.Fatalf("listenDiagSocket() error = %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(resolvedPath)
	})

	const serverDelay = 120 * time.Millisecond
	serverErrCh := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverErrCh <- acceptErr
			return
		}
		defer connection.Close()
		buffer := make([]byte, 128)
		n, readErr := connection.Read(buffer)
		if readErr != nil {
			serverErrCh <- readErr
			return
		}
		if string(buffer[:n]) != diagSignalPayload {
			serverErrCh <- errors.New("unexpected payload")
			return
		}
		time.Sleep(serverDelay)
		_, writeErr := connection.Write([]byte(diagSignalAckPayload))
		serverErrCh <- writeErr
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := SendDiagnoseSignal(ctx, resolvedPath); err != nil {
		t.Fatalf("SendDiagnoseSignal() error = %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < serverDelay {
		t.Fatalf("SendDiagnoseSignal returned too early: elapsed=%s, want >= %s", elapsed, serverDelay)
	}

	select {
	case serverErr := <-serverErrCh:
		if serverErr != nil {
			t.Fatalf("server error = %v", serverErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server completion")
	}
}

func TestListenDiagSocketRecoversStaleSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "stale.sock")
	staleListener, resolvedPath, err := listenDiagSocket(socketPath)
	if err != nil {
		t.Fatalf("prepare stale listener error = %v", err)
	}
	_ = staleListener.Close()

	listener, _, err := listenDiagSocket(resolvedPath)
	if err != nil {
		t.Fatalf("listenDiagSocket() with stale socket error = %v", err)
	}
	_ = listener.Close()
	_ = os.Remove(resolvedPath)
}

func TestCleanupStaleSocketRejectsRegularFile(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "not-socket.sock")
	if err := os.WriteFile(socketPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file error = %v", err)
	}
	err := cleanupStaleSocket(socketPath)
	if err == nil {
		t.Fatal("expected non-socket error")
	}
	if !strings.Contains(err.Error(), "not socket") {
		t.Fatalf("error = %v, want contains %q", err, "not socket")
	}
}

func TestRunSingleDiagnosisGatewayUnavailableDoesNotPanic(t *testing.T) {
	buffer := NewUTF8RingBuffer(1024)
	_, _ = buffer.Write([]byte("中文日志 + \u001b[31merror\u001b[0m"))

	output := &bytes.Buffer{}
	runSingleDiagnosis(output, buffer, ManualShellOptions{
		Workdir:              t.TempDir(),
		Shell:                "/bin/bash",
		GatewayListenAddress: filepath.Join(t.TempDir(), "missing-gateway.sock"),
		GatewayTokenFile:     filepath.Join(t.TempDir(), "missing-auth.json"),
	}, filepath.Join(t.TempDir(), "diag.sock"))

	if !strings.Contains(output.String(), "NeoCode Diagnosis") {
		t.Fatalf("output = %q, want contains %q", output.String(), "NeoCode Diagnosis")
	}
	assertNoBareLineFeed(t, output.String())
}

func TestPrintProxyInitializedBanner(t *testing.T) {
	buffer := &bytes.Buffer{}
	printProxyInitializedBanner(buffer)
	if buffer.String() != proxyInitializedBanner+"\r\n" {
		t.Fatalf("banner output = %q, want %q", buffer.String(), proxyInitializedBanner+"\\r\\n")
	}
}

func TestPrintProxyExitedBanner(t *testing.T) {
	buffer := &bytes.Buffer{}
	printProxyExitedBanner(buffer)
	if buffer.String() != "\r\n"+proxyExitedBanner+"\r\n" {
		t.Fatalf("banner output = %q, want %q", buffer.String(), "\\r\\n"+proxyExitedBanner+"\\r\\n")
	}
}

func TestRenderDiagnosisUsesCRLFLineEndings(t *testing.T) {
	buffer := &bytes.Buffer{}
	renderDiagnosis(buffer, "", false)
	output := buffer.String()
	assertNoBareLineFeed(t, output)
	if !strings.Contains(output, "\r\n") {
		t.Fatalf("renderDiagnosis output = %q, want contains CRLF", output)
	}
}

func TestEnableHostTerminalRawModeSkipsNonTerminal(t *testing.T) {
	originalInput := hostTerminalInput
	originalIsTerminal := isTerminalFD
	originalMakeRaw := makeRawTerminal
	originalRestore := restoreTerminal
	t.Cleanup(func() {
		hostTerminalInput = originalInput
		isTerminalFD = originalIsTerminal
		makeRawTerminal = originalMakeRaw
		restoreTerminal = originalRestore
	})

	filePath := filepath.Join(t.TempDir(), "stdin.txt")
	if err := os.WriteFile(filePath, []byte("stdin"), 0o600); err != nil {
		t.Fatalf("write stdin file error = %v", err)
	}
	inputFile, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("open stdin file error = %v", err)
	}
	defer inputFile.Close()

	hostTerminalInput = inputFile
	isTerminalFD = func(int) bool { return false }

	makeRawCalled := false
	makeRawTerminal = func(int) (*term.State, error) {
		makeRawCalled = true
		return &term.State{}, nil
	}
	restoreFn, err := enableHostTerminalRawMode()
	if err != nil {
		t.Fatalf("enableHostTerminalRawMode() error = %v", err)
	}
	if makeRawCalled {
		t.Fatal("makeRawTerminal should not be called for non-terminal input")
	}
	if restoreErr := restoreFn(); restoreErr != nil {
		t.Fatalf("restoreFn() error = %v", restoreErr)
	}
}

func TestEnableHostTerminalRawModeCallsMakeRawAndRestore(t *testing.T) {
	originalInput := hostTerminalInput
	originalIsTerminal := isTerminalFD
	originalMakeRaw := makeRawTerminal
	originalRestore := restoreTerminal
	t.Cleanup(func() {
		hostTerminalInput = originalInput
		isTerminalFD = originalIsTerminal
		makeRawTerminal = originalMakeRaw
		restoreTerminal = originalRestore
	})

	filePath := filepath.Join(t.TempDir(), "stdin-terminal.txt")
	if err := os.WriteFile(filePath, []byte("stdin"), 0o600); err != nil {
		t.Fatalf("write stdin file error = %v", err)
	}
	inputFile, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("open stdin file error = %v", err)
	}
	defer inputFile.Close()
	hostTerminalInput = inputFile

	expectedFD := int(inputFile.Fd())
	state := &term.State{}
	makeRawCalled := false
	restoreCalled := false

	isTerminalFD = func(fd int) bool {
		if fd != expectedFD {
			t.Fatalf("isTerminal fd = %d, want %d", fd, expectedFD)
		}
		return true
	}
	makeRawTerminal = func(fd int) (*term.State, error) {
		if fd != expectedFD {
			t.Fatalf("makeRaw fd = %d, want %d", fd, expectedFD)
		}
		makeRawCalled = true
		return state, nil
	}
	restoreTerminal = func(fd int, restored *term.State) error {
		if fd != expectedFD {
			t.Fatalf("restore fd = %d, want %d", fd, expectedFD)
		}
		if restored != state {
			t.Fatalf("restore state pointer mismatch")
		}
		restoreCalled = true
		return nil
	}

	restoreFn, err := enableHostTerminalRawMode()
	if err != nil {
		t.Fatalf("enableHostTerminalRawMode() error = %v", err)
	}
	if !makeRawCalled {
		t.Fatal("expected makeRawTerminal to be called")
	}
	if restoreErr := restoreFn(); restoreErr != nil {
		t.Fatalf("restoreFn() error = %v", restoreErr)
	}
	if !restoreCalled {
		t.Fatal("expected restoreTerminal to be called")
	}
}

func TestEnableHostTerminalRawModeMakeRawError(t *testing.T) {
	originalInput := hostTerminalInput
	originalIsTerminal := isTerminalFD
	originalMakeRaw := makeRawTerminal
	originalRestore := restoreTerminal
	t.Cleanup(func() {
		hostTerminalInput = originalInput
		isTerminalFD = originalIsTerminal
		makeRawTerminal = originalMakeRaw
		restoreTerminal = originalRestore
	})

	filePath := filepath.Join(t.TempDir(), "stdin-error.txt")
	if err := os.WriteFile(filePath, []byte("stdin"), 0o600); err != nil {
		t.Fatalf("write stdin file error = %v", err)
	}
	inputFile, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("open stdin file error = %v", err)
	}
	defer inputFile.Close()
	hostTerminalInput = inputFile

	isTerminalFD = func(int) bool { return true }
	makeRawTerminal = func(int) (*term.State, error) {
		return nil, errors.New("raw mode failed")
	}

	_, err = enableHostTerminalRawMode()
	if err == nil {
		t.Fatal("expected raw mode error")
	}
	if !strings.Contains(err.Error(), "set host terminal raw mode") {
		t.Fatalf("error = %v, want contains %q", err, "set host terminal raw mode")
	}
}

func TestResolveShellPathDefaultsToBinBash(t *testing.T) {
	t.Setenv("SHELL", "")
	path := resolveShellPath("")
	if path != "/bin/bash" {
		t.Fatalf("resolveShellPath() = %q, want %q", path, "/bin/bash")
	}
}

func TestResolveShellPathUsesShellEnv(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	path := resolveShellPath("")
	if path != "/bin/zsh" {
		t.Fatalf("resolveShellPath() = %q, want %q", path, "/bin/zsh")
	}
}

func TestResolveShellPathPrefersExplicit(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	path := resolveShellPath("/bin/fish")
	if path != "/bin/fish" {
		t.Fatalf("resolveShellPath() = %q, want %q", path, "/bin/fish")
	}
}

func TestIsClosedNetworkErrorNil(t *testing.T) {
	if isClosedNetworkError(nil) {
		t.Fatal("isClosedNetworkError(nil) should be false")
	}
}

func TestIsClosedNetworkErrorDetectsNetErrClosed(t *testing.T) {
	if !isClosedNetworkError(net.ErrClosed) {
		t.Fatal("isClosedNetworkError(net.ErrClosed) should be true")
	}
}

func TestIsClosedNetworkErrorDetectsStringMatch(t *testing.T) {
	err := errors.New("use of closed network connection")
	if !isClosedNetworkError(err) {
		t.Fatal("isClosedNetworkError() should be true for closed connection string")
	}
}

func TestIsClosedNetworkErrorNonMatch(t *testing.T) {
	err := errors.New("some other error")
	if isClosedNetworkError(err) {
		t.Fatal("isClosedNetworkError() should be false for unrelated error")
	}
}

func TestSerializedWriterNilWriter(t *testing.T) {
	var w *serializedWriter
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write() on nil receiver error = %v", err)
	}
	if n != len("hello") {
		t.Fatalf("Write() n = %d, want %d", n, len("hello"))
	}
}

func TestSerializedWriterNilLock(t *testing.T) {
	var buffer bytes.Buffer
	w := &serializedWriter{writer: &buffer, lock: nil}
	n, err := w.Write([]byte("data"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 4 {
		t.Fatalf("Write() n = %d, want 4", n)
	}
	if buffer.String() != "data" {
		t.Fatalf("buffer = %q, want %q", buffer.String(), "data")
	}
}

func TestSerializedWriterWithLock(t *testing.T) {
	var buffer bytes.Buffer
	var mu sync.Mutex
	w := &serializedWriter{writer: &buffer, lock: &mu}
	n, err := w.Write([]byte("locked-write"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len("locked-write") {
		t.Fatalf("Write() n = %d, want %d", n, len("locked-write"))
	}
	if buffer.String() != "locked-write" {
		t.Fatalf("buffer = %q, want %q", buffer.String(), "locked-write")
	}
}

func TestWriteProxyTextNilWriter(t *testing.T) {
	writeProxyText(nil, "text")
	writeProxyLine(nil, "text")
}

func TestWriteProxyTextEmptyText(t *testing.T) {
	var buffer bytes.Buffer
	writeProxyText(&buffer, "")
	if buffer.Len() != 0 {
		t.Fatalf("expected empty output, got %q", buffer.String())
	}
}

func TestWriteProxyTextNormalizesLineEndings(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello\nworld", "hello\r\nworld"},
		{"hello\r\nworld", "hello\r\nworld"},
		{"hello\rworld", "hello\r\nworld"},
		{"line1\nline2\nline3", "line1\r\nline2\r\nline3"},
	}
	for _, tt := range tests {
		var buffer bytes.Buffer
		writeProxyText(&buffer, tt.input)
		if buffer.String() != tt.expected {
			t.Fatalf("writeProxyText(%q) = %q, want %q", tt.input, buffer.String(), tt.expected)
		}
	}
}

func TestWriteProxyLine(t *testing.T) {
	var buffer bytes.Buffer
	writeProxyLine(&buffer, "header")
	// writeProxyText with "\n" appended → normalized to "\r\n"
	if buffer.String() != "header\r\n" {
		t.Fatalf("writeProxyLine output = %q, want %q", buffer.String(), "header\\r\\n")
	}
}

func TestWriteProxyf(t *testing.T) {
	var buffer bytes.Buffer
	writeProxyf(&buffer, "count=%d, name=%s", 42, "test")
	expected := "count=42, name=test"
	// writeProxyf → writeProxyText which normalizes
	got := buffer.String()
	if got != expected {
		t.Fatalf("writeProxyf output = %q, want %q", got, expected)
	}
}

func TestSyncPTYWindowSizeNilPTY(t *testing.T) {
	syncPTYWindowSize(nil, nil)
}

func TestWatchPTYWindowResizeNilPTY(t *testing.T) {
	stop := watchPTYWindowResize(nil, nil)
	stop()
}

func TestDecodeToolResult(t *testing.T) {
	result, err := decodeToolResult([]byte(`{"content":"ok","isError":false}`))
	if err != nil {
		t.Fatalf("decodeToolResult() error = %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("Content = %q, want %q", result.Content, "ok")
	}
	if result.IsError {
		t.Fatal("IsError should be false")
	}
}

func TestDecodeToolResultInvalidPayload(t *testing.T) {
	_, err := decodeToolResult([]byte(`not-json`))
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDecodeToolResultFromStruct(t *testing.T) {
	result, err := decodeToolResult(map[string]any{"content": "from-map", "isError": true})
	if err != nil {
		t.Fatalf("decodeToolResult() error = %v", err)
	}
	if result.Content != "from-map" {
		t.Fatalf("Content = %q, want %q", result.Content, "from-map")
	}
	if !result.IsError {
		t.Fatal("IsError should be true")
	}
}

func TestRenderDiagnosisNilOutput(t *testing.T) {
	renderDiagnosis(nil, "some content", false)
}

func TestRenderDiagnosisEmptyContent(t *testing.T) {
	var buffer bytes.Buffer
	renderDiagnosis(&buffer, "", false)
	output := buffer.String()
	if !strings.Contains(output, "无可用诊断内容") {
		t.Fatalf("output = %q, want contains %q", output, "无可用诊断内容")
	}
	assertNoBareLineFeed(t, output)
}

func TestRenderDiagnosisErrorHeader(t *testing.T) {
	var buffer bytes.Buffer
	renderDiagnosis(&buffer, `{"confidence":0.9,"rootCause":"OOM","fixCommands":["free -h"]}`, true)
	output := buffer.String()
	assertNoBareLineFeed(t, output)
	if !strings.Contains(output, "\033[31m") {
		t.Fatalf("error output should use red color, got %q", output)
	}
}

func TestRenderDiagnosisFullContent(t *testing.T) {
	var buffer bytes.Buffer
	renderDiagnosis(&buffer, `{"confidence":0.95,"rootCause":"disk full","investigationCommands":["df -h"],"fixCommands":["rm -rf /tmp/*"]}`, false)
	output := buffer.String()
	assertNoBareLineFeed(t, output)
	if !strings.Contains(output, "置信度: 0.95") {
		t.Fatalf("output = %q, want contains %q", output, "置信度: 0.95")
	}
	if !strings.Contains(output, "建议排查命令") {
		t.Fatalf("output = %q, want contains %q", output, "建议排查命令")
	}
	if !strings.Contains(output, "建议修复命令") {
		t.Fatalf("output = %q, want contains %q", output, "建议修复命令")
	}
}

func TestCleanupStaleSocketNotExist(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "nonexistent.sock")
	err := cleanupStaleSocket(socketPath)
	if err != nil {
		t.Fatalf("cleanupStaleSocket() error = %v, want nil", err)
	}
}

func TestHandleDiagSocketConnectionNil(t *testing.T) {
	handleDiagSocketConnection(context.Background(), nil, make(chan diagSignalRequest, 1))
}

func TestConsumeDiagSignalsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	consumeDiagSignals(ctx, nil, nil, nil, ManualShellOptions{}, "")
}

func TestConsumeDiagSignalsChannelClosed(t *testing.T) {
	ch := make(chan diagSignalRequest)
	close(ch)
	consumeDiagSignals(context.Background(), ch, nil, nil, ManualShellOptions{}, "")
}

func TestPrintProxyFunctionsNilWriter(t *testing.T) {
	printProxyInitializedBanner(nil)
	printProxyExitedBanner(nil)
}

func TestRunSingleDiagnosisNilOutput(t *testing.T) {
	runSingleDiagnosis(nil, nil, ManualShellOptions{}, "")
}
