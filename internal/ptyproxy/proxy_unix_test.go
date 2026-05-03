//go:build !windows

package ptyproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/tools"

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

func TestHandleDiagSocketConnectionAutoMode(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		wantEnabled bool
	}{
		{name: "auto on", command: diagCommandAutoOn, wantEnabled: true},
		{name: "auto off", command: diagCommandAutoOff, wantEnabled: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			serverConn, clientConn := net.Pipe()
			defer clientConn.Close()

			jobCh := make(chan diagnoseJob, 1)
			autoState := &autoRuntimeState{}
			autoState.Enabled.Store(!tc.wantEnabled)
			done := make(chan struct{})
			go func() {
				handleDiagSocketConnection(context.Background(), serverConn, jobCh, autoState)
				close(done)
			}()

			request := diagIPCRequest{Cmd: tc.command}
			raw, _ := json.Marshal(request)
			_, _ = clientConn.Write(append(raw, '\n'))

			line, err := bufio.NewReader(clientConn).ReadBytes('\n')
			if err != nil {
				t.Fatalf("read response error = %v", err)
			}
			var response diagIPCResponse
			if err := json.Unmarshal(line, &response); err != nil {
				t.Fatalf("unmarshal response error = %v", err)
			}
			if !response.OK {
				t.Fatalf("response not ok: %#v", response)
			}
			if autoState.Enabled.Load() != tc.wantEnabled {
				t.Fatalf("enabled = %v, want %v", autoState.Enabled.Load(), tc.wantEnabled)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("timeout waiting handler return")
			}
		})
	}
}

func TestHandleDiagSocketConnectionAutoStatus(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	jobCh := make(chan diagnoseJob, 1)
	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)

	done := make(chan struct{})
	go func() {
		handleDiagSocketConnection(context.Background(), serverConn, jobCh, autoState)
		close(done)
	}()

	request := diagIPCRequest{Cmd: diagCommandAutoStatus}
	raw, _ := json.Marshal(request)
	_, _ = clientConn.Write(append(raw, '\n'))

	line, err := bufio.NewReader(clientConn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response error = %v", err)
	}
	var response diagIPCResponse
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatalf("unmarshal response error = %v", err)
	}
	if !response.OK || !response.AutoEnabled {
		t.Fatalf("response = %#v, want ok and auto_enabled=true", response)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting handler return")
	}
}

func TestHandleDiagSocketConnectionDiagnose(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	jobCh := make(chan diagnoseJob, 1)
	autoState := &autoRuntimeState{}
	done := make(chan struct{})
	go func() {
		handleDiagSocketConnection(context.Background(), serverConn, jobCh, autoState)
		close(done)
	}()

	raw, _ := json.Marshal(diagIPCRequest{Cmd: diagCommandDiagnose})
	_, _ = clientConn.Write(append(raw, '\n'))

	var job diagnoseJob
	select {
	case job = <-jobCh:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting diagnose job")
	}
	if job.Done == nil {
		t.Fatal("job.Done should not be nil")
	}
	job.Done <- diagIPCResponse{OK: true, Message: "done"}

	line, err := bufio.NewReader(clientConn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response error = %v", err)
	}
	var response diagIPCResponse
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatalf("unmarshal response error = %v", err)
	}
	if !response.OK {
		t.Fatalf("response not ok: %#v", response)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting handler return")
	}
}

func TestSendDiagIPCCommandToPath(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "ipc.sock")
	listener, resolvedPath, err := listenDiagSocket(socketPath)
	if err != nil {
		t.Fatalf("listenDiagSocket() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(resolvedPath)
	}()

	serverDone := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer conn.Close()

		line, readErr := bufio.NewReader(conn).ReadBytes('\n')
		if readErr != nil {
			serverDone <- readErr
			return
		}

		var request diagIPCRequest
		if err := json.Unmarshal(line, &request); err != nil {
			serverDone <- err
			return
		}
		if request.Cmd != diagCommandDiagnose {
			serverDone <- io.ErrUnexpectedEOF
			return
		}
		response, _ := json.Marshal(diagIPCResponse{OK: true, Message: "ok"})
		_, writeErr := conn.Write(append(response, '\n'))
		serverDone <- writeErr
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := sendDiagIPCCommandToPath(ctx, resolvedPath, diagIPCRequest{Cmd: diagCommandDiagnose})
	if err != nil {
		t.Fatalf("sendDiagIPCCommandToPath() error = %v", err)
	}
	if !response.OK {
		t.Fatal("response.OK = false")
	}
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatalf("server error = %v", serverErr)
	}
}

func TestSendDiagIPCCommandToPathRejectsResponse(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "ipc.sock")
	listener, resolvedPath, err := listenDiagSocket(socketPath)
	if err != nil {
		t.Fatalf("listenDiagSocket() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(resolvedPath)
	}()

	serverDone := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer conn.Close()

		line, readErr := bufio.NewReader(conn).ReadBytes('\n')
		if readErr != nil {
			serverDone <- readErr
			return
		}
		var request diagIPCRequest
		if err := json.Unmarshal(line, &request); err != nil {
			serverDone <- err
			return
		}
		response, _ := json.Marshal(diagIPCResponse{OK: false, Message: "denied"})
		_, writeErr := conn.Write(append(response, '\n'))
		serverDone <- writeErr
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = sendDiagIPCCommandToPath(ctx, resolvedPath, diagIPCRequest{Cmd: diagCommandDiagnose})
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected denied error, got %v", err)
	}
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatalf("server error = %v", serverErr)
	}
}

func TestSendDiagIPCCommandEmptyPath(t *testing.T) {
	_, err := sendDiagIPCCommand(context.Background(), "   ", diagIPCRequest{Cmd: diagCommandDiagnose})
	if err == nil {
		t.Fatal("expected empty socket path error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v, want contains empty", err)
	}
}

func TestNormalizeDiagIPCCommand(t *testing.T) {
	if got := normalizeDiagIPCCommand("  AUTO_ON  "); got != "auto_on" {
		t.Fatalf("normalizeDiagIPCCommand() = %q, want %q", got, "auto_on")
	}
}

func TestCommandTrackerObserve(t *testing.T) {
	tests := []struct {
		name   string
		inputs [][]byte
		want   string
	}{
		{
			name:   "single command",
			inputs: [][]byte{[]byte("go test\r")},
			want:   "go test",
		},
		{
			name:   "backspace handling",
			inputs: [][]byte{[]byte("go tes\x08st\r")},
			want:   "go test",
		},
		{
			name:   "multiple commands",
			inputs: [][]byte{[]byte("ls\r"), []byte("cd ..\r")},
			want:   "cd ..",
		},
		{
			name:   "line feed split",
			inputs: [][]byte{[]byte("echo 1\n"), []byte("echo 2\r")},
			want:   "echo 2",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tracker := &commandTracker{}
			for _, input := range tc.inputs {
				tracker.Observe(input)
			}
			if got := tracker.LastCommand(); got != tc.want {
				t.Fatalf("LastCommand() = %q, want %q", got, tc.want)
			}
		})
	}

	var nilTracker *commandTracker
	nilTracker.Observe([]byte("ignored\r"))
	if got := nilTracker.LastCommand(); got != "" {
		t.Fatalf("nil tracker LastCommand() = %q, want empty", got)
	}
}

func TestRenderDiagnosis(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		isError  bool
		contains []string
	}{
		{
			name:     "empty content",
			content:  "",
			contains: []string{"NeoCode Diagnosis", "-"},
		},
		{
			name:     "plain text fallback",
			content:  "raw diagnose output",
			contains: []string{"NeoCode Diagnosis", "raw diagnose output"},
		},
		{
			name: "json diagnosis",
			content: `{"confidence":0.82,"root_cause":"network unreachable","investigation_commands":["ping 1.1.1.1"],` +
				`"fix_commands":["export HTTPS_PROXY=http://127.0.0.1:7890"]}`,
			contains: []string{
				"0.82",
				"network unreachable",
				"ping 1.1.1.1",
				"export HTTPS_PROXY=http://127.0.0.1:7890",
			},
		},
		{
			name:     "error header color",
			content:  "fatal error",
			isError:  true,
			contains: []string{"\u001b[31m[NeoCode Diagnosis]\u001b[0m"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output := &bytes.Buffer{}
			renderDiagnosis(output, tc.content, tc.isError)
			text := output.String()
			for _, fragment := range tc.contains {
				if !strings.Contains(text, fragment) {
					t.Fatalf("output = %q, want contains %q", text, fragment)
				}
			}
			assertNoBareLineFeed(t, text)
		})
	}
}

func TestSerializedWriterConcurrent(t *testing.T) {
	target := &bytes.Buffer{}
	lock := &sync.Mutex{}
	writer := &serializedWriter{writer: target, lock: lock}

	const count = 64
	var wg sync.WaitGroup
	wg.Add(count)
	for index := 0; index < count; index++ {
		go func() {
			defer wg.Done()
			_, _ = writer.Write([]byte("x"))
		}()
	}
	wg.Wait()

	if got := len(target.String()); got != count {
		t.Fatalf("len(output) = %d, want %d", got, count)
	}
}

func TestIsClosedNetworkError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "net err closed", err: net.ErrClosed, want: true},
		{name: "closed message", err: errors.New("use of closed network connection"), want: true},
		{name: "other error", err: errors.New("permission denied"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isClosedNetworkError(tc.err); got != tc.want {
				t.Fatalf("isClosedNetworkError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestWriteProxyTextCRLF(t *testing.T) {
	buffer := &bytes.Buffer{}
	writeProxyText(buffer, "a\nb\rc\r\nd")
	writeProxyLine(buffer, "line")
	writeProxyf(buffer, "fmt:%s\n", "ok")
	text := buffer.String()
	if !strings.Contains(text, "a\r\nb\r\nc\r\nd") {
		t.Fatalf("text = %q, want normalized CRLF content", text)
	}
	if !strings.Contains(text, "line\r\n") {
		t.Fatalf("text = %q, want line with CRLF", text)
	}
	if !strings.Contains(text, "fmt:ok\r\n") {
		t.Fatalf("text = %q, want formatted CRLF line", text)
	}
	assertNoBareLineFeed(t, text)

	writeProxyText(nil, "ignored")
	writeProxyLine(nil, "ignored")
	writeProxyf(nil, "ignored")
}

func TestEnableHostTerminalRawMode(t *testing.T) {
	originalInput := hostTerminalInput
	originalIsTerminal := isTerminalFD
	originalMakeRaw := makeRawTerminal
	originalRestore := restoreTerminal
	t.Cleanup(func() { hostTerminalInput = originalInput })
	t.Cleanup(func() { isTerminalFD = originalIsTerminal })
	t.Cleanup(func() { makeRawTerminal = originalMakeRaw })
	t.Cleanup(func() { restoreTerminal = originalRestore })

	file, err := os.CreateTemp(t.TempDir(), "terminal-*")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer file.Close()

	hostTerminalInput = file
	isTerminalFD = func(int) bool { return true }
	makeRawTerminal = func(int) (*term.State, error) { return &term.State{}, nil }

	restoreCalled := false
	restoreTerminal = func(int, *term.State) error {
		restoreCalled = true
		return nil
	}

	restoreFn, err := enableHostTerminalRawMode()
	if err != nil {
		t.Fatalf("enableHostTerminalRawMode() error = %v", err)
	}
	if restoreFn == nil {
		t.Fatal("restore function should not be nil")
	}
	if err := restoreFn(); err != nil {
		t.Fatalf("restoreFn() error = %v", err)
	}
	if !restoreCalled {
		t.Fatal("expected restoreTerminal called")
	}
}

func TestEnableHostTerminalRawModeFallbacks(t *testing.T) {
	originalInput := hostTerminalInput
	originalIsTerminal := isTerminalFD
	originalMakeRaw := makeRawTerminal
	t.Cleanup(func() { hostTerminalInput = originalInput })
	t.Cleanup(func() { isTerminalFD = originalIsTerminal })
	t.Cleanup(func() { makeRawTerminal = originalMakeRaw })

	hostTerminalInput = nil
	restoreFn, err := enableHostTerminalRawMode()
	if err != nil {
		t.Fatalf("enableHostTerminalRawMode() with nil input error = %v", err)
	}
	if err := restoreFn(); err != nil {
		t.Fatalf("restoreFn() error = %v", err)
	}

	file, err := os.CreateTemp(t.TempDir(), "terminal-*")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer file.Close()
	hostTerminalInput = file
	isTerminalFD = func(int) bool { return false }
	restoreFn, err = enableHostTerminalRawMode()
	if err != nil {
		t.Fatalf("enableHostTerminalRawMode() non-terminal error = %v", err)
	}
	if err := restoreFn(); err != nil {
		t.Fatalf("restoreFn() error = %v", err)
	}

	isTerminalFD = func(int) bool { return true }
	makeRawTerminal = func(int) (*term.State, error) { return nil, errors.New("make raw failed") }
	_, err = enableHostTerminalRawMode()
	if err == nil || !strings.Contains(err.Error(), "set host terminal raw mode") {
		t.Fatalf("err = %v, want wrapped make raw error", err)
	}
}

func TestInstallHostTerminalRestoreGuardNoop(t *testing.T) {
	originalInput := hostTerminalInput
	t.Cleanup(func() { hostTerminalInput = originalInput })
	hostTerminalInput = nil
	restore := installHostTerminalRestoreGuard()
	restore()
}

func TestCopyInputWithTracker(t *testing.T) {
	tracker := &commandTracker{}
	input := []byte("go tes\x08st\rnext\r")
	reader := bytes.NewReader(input)
	output := &bytes.Buffer{}

	written, err := copyInputWithTracker(output, reader, tracker)
	if err != nil {
		t.Fatalf("copyInputWithTracker() error = %v", err)
	}
	if written != int64(len(input)) {
		t.Fatalf("written = %d, want %d", written, len(input))
	}
	if output.String() != string(input) {
		t.Fatalf("output = %q, want %q", output.String(), string(input))
	}
	if tracker.LastCommand() != "next" {
		t.Fatalf("LastCommand() = %q, want %q", tracker.LastCommand(), "next")
	}
}

func TestCopyInputWithTrackerNilIO(t *testing.T) {
	written, err := copyInputWithTracker(nil, nil, &commandTracker{})
	if err != nil {
		t.Fatalf("copyInputWithTracker(nil,nil) error = %v", err)
	}
	if written != 0 {
		t.Fatalf("written = %d, want 0", written)
	}
}

func TestStreamPTYOutputEmitsAutoTrigger(t *testing.T) {
	payloadReader, payloadWriter := io.Pipe()
	defer payloadReader.Close()
	output := &bytes.Buffer{}
	commandLog := NewUTF8RingBuffer(1024)
	tracker := &commandTracker{}
	tracker.Observe([]byte("go test ./...\r"))
	autoTriggers := make(chan diagnoseTrigger, 1)
	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		streamPTYOutput(payloadReader, output, commandLog, tracker, autoTriggers, autoState)
	}()
	go func() {
		// Write OSC133 events in lifecycle order to avoid chunk-order timing flakiness.
		_, _ = payloadWriter.Write([]byte("\x1b]133;C\x07"))
		_, _ = payloadWriter.Write([]byte("fatal: build failed\n"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;D;1\x07"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;A\x07"))
		_ = payloadWriter.Close()
	}()

	select {
	case trigger := <-autoTriggers:
		if trigger.CommandText != "go test ./..." {
			t.Fatalf("trigger.CommandText = %q, want %q", trigger.CommandText, "go test ./...")
		}
		if trigger.ExitCode != 1 {
			t.Fatalf("trigger.ExitCode = %d, want 1", trigger.ExitCode)
		}
		if !strings.Contains(trigger.OutputText, "fatal: build failed") {
			t.Fatalf("trigger.OutputText = %q, want contains fatal message", trigger.OutputText)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected one auto diagnose trigger")
	}

	select {
	case <-streamDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("streamPTYOutput did not finish")
	}

	if !strings.Contains(output.String(), "fatal: build failed") {
		t.Fatalf("output = %q, want contains visible command output", output.String())
	}
}

func TestStreamPTYOutputSkipsTriggerWhenAutoDisabled(t *testing.T) {
	// Without OSC133 A (PromptReady): auto stays disabled, pendingTrigger is never sent.
	payload := strings.NewReader(
		"\x1b]133;C\x07" +
			"fatal: build failed\n" +
			"\x1b]133;D;1\x07",
	)
	output := &bytes.Buffer{}
	commandLog := NewUTF8RingBuffer(1024)
	tracker := &commandTracker{}
	tracker.Observe([]byte("go test ./...\r"))
	autoTriggers := make(chan diagnoseTrigger, 1)
	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(false)

	streamPTYOutput(payload, output, commandLog, tracker, autoTriggers, autoState)

	select {
	case trigger := <-autoTriggers:
		t.Fatalf("unexpected trigger: %#v", trigger)
	default:
	}
	if !strings.Contains(output.String(), "fatal: build failed") {
		t.Fatalf("output = %q, want contains fatal text", output.String())
	}
}

func TestStreamPTYOutputReenablesAutoOnPromptReady(t *testing.T) {
	payload := strings.NewReader(
		"\x1b]133;C\x07" +
			"fatal: build failed\n" +
			"\x1b]133;D;1\x07" +
			"\x1b]133;A\x07",
	)
	output := &bytes.Buffer{}
	commandLog := NewUTF8RingBuffer(1024)
	tracker := &commandTracker{}
	tracker.Observe([]byte("go test ./...\r"))
	autoTriggers := make(chan diagnoseTrigger, 1)
	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(false)
	autoState.OSCReady.Store(false)

	streamPTYOutput(payload, output, commandLog, tracker, autoTriggers, autoState)

	if !autoState.Enabled.Load() {
		t.Fatal("expected auto to be re-enabled after OSC133 PromptReady")
	}
	if !autoState.OSCReady.Load() {
		t.Fatal("expected OSCReady to be set after PromptReady")
	}
	select {
	case trigger := <-autoTriggers:
		if trigger.CommandText != "go test ./..." {
			t.Fatalf("trigger.CommandText = %q, want %q", trigger.CommandText, "go test ./...")
		}
	default:
		t.Fatal("expected one auto diagnose trigger after re-enable")
	}
	if !strings.Contains(output.String(), "fatal: build failed") {
		t.Fatalf("output = %q, want contains fatal text", output.String())
	}
}

func TestDecodeToolResult(t *testing.T) {
	result, err := decodeToolResult(map[string]any{
		"Content": "ok",
		"IsError": false,
	})
	if err != nil {
		t.Fatalf("decodeToolResult() error = %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("result.Content = %q, want %q", result.Content, "ok")
	}
	if result.IsError {
		t.Fatal("result.IsError = true, want false")
	}
}

func TestDecodeToolResultErrors(t *testing.T) {
	_, err := decodeToolResult(func() {})
	if err == nil || !strings.Contains(err.Error(), "encode tool payload") {
		t.Fatalf("expected encode tool payload error, got %v", err)
	}

	_, err = decodeToolResult("plain-text")
	if err == nil || !strings.Contains(err.Error(), "decode tool payload") {
		t.Fatalf("expected decode tool payload error, got %v", err)
	}
}

func TestCallDiagnoseToolSuccess(t *testing.T) {
	baseDir := t.TempDir()
	gatewaySocket := filepath.Join(baseDir, "gateway.sock")
	authTokenFile := filepath.Join(baseDir, "auth.json")
	writeGatewayAuthTokenFile(t, authTokenFile, "test-token")

	cleanupServer, serverDone := startGatewayRPCMockServer(t, gatewaySocket, func(decoder *json.Decoder, encoder *json.Encoder) error {
		authenticateRequest, err := readRPCRequest(decoder)
		if err != nil {
			return err
		}
		if authenticateRequest.Method != protocol.MethodGatewayAuthenticate {
			return fmt.Errorf("authenticate method = %q", authenticateRequest.Method)
		}
		var authenticateParams protocol.AuthenticateParams
		if err := json.Unmarshal(authenticateRequest.Params, &authenticateParams); err != nil {
			return fmt.Errorf("decode authenticate params: %w", err)
		}
		if authenticateParams.Token != "test-token" {
			return fmt.Errorf("authenticate token = %q", authenticateParams.Token)
		}
		if err := writeRPCResult(encoder, authenticateRequest.ID, gateway.MessageFrame{
			Type:   gateway.FrameTypeAck,
			Action: gateway.FrameActionAuthenticate,
		}); err != nil {
			return err
		}

		executeRequest, err := readRPCRequest(decoder)
		if err != nil {
			return err
		}
		if executeRequest.Method != protocol.MethodGatewayExecuteSystemTool {
			return fmt.Errorf("execute method = %q", executeRequest.Method)
		}
		var executeParams protocol.ExecuteSystemToolParams
		if err := json.Unmarshal(executeRequest.Params, &executeParams); err != nil {
			return fmt.Errorf("decode execute params: %w", err)
		}
		if executeParams.ToolName != tools.ToolNameDiagnose {
			return fmt.Errorf("tool name = %q", executeParams.ToolName)
		}
		var diagnoseArgs diagnoseToolArgs
		if err := json.Unmarshal(executeParams.Arguments, &diagnoseArgs); err != nil {
			return fmt.Errorf("decode diagnose args: %w", err)
		}
		if diagnoseArgs.CommandText != "go test ./..." {
			return fmt.Errorf("command text = %q", diagnoseArgs.CommandText)
		}
		if diagnoseArgs.ExitCode != 1 {
			return fmt.Errorf("exit code = %d", diagnoseArgs.ExitCode)
		}

		return writeRPCResult(encoder, executeRequest.ID, gateway.MessageFrame{
			Type:   gateway.FrameTypeAck,
			Action: gateway.FrameActionExecuteSystemTool,
			Payload: map[string]any{
				"Content": "diagnosis ok",
				"IsError": false,
			},
		})
	})
	defer cleanupServer()

	buffer := NewUTF8RingBuffer(2048)
	_, _ = buffer.Write([]byte("fallback log"))
	result, err := callDiagnoseTool(
		nil,
		buffer,
		ManualShellOptions{
			Workdir:              baseDir,
			Shell:                "/bin/bash",
			GatewayListenAddress: gatewaySocket,
			GatewayTokenFile:     authTokenFile,
		},
		filepath.Join(baseDir, "diag.sock"),
		diagnoseTrigger{
			CommandText: "go test ./...",
			ExitCode:    1,
			OutputText:  "fatal: build failed",
		},
	)
	if err != nil {
		t.Fatalf("callDiagnoseTool() error = %v", err)
	}
	if result.Content != "diagnosis ok" {
		t.Fatalf("result.Content = %q, want diagnosis ok", result.Content)
	}
	if result.IsError {
		t.Fatal("result.IsError = true, want false")
	}
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatalf("mock gateway server error = %v", serverErr)
	}
}

func TestCallDiagnoseToolGatewayFrameError(t *testing.T) {
	baseDir := t.TempDir()
	gatewaySocket := filepath.Join(baseDir, "gateway.sock")
	authTokenFile := filepath.Join(baseDir, "auth.json")
	writeGatewayAuthTokenFile(t, authTokenFile, "test-token")

	cleanupServer, serverDone := startGatewayRPCMockServer(t, gatewaySocket, func(decoder *json.Decoder, encoder *json.Encoder) error {
		authenticateRequest, err := readRPCRequest(decoder)
		if err != nil {
			return err
		}
		if err := writeRPCResult(encoder, authenticateRequest.ID, gateway.MessageFrame{
			Type:   gateway.FrameTypeAck,
			Action: gateway.FrameActionAuthenticate,
		}); err != nil {
			return err
		}

		executeRequest, err := readRPCRequest(decoder)
		if err != nil {
			return err
		}
		return writeRPCResult(encoder, executeRequest.ID, gateway.MessageFrame{
			Type: gateway.FrameTypeError,
			Error: &gateway.FrameError{
				Code:    "mock_failed",
				Message: "boom",
			},
		})
	})
	defer cleanupServer()

	buffer := NewUTF8RingBuffer(1024)
	_, _ = buffer.Write([]byte("fallback log"))
	_, err := callDiagnoseTool(
		nil,
		buffer,
		ManualShellOptions{
			Workdir:              baseDir,
			Shell:                "/bin/bash",
			GatewayListenAddress: gatewaySocket,
			GatewayTokenFile:     authTokenFile,
		},
		filepath.Join(baseDir, "diag.sock"),
		diagnoseTrigger{CommandText: "go test ./...", ExitCode: 1, OutputText: "fatal"},
	)
	if err == nil {
		t.Fatal("expected gateway frame error")
	}
	if !strings.Contains(err.Error(), "mock_failed") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("unexpected error = %v", err)
	}
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatalf("mock gateway server error = %v", serverErr)
	}
}

func TestSendDiagIPCCommandFallbackToLegacySocket(t *testing.T) {
	homeDir := t.TempDir()
	legacyTmpDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("TMPDIR", legacyTmpDir)

	legacySocket := filepath.Join(legacyTmpDir, fmt.Sprintf("%s%d%s", diagSocketFilePrefix, time.Now().UnixNano(), diagSocketFileSuffix))
	listener, resolvedLegacySocket, err := listenDiagSocket(legacySocket)
	if err != nil {
		t.Fatalf("listenDiagSocket() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(resolvedLegacySocket)
	}()

	serverDone := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer connection.Close()

		request, readErr := bufio.NewReader(connection).ReadBytes('\n')
		if readErr != nil {
			serverDone <- readErr
			return
		}

		var payload diagIPCRequest
		if err := json.Unmarshal(request, &payload); err != nil {
			serverDone <- err
			return
		}
		if payload.Cmd != diagCommandDiagnose {
			serverDone <- fmt.Errorf("cmd = %q", payload.Cmd)
			return
		}
		response, _ := json.Marshal(diagIPCResponse{OK: true, Message: "ok"})
		_, writeErr := connection.Write(append(response, '\n'))
		serverDone <- writeErr
	}()

	primarySocket := filepath.Join(homeDir, ".neocode", "run", "missing.sock")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := sendDiagIPCCommand(ctx, primarySocket, diagIPCRequest{Cmd: diagCommandDiagnose})
	if err != nil {
		t.Fatalf("sendDiagIPCCommand() error = %v", err)
	}
	if !response.OK {
		t.Fatalf("response = %#v, want ok=true", response)
	}
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatalf("legacy socket server error = %v", serverErr)
	}
}

func TestServeDiagSocketAcceptError(t *testing.T) {
	releaseAccept := make(chan struct{})
	listener := &scriptedListener{
		acceptSteps: []func() (net.Conn, error){
			func() (net.Conn, error) { return nil, errors.New("accept failed") },
			func() (net.Conn, error) {
				<-releaseAccept
				return nil, net.ErrClosed
			},
		},
	}

	output := &bytes.Buffer{}
	jobCh := make(chan diagnoseJob, 1)
	autoState := &autoRuntimeState{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		serveDiagSocket(ctx, listener, jobCh, autoState, output)
	}()

	deadline := time.After(500 * time.Millisecond)
	for {
		if strings.Contains(output.String(), "accept signal error") {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected accept error output, got %q", output.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()
	close(releaseAccept)
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("serveDiagSocket did not exit after context cancellation")
	}
}

func TestRunSingleDiagnosisGatewayUnavailableDoesNotPanic(t *testing.T) {
	buffer := NewUTF8RingBuffer(1024)
	_, _ = buffer.Write([]byte("diagnose log + \u001b[31merror\u001b[0m"))

	output := &bytes.Buffer{}
	runSingleDiagnosis(
		nil,
		output,
		buffer,
		ManualShellOptions{
			Workdir:              t.TempDir(),
			Shell:                "/bin/bash",
			GatewayListenAddress: filepath.Join(t.TempDir(), "missing-gateway.sock"),
			GatewayTokenFile:     filepath.Join(t.TempDir(), "missing-auth.json"),
		},
		filepath.Join(t.TempDir(), "diag.sock"),
		diagnoseTrigger{
			CommandText: "go test ./...",
			ExitCode:    1,
			OutputText:  "fatal: missing module",
		},
		false,
		&autoRuntimeState{},
	)

	if !strings.Contains(output.String(), "NeoCode Diagnosis") {
		t.Fatalf("output = %q, want contains %q", output.String(), "NeoCode Diagnosis")
	}
	assertNoBareLineFeed(t, output.String())
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

func TestRunManualShellBasicIntegration(t *testing.T) {
	if os.Getenv("PTY_INTEGRATION_TEST") != "1" {
		t.Skip("set PTY_INTEGRATION_TEST=1 to run integration test")
	}
	if _, err := os.Stat("/bin/echo"); err != nil {
		t.Skip("/bin/echo not available")
	}

	originalInput := hostTerminalInput
	t.Cleanup(func() { hostTerminalInput = originalInput })
	hostTerminalInput = nil

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := RunManualShell(ctx, ManualShellOptions{
		Workdir: t.TempDir(),
		Shell:   "/bin/echo",
		Stdin:   bytes.NewReader(nil),
		Stdout:  stdout,
		Stderr:  stderr,
	})
	if err != nil {
		t.Fatalf("RunManualShell() error = %v, stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), proxyInitializedBanner) {
		t.Fatalf("stdout = %q, want contains initialized banner", stdout.String())
	}
	if !strings.Contains(stdout.String(), proxyExitedBanner) {
		t.Fatalf("stdout = %q, want contains exited banner", stdout.String())
	}
}

func writeGatewayAuthTokenFile(t *testing.T, path string, token string) {
	t.Helper()
	payload := map[string]any{
		"version":    1,
		"token":      token,
		"created_at": time.Now().UTC(),
		"updated_at": time.Now().UTC(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal auth token payload error = %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write auth token file error = %v", err)
	}
}

func startGatewayRPCMockServer(
	t *testing.T,
	socketPath string,
	handler func(decoder *json.Decoder, encoder *json.Encoder) error,
) (func(), <-chan error) {
	t.Helper()
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen gateway socket error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer connection.Close()

		decoder := json.NewDecoder(connection)
		encoder := json.NewEncoder(connection)
		done <- handler(decoder, encoder)
	}()

	cleanup := func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}
	return cleanup, done
}

func readRPCRequest(decoder *json.Decoder) (protocol.JSONRPCRequest, error) {
	var request protocol.JSONRPCRequest
	if err := decoder.Decode(&request); err != nil {
		return protocol.JSONRPCRequest{}, err
	}
	return request, nil
}

func writeRPCResult(encoder *json.Encoder, id json.RawMessage, result any) error {
	response, rpcErr := protocol.NewJSONRPCResultResponse(id, result)
	if rpcErr != nil {
		return fmt.Errorf("new jsonrpc result response: %v", rpcErr)
	}
	if err := encoder.Encode(response); err != nil {
		return fmt.Errorf("encode jsonrpc result: %w", err)
	}
	return nil
}

type scriptedListener struct {
	mu          sync.Mutex
	acceptSteps []func() (net.Conn, error)
	index       int
}

func (s *scriptedListener) Accept() (net.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index >= len(s.acceptSteps) {
		return nil, net.ErrClosed
	}
	step := s.acceptSteps[s.index]
	s.index++
	return step()
}

func (s *scriptedListener) Close() error {
	return nil
}

func (s *scriptedListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "scripted-listener", Net: "unix"}
}
