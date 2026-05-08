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
	gatewayclient "neo-code/internal/gateway/client"
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

func TestListenIDMSocketDerivesFromDiagSocketOverride(t *testing.T) {
	diagSocketPath := filepath.Join(t.TempDir(), "custom-diag.sock")
	listener, idmPath, err := listenIDMSocket(diagSocketPath)
	if err != nil {
		t.Fatalf("listenIDMSocket() error = %v", err)
	}
	defer listener.Close()
	defer os.Remove(idmPath)

	expected := filepath.Join(filepath.Dir(diagSocketPath), "custom-diag-idm.sock")
	if filepath.Clean(idmPath) != filepath.Clean(expected) {
		t.Fatalf("idm path = %q, want %q", idmPath, expected)
	}
}

func TestListenIDMSocketFallsBackToDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	listener, idmPath, err := listenIDMSocket("")
	if err != nil {
		t.Fatalf("listenIDMSocket() error = %v", err)
	}
	defer listener.Close()
	defer os.Remove(idmPath)

	if !strings.HasSuffix(filepath.ToSlash(idmPath), "-idm.sock") {
		t.Fatalf("idm path = %q, want suffix -idm.sock", idmPath)
	}
}

func TestListenIDMSocketRejectsInvalidDiagPath(t *testing.T) {
	_, _, err := listenIDMSocket(" . ")
	if err == nil || !strings.Contains(err.Error(), "empty diagnose socket") {
		t.Fatalf("err = %v, want empty diagnose socket", err)
	}
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
			autoState.OSCReady.Store(true)
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

func TestSendDiagIPCCommandWithoutRunDirFallback(t *testing.T) {
	_, err := sendDiagIPCCommand(context.Background(), "/tmp/not-in-run-dir.sock", diagIPCRequest{Cmd: diagCommandDiagnose})
	if err == nil {
		t.Fatal("expected sendDiagIPCCommand() to fail")
	}
	if strings.Contains(err.Error(), "legacy tmp path") {
		t.Fatalf("unexpected legacy fallback for non-run path: %v", err)
	}
}

func TestSendDiagIPCCommandRunDirFallbackMissingLegacy(t *testing.T) {
	home := t.TempDir()
	legacyTmpDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", legacyTmpDir)

	primarySocket := filepath.Join(home, ".neocode", "run", "missing.sock")
	_, err := sendDiagIPCCommand(context.Background(), primarySocket, diagIPCRequest{Cmd: diagCommandDiagnose})
	if err == nil {
		t.Fatal("expected sendDiagIPCCommand() to fail")
	}
}

func TestSendDiagIPCCommandToPathResponseReadAndDecodeErrors(t *testing.T) {
	t.Run("read response failed", func(t *testing.T) {
		socketPath := filepath.Join(t.TempDir(), "read-failed.sock")
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
			// Drain the client request so the write phase succeeds before we close.
			_, _ = bufio.NewReader(conn).ReadBytes('\n')
			_ = conn.Close()
			serverDone <- nil
		}()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err = sendDiagIPCCommandToPath(ctx, resolvedPath, diagIPCRequest{Cmd: diagCommandDiagnose})
		if err == nil || !strings.Contains(err.Error(), "wait diagnose completion") {
			t.Fatalf("err = %v, want wait diagnose completion", err)
		}
		if serverErr := <-serverDone; serverErr != nil {
			t.Fatalf("server error = %v", serverErr)
		}
	})

	t.Run("decode response failed", func(t *testing.T) {
		socketPath := filepath.Join(t.TempDir(), "decode-failed.sock")
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
			_, _ = bufio.NewReader(conn).ReadBytes('\n')
			_, writeErr := conn.Write([]byte("not-json\n"))
			serverDone <- writeErr
		}()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err = sendDiagIPCCommandToPath(ctx, resolvedPath, diagIPCRequest{Cmd: diagCommandDiagnose})
		if err == nil || !strings.Contains(err.Error(), "decode diag response") {
			t.Fatalf("err = %v, want decode diag response", err)
		}
		if serverErr := <-serverDone; serverErr != nil {
			t.Fatalf("server error = %v", serverErr)
		}
	})

	t.Run("empty rejection message", func(t *testing.T) {
		socketPath := filepath.Join(t.TempDir(), "empty-reject.sock")
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
			_, _ = bufio.NewReader(conn).ReadBytes('\n')
			resp, _ := json.Marshal(diagIPCResponse{OK: false, Message: ""})
			_, writeErr := conn.Write(append(resp, '\n'))
			serverDone <- writeErr
		}()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err = sendDiagIPCCommandToPath(ctx, resolvedPath, diagIPCRequest{Cmd: diagCommandDiagnose})
		if err == nil || !strings.Contains(err.Error(), "diagnose command rejected") {
			t.Fatalf("err = %v, want default rejection message", err)
		}
		if serverErr := <-serverDone; serverErr != nil {
			t.Fatalf("server error = %v", serverErr)
		}
	})
}

func TestSendDiagIPCCommandToPathContextDone(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "context-done.sock")
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
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		time.Sleep(120 * time.Millisecond)
		resp, _ := json.Marshal(diagIPCResponse{OK: true, Message: "ok"})
		_, writeErr := conn.Write(append(resp, '\n'))
		if writeErr != nil && !strings.Contains(strings.ToLower(writeErr.Error()), "broken pipe") {
			serverDone <- writeErr
			return
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = sendDiagIPCCommandToPath(ctx, resolvedPath, diagIPCRequest{Cmd: diagCommandDiagnose})
	if err == nil || !strings.Contains(err.Error(), "wait diagnose completion") {
		t.Fatalf("err = %v, want context wait diagnose completion", err)
	}
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatalf("server error = %v", serverErr)
	}
}

func TestSignalHelpersViaIPC(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "ipc.sock")
	listener, resolvedPath, err := listenDiagSocket(socketPath)
	if err != nil {
		t.Fatalf("listenDiagSocket() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(resolvedPath)
	}()

	receivedCommands := make(chan string, 4)
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)
		for i := 0; i < 4; i++ {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				serverDone <- acceptErr
				return
			}
			reader := bufio.NewReader(conn)
			line, readErr := reader.ReadBytes('\n')
			if readErr != nil {
				_ = conn.Close()
				serverDone <- readErr
				return
			}
			var request diagIPCRequest
			if err := json.Unmarshal(line, &request); err != nil {
				_ = conn.Close()
				serverDone <- err
				return
			}
			receivedCommands <- request.Cmd

			response := diagIPCResponse{OK: true, Message: "ok"}
			if request.Cmd == diagCommandAutoStatus {
				response.AutoEnabled = true
			}
			encoded, _ := json.Marshal(response)
			if _, writeErr := conn.Write(append(encoded, '\n')); writeErr != nil {
				_ = conn.Close()
				serverDone <- writeErr
				return
			}
			_ = conn.Close()
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := SendDiagnoseSignal(ctx, resolvedPath); err != nil {
		t.Fatalf("SendDiagnoseSignal() error = %v", err)
	}
	if err := SendAutoModeSignal(ctx, resolvedPath, true); err != nil {
		t.Fatalf("SendAutoModeSignal(on) error = %v", err)
	}
	if err := SendAutoModeSignal(ctx, resolvedPath, false); err != nil {
		t.Fatalf("SendAutoModeSignal(off) error = %v", err)
	}
	enabled, err := QueryAutoMode(ctx, resolvedPath)
	if err != nil {
		t.Fatalf("QueryAutoMode() error = %v", err)
	}
	if !enabled {
		t.Fatal("QueryAutoMode() = false, want true")
	}

	var commands []string
	for i := 0; i < 4; i++ {
		commands = append(commands, <-receivedCommands)
	}
	want := []string{
		diagCommandDiagnose,
		diagCommandAutoOn,
		diagCommandAutoOff,
		diagCommandAutoStatus,
	}
	for i := range want {
		if commands[i] != want[i] {
			t.Fatalf("command[%d] = %q, want %q", i, commands[i], want[i])
		}
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
		{
			name: "ignore terminal escape keys",
			inputs: [][]byte{
				[]byte("cat a/b/csssssss"),
				[]byte("\x1b[A"),
				[]byte("\x1b[B"),
				[]byte("\r"),
			},
			want: "cat a/b/csssssss",
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

func TestInstallHostTerminalRestoreGuardNonTerminalNoop(t *testing.T) {
	originalInput := hostTerminalInput
	originalIsTerminal := isTerminalFD
	t.Cleanup(func() { hostTerminalInput = originalInput })
	t.Cleanup(func() { isTerminalFD = originalIsTerminal })

	file, err := os.CreateTemp(t.TempDir(), "terminal-*")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer file.Close()

	hostTerminalInput = file
	isTerminalFD = func(int) bool { return false }
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

func TestStreamPTYOutputEmitsAutoTriggerWithoutCommandStartEvent(t *testing.T) {
	payloadReader, payloadWriter := io.Pipe()
	defer payloadReader.Close()
	output := &bytes.Buffer{}
	commandLog := NewUTF8RingBuffer(1024)
	tracker := &commandTracker{}
	tracker.Observe([]byte("cat missing-file\r"))
	autoTriggers := make(chan diagnoseTrigger, 1)
	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		streamPTYOutput(payloadReader, output, commandLog, tracker, autoTriggers, autoState)
	}()
	go func() {
		// 模拟仅有 D/A 事件、缺失 C 事件的 shell 集成场景。
		_, _ = payloadWriter.Write([]byte("cat: missing-file: No such file or directory\n"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;D;1\x07"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;A\x07"))
		_ = payloadWriter.Close()
	}()

	select {
	case trigger := <-autoTriggers:
		if trigger.CommandText != "cat missing-file" {
			t.Fatalf("trigger.CommandText = %q, want %q", trigger.CommandText, "cat missing-file")
		}
		if trigger.ExitCode != 1 {
			t.Fatalf("trigger.ExitCode = %d, want 1", trigger.ExitCode)
		}
		if !strings.Contains(trigger.OutputText, "No such file or directory") {
			t.Fatalf("trigger.OutputText = %q, want contains missing-file error", trigger.OutputText)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected one auto diagnose trigger when command_start is missing")
	}

	select {
	case <-streamDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("streamPTYOutput did not finish")
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

func TestStreamPTYOutputPromptReadyKeepsUserAutoSwitch(t *testing.T) {
	payloadReader, payloadWriter := io.Pipe()
	defer payloadReader.Close()
	output := &bytes.Buffer{}
	commandLog := NewUTF8RingBuffer(1024)
	tracker := &commandTracker{}
	tracker.Observe([]byte("go test ./...\r"))
	autoTriggers := make(chan diagnoseTrigger, 1)
	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(false)
	autoState.OSCReady.Store(false)

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		streamPTYOutput(payloadReader, output, commandLog, tracker, autoTriggers, autoState)
	}()
	go func() {
		_, _ = payloadWriter.Write([]byte("\x1b]133;C\x07"))
		_, _ = payloadWriter.Write([]byte("fatal: build failed\n"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;D;1\x07"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;A\x07"))
		_ = payloadWriter.Close()
	}()

	select {
	case trigger := <-autoTriggers:
		t.Fatalf("unexpected trigger when auto is disabled by user: %#v", trigger)
	default:
	}
	select {
	case <-streamDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("streamPTYOutput did not finish")
	}
	if autoState.Enabled.Load() {
		t.Fatal("expected auto switch to keep disabled after OSC133 PromptReady")
	}
	if !autoState.OSCReady.Load() {
		t.Fatal("expected OSCReady to be set after PromptReady")
	}
	if !strings.Contains(output.String(), "fatal: build failed") {
		t.Fatalf("output = %q, want contains fatal text", output.String())
	}
}

func TestStreamPTYOutputSuppressesTriggerInAltScreen(t *testing.T) {
	t.Setenv(DiagAltScreenGuardDisableEnv, "")

	payload := strings.NewReader(
		"\x1b[?1049h" +
			"\x1b]133;C\x07" +
			"fatal: build failed in alt screen\n" +
			"\x1b]133;D;1\x07" +
			"\x1b]133;A\x07",
	)
	output := &bytes.Buffer{}
	commandLog := NewUTF8RingBuffer(1024)
	tracker := &commandTracker{}
	tracker.Observe([]byte("go test ./...\r"))
	autoTriggers := make(chan diagnoseTrigger, 1)
	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)

	streamPTYOutput(payload, output, commandLog, tracker, autoTriggers, autoState)

	select {
	case trigger := <-autoTriggers:
		t.Fatalf("unexpected trigger in alt screen: %#v", trigger)
	default:
	}
	if !strings.Contains(output.String(), "fatal: build failed in alt screen") {
		t.Fatalf("output = %q, want contains alt-screen error text", output.String())
	}
}

func TestStreamPTYOutputSuppressesFirstTriggerAfterAltExitOnly(t *testing.T) {
	t.Setenv(DiagAltScreenGuardDisableEnv, "")

	payloadReader, payloadWriter := io.Pipe()
	defer payloadReader.Close()
	output := &bytes.Buffer{}
	commandLog := NewUTF8RingBuffer(1024)
	tracker := &commandTracker{}
	tracker.Observe([]byte("go test ./...\r"))
	autoTriggers := make(chan diagnoseTrigger, 4)
	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)
	autoState.OSCReady.Store(false)

	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		streamPTYOutput(payloadReader, output, commandLog, tracker, autoTriggers, autoState)
	}()
	go func() {
		_, _ = payloadWriter.Write([]byte("\x1b[?1049h"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;C\x07"))
		_, _ = payloadWriter.Write([]byte("fatal first failure\n"))
		_, _ = payloadWriter.Write([]byte("\x1b[?1049l"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;D;1\x07"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;A\x07"))

		_, _ = payloadWriter.Write([]byte("\x1b]133;C\x07"))
		_, _ = payloadWriter.Write([]byte("fatal second failure\n"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;D;1\x07"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;A\x07"))
		_ = payloadWriter.Close()
	}()

	select {
	case <-streamDone:
	case <-time.After(time.Second):
		t.Fatal("streamPTYOutput did not finish")
	}

	triggers := make([]diagnoseTrigger, 0, 2)
drainLoop:
	for {
		select {
		case trigger := <-autoTriggers:
			triggers = append(triggers, trigger)
		default:
			break drainLoop
		}
	}
	if len(triggers) != 1 {
		t.Fatalf("trigger count = %d, want 1", len(triggers))
	}
	if !strings.Contains(triggers[0].OutputText, "fatal second failure") {
		t.Fatalf("trigger output = %q, want second failure", triggers[0].OutputText)
	}
}

func TestStreamPTYOutputAltScreenGuardDisabledByEnv(t *testing.T) {
	t.Setenv(DiagAltScreenGuardDisableEnv, "true")

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
		_, _ = payloadWriter.Write([]byte("\x1b[?1049h"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;C\x07"))
		_, _ = payloadWriter.Write([]byte("fatal: guard disabled should still trigger\n"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;D;1\x07"))
		_, _ = payloadWriter.Write([]byte("\x1b]133;A\x07"))
		_ = payloadWriter.Close()
	}()

	select {
	case trigger := <-autoTriggers:
		if !strings.Contains(trigger.OutputText, "guard disabled should still trigger") {
			t.Fatalf("trigger output = %q, want guard-disabled failure text", trigger.OutputText)
		}
	case <-time.After(time.Second):
		t.Fatal("expected trigger when alt-screen guard is disabled by env")
	}
	select {
	case <-streamDone:
	case <-time.After(time.Second):
		t.Fatal("streamPTYOutput did not finish")
	}
	if !strings.Contains(output.String(), "guard disabled should still trigger") {
		t.Fatalf("output = %q, want contains failure text", output.String())
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

	rpcClient, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress: gatewaySocket,
		TokenFile:     authTokenFile,
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	defer rpcClient.Close()
	authCtx, authCancel := context.WithTimeout(context.Background(), time.Second)
	if err := rpcClient.Authenticate(authCtx); err != nil {
		authCancel()
		t.Fatalf("Authenticate() error = %v", err)
	}
	authCancel()

	buffer := NewUTF8RingBuffer(2048)
	_, _ = buffer.Write([]byte("fallback log"))
	result, err := callDiagnoseTool(
		rpcClient,
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

	rpcClient, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress: gatewaySocket,
		TokenFile:     authTokenFile,
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	defer rpcClient.Close()
	authCtx, authCancel := context.WithTimeout(context.Background(), time.Second)
	if err := rpcClient.Authenticate(authCtx); err != nil {
		authCancel()
		t.Fatalf("Authenticate() error = %v", err)
	}
	authCancel()

	buffer := NewUTF8RingBuffer(1024)
	_, _ = buffer.Write([]byte("fallback log"))
	_, err = callDiagnoseTool(
		rpcClient,
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
	if !strings.Contains(err.Error(), "诊断服务暂不可用") {
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

	const legacyPID = 48213
	legacySocket := filepath.Join(
		legacyTmpDir,
		fmt.Sprintf("%s%d%s", diagSocketFilePrefix, legacyPID, diagSocketFileSuffix),
	)
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

	primarySocket := filepath.Join(
		homeDir,
		".neocode",
		"run",
		fmt.Sprintf("%s%d%s", diagSocketFilePrefix, legacyPID, diagSocketFileSuffix),
	)
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

func TestSendDiagIPCCommandFallbackRejectsMismatchedLegacyPID(t *testing.T) {
	homeDir := t.TempDir()
	legacyTmpDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("TMPDIR", legacyTmpDir)

	const legacyPID = 39101
	legacySocket := filepath.Join(
		legacyTmpDir,
		fmt.Sprintf("%s%d%s", diagSocketFilePrefix, legacyPID, diagSocketFileSuffix),
	)
	listener, resolvedLegacySocket, err := listenDiagSocket(legacySocket)
	if err != nil {
		t.Fatalf("listenDiagSocket() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(resolvedLegacySocket)
	}()

	primarySocket := filepath.Join(
		homeDir,
		".neocode",
		"run",
		fmt.Sprintf("%s%d%s", diagSocketFilePrefix, legacyPID+1, diagSocketFileSuffix),
	)
	_, err = sendDiagIPCCommand(context.Background(), primarySocket, diagIPCRequest{Cmd: diagCommandDiagnose})
	if err == nil {
		t.Fatal("expected sendDiagIPCCommand() to fail when legacy pid mismatches primary pid")
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
	_ = runSingleDiagnosis(
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

func TestRunSingleDiagnosisAutoDisabledSkipsRender(t *testing.T) {
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
			Type:   gateway.FrameTypeAck,
			Action: gateway.FrameActionExecuteSystemTool,
			Payload: map[string]any{
				"Content": `{"confidence":0.8,"root_cause":"ok","fix_commands":["echo fix"],"investigation_commands":["echo inv"]}`,
				"IsError": false,
			},
		})
	})
	defer cleanupServer()

	rpcClient, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress: gatewaySocket,
		TokenFile:     authTokenFile,
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	defer rpcClient.Close()
	authCtx, authCancel := context.WithTimeout(context.Background(), time.Second)
	if err := rpcClient.Authenticate(authCtx); err != nil {
		authCancel()
		t.Fatalf("Authenticate() error = %v", err)
	}
	authCancel()

	buffer := NewUTF8RingBuffer(1024)
	_, _ = buffer.Write([]byte("fallback log"))
	output := &bytes.Buffer{}
	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(false)

	_ = runSingleDiagnosis(
		rpcClient,
		output,
		buffer,
		ManualShellOptions{Workdir: baseDir, Shell: "/bin/bash"},
		filepath.Join(baseDir, "diag.sock"),
		diagnoseTrigger{CommandText: "go test ./...", ExitCode: 1, OutputText: "fatal"},
		true,
		autoState,
	)
	if strings.Contains(output.String(), "NeoCode Diagnosis") {
		t.Fatalf("output should be empty when auto mode is disabled, got %q", output.String())
	}
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatalf("server error = %v", serverErr)
	}
}

func TestConsumeDiagSignalsAndBannerPaths(t *testing.T) {
	t.Run("consume job and notify done", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		jobCh := make(chan diagnoseJob, 1)
		doneCh := make(chan diagIPCResponse, 1)
		jobCh <- diagnoseJob{
			Trigger: diagnoseTrigger{CommandText: "go test ./...", ExitCode: 1, OutputText: "fatal"},
			Done:    doneCh,
			IsAuto:  false,
		}
		close(jobCh)

		output := &bytes.Buffer{}
		autoState := &autoRuntimeState{}
		autoState.Enabled.Store(true)

		consumeDiagSignals(ctx, nil, jobCh, output, NewUTF8RingBuffer(1024), ManualShellOptions{}, "/tmp/diag.sock", autoState, nil, nil)
		select {
		case response := <-doneCh:
			if !response.OK {
				t.Fatalf("response = %#v, want OK", response)
			}
		default:
			t.Fatal("expected consumeDiagSignals to notify job done")
		}
	})

	t.Run("banner helpers", func(t *testing.T) {
		printProxyInitializedBanner(nil)
		printWelcomeBanner(nil)
		printAutoModeBanner(nil, nil)
		printProxyExitedBanner(nil)

		buffer := &bytes.Buffer{}
		printProxyInitializedBanner(buffer)
		printWelcomeBanner(buffer)
		autoOn := &autoRuntimeState{}
		autoOn.Enabled.Store(true)
		printAutoModeBanner(buffer, autoOn)
		autoOff := &autoRuntimeState{}
		autoOff.Enabled.Store(false)
		printAutoModeBanner(buffer, autoOff)
		printProxyExitedBanner(buffer)

		text := buffer.String()
		if !strings.Contains(text, proxyInitializedBanner) || !strings.Contains(text, proxyExitedBanner) {
			t.Fatalf("banner output = %q", text)
		}
	})

	t.Run("auto diagnosis soft failure does not callback", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		jobCh := make(chan diagnoseJob, 1)
		jobCh <- diagnoseJob{
			Trigger: diagnoseTrigger{CommandText: "cat missing", ExitCode: 1, OutputText: "No such file"},
			IsAuto:  true,
		}
		close(jobCh)

		output := &bytes.Buffer{}
		autoState := &autoRuntimeState{}
		autoState.Enabled.Store(true)
		autoState.OSCReady.Store(true)
		callbackTriggered := make(chan error, 1)

		consumeDiagSignals(
			ctx,
			nil,
			jobCh,
			output,
			NewUTF8RingBuffer(1024),
			ManualShellOptions{},
			"/tmp/diag.sock",
			autoState,
			func(err error) { callbackTriggered <- err },
			nil,
		)

		select {
		case <-callbackTriggered:
			t.Fatal("soft failure should not trigger auto diagnosis fatal callback")
		default:
		}
	})
}

func TestShouldTerminateShellOnAutoDiagnoseError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "timeout", err: errors.New("context deadline exceeded"), want: false},
		{name: "rate limit", err: errors.New("provider error: rate_limited"), want: false},
		{name: "provider stream eof", err: errors.New("runtime: subagent provider generate: SDK stream error: EOF"), want: false},
		{name: "unauthorized", err: errors.New("gateway rpc gateway.executeSystemTool failed (unauthorized): unauthorized"), want: true},
		{name: "transport closed", err: errors.New("gateway rpc gateway.executeSystemTool transport error: use of closed network connection"), want: true},
	}

	for _, tc := range tests {
		got := shouldTerminateShellOnAutoDiagnoseError(tc.err)
		if got != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHandleDiagSocketConnectionAutoModeBranches(t *testing.T) {
	execRequest := func(t *testing.T, cmd string, initialEnabled bool, oscReady bool) (diagIPCResponse, bool) {
		t.Helper()
		serverConn, clientConn := net.Pipe()
		defer clientConn.Close()

		autoState := &autoRuntimeState{}
		autoState.Enabled.Store(initialEnabled)
		autoState.OSCReady.Store(oscReady)
		jobCh := make(chan diagnoseJob, 1)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		done := make(chan struct{})
		go func() {
			defer close(done)
			handleDiagSocketConnection(ctx, serverConn, jobCh, autoState)
		}()

		raw, err := json.Marshal(diagIPCRequest{Cmd: cmd})
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		if _, err := clientConn.Write(append(raw, '\n')); err != nil {
			t.Fatalf("write request: %v", err)
		}

		line, err := bufio.NewReader(clientConn).ReadBytes('\n')
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		var response diagIPCResponse
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		<-done
		return response, autoState.Enabled.Load()
	}

	t.Run("auto off", func(t *testing.T) {
		response, enabled := execRequest(t, diagCommandAutoOff, true, true)
		if !response.OK || enabled {
			t.Fatalf("response=%#v enabled=%v, want ok=true enabled=false", response, enabled)
		}
	})

	t.Run("auto status", func(t *testing.T) {
		response, enabled := execRequest(t, diagCommandAutoStatus, false, false)
		if !response.OK || response.Message == "" || response.AutoEnabled != enabled {
			t.Fatalf("response=%#v enabled=%v", response, enabled)
		}
	})

	t.Run("auto on unavailable without osc", func(t *testing.T) {
		response, enabled := execRequest(t, diagCommandAutoOn, false, false)
		if !response.OK || enabled {
			t.Fatalf("response=%#v enabled=%v, want ok=true enabled=false", response, enabled)
		}
		if !strings.Contains(response.Message, "unavailable") {
			t.Fatalf("response message = %q, want contains unavailable", response.Message)
		}
	})

	t.Run("auto on with osc ready", func(t *testing.T) {
		response, enabled := execRequest(t, diagCommandAutoOn, false, true)
		if !response.OK || !enabled {
			t.Fatalf("response=%#v enabled=%v, want ok=true enabled=true", response, enabled)
		}
	})
}

func TestConsumeDiagSignalsClosedChannelReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobCh := make(chan diagnoseJob)
	close(jobCh)

	done := make(chan struct{})
	go func() {
		defer close(done)
		consumeDiagSignals(ctx, nil, jobCh, &bytes.Buffer{}, NewUTF8RingBuffer(1024), ManualShellOptions{}, "/tmp/diag.sock", &autoRuntimeState{}, nil, nil)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumeDiagSignals did not return after channel closed")
	}
}

func TestCallDiagnoseToolDecodePayloadFailure(t *testing.T) {
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
			Type:    gateway.FrameTypeAck,
			Action:  gateway.FrameActionExecuteSystemTool,
			Payload: "plain-text-payload",
		})
	})
	defer cleanupServer()

	rpcClient, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress: gatewaySocket,
		TokenFile:     authTokenFile,
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	defer rpcClient.Close()

	authCtx, authCancel := context.WithTimeout(context.Background(), time.Second)
	if err := rpcClient.Authenticate(authCtx); err != nil {
		authCancel()
		t.Fatalf("Authenticate() error = %v", err)
	}
	authCancel()

	buffer := NewUTF8RingBuffer(1024)
	_, _ = buffer.Write([]byte("decode-failure log"))
	_, err = callDiagnoseTool(
		rpcClient,
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
	if err == nil || !strings.Contains(err.Error(), "诊断结果解析失败") {
		t.Fatalf("expected decode failure error, got %v", err)
	}
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatalf("server error = %v", serverErr)
	}
}

func TestSyncPTYWindowSize(t *testing.T) {
	syncPTYWindowSize(nil, nil)

	file, err := os.CreateTemp(t.TempDir(), "not-pty-*")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer file.Close()

	errWriter := &bytes.Buffer{}
	syncPTYWindowSize(errWriter, file)
	if !strings.Contains(errWriter.String(), "inherit terminal size failed") {
		t.Fatalf("errWriter = %q, want contains inherit terminal size failed", errWriter.String())
	}
}

func TestPrepareBashInitRCAndBuildShellCommand(t *testing.T) {
	t.Run("prepareBashInitRC", func(t *testing.T) {
		originalCreate := createBashInitRCFile
		t.Cleanup(func() { createBashInitRCFile = originalCreate })

		createBashInitRCFile = func() (string, error) { return "/tmp/mock.rc", nil }
		if got := prepareBashInitRC("/bin/bash"); got != "/tmp/mock.rc" {
			t.Fatalf("prepareBashInitRC() = %q, want /tmp/mock.rc", got)
		}
		if got := prepareBashInitRC("/bin/zsh"); got != "" {
			t.Fatalf("prepareBashInitRC(non-bash) = %q, want empty", got)
		}

		createBashInitRCFile = func() (string, error) { return "", errors.New("mock create error") }
		if got := prepareBashInitRC("/bin/bash"); got != "" {
			t.Fatalf("prepareBashInitRC(create error) = %q, want empty", got)
		}
	})

	t.Run("prepareZshInitDir", func(t *testing.T) {
		originalCreate := createZshInitDir
		t.Cleanup(func() { createZshInitDir = originalCreate })

		createZshInitDir = func() (string, error) { return "/tmp/mock-zdotdir", nil }
		if got := prepareZshInitDir("/bin/zsh"); got != "/tmp/mock-zdotdir" {
			t.Fatalf("prepareZshInitDir() = %q, want /tmp/mock-zdotdir", got)
		}
		if got := prepareZshInitDir("/bin/bash"); got != "" {
			t.Fatalf("prepareZshInitDir(non-zsh) = %q, want empty", got)
		}

		createZshInitDir = func() (string, error) { return "", errors.New("mock create error") }
		if got := prepareZshInitDir("/bin/zsh"); got != "" {
			t.Fatalf("prepareZshInitDir(create error) = %q, want empty", got)
		}
	})

	t.Run("buildShellCommand", func(t *testing.T) {
		originalCreate := createBashInitRCFile
		originalDelete := deleteBashInitRCFile
		originalCreateZsh := createZshInitDir
		originalDeleteZsh := deleteZshInitDir
		t.Cleanup(func() { createBashInitRCFile = originalCreate })
		t.Cleanup(func() { deleteBashInitRCFile = originalDelete })
		t.Cleanup(func() { createZshInitDir = originalCreateZsh })
		t.Cleanup(func() { deleteZshInitDir = originalDeleteZsh })

		rcPath := filepath.Join(t.TempDir(), "mock.rc")
		if err := os.WriteFile(rcPath, []byte("mock"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		zdotDirPath := t.TempDir()

		deletedPath := ""
		deletedZdotDir := ""
		createBashInitRCFile = func() (string, error) { return rcPath, nil }
		deleteBashInitRCFile = func(path string) { deletedPath = path }
		createZshInitDir = func() (string, error) { return zdotDirPath, nil }
		deleteZshInitDir = func(path string) { deletedZdotDir = path }

		cmd, cleanup := buildShellCommand("/bin/bash", ManualShellOptions{Workdir: t.TempDir()}, "/tmp/diag.sock", "/tmp/idm.sock")
		if cmd.Path != "/bin/bash" {
			t.Fatalf("cmd.Path = %q, want /bin/bash", cmd.Path)
		}
		if !strings.Contains(strings.Join(cmd.Args, " "), "--rcfile") {
			t.Fatalf("cmd.Args = %#v, want contains --rcfile", cmd.Args)
		}
		foundDiagEnv := false
		foundIDMEnv := false
		for _, env := range cmd.Env {
			if strings.HasPrefix(env, DiagSocketEnv+"=") && strings.HasSuffix(env, "/tmp/diag.sock") {
				foundDiagEnv = true
			}
			if strings.HasPrefix(env, IDMDiagSocketEnv+"=") && strings.HasSuffix(env, "/tmp/idm.sock") {
				foundIDMEnv = true
			}
		}
		if !foundDiagEnv {
			t.Fatalf("cmd.Env = %#v, want %s", cmd.Env, DiagSocketEnv)
		}
		if !foundIDMEnv {
			t.Fatalf("cmd.Env = %#v, want %s", cmd.Env, IDMDiagSocketEnv)
		}
		cleanup()
		if deletedPath != rcPath {
			t.Fatalf("deletedPath = %q, want %q", deletedPath, rcPath)
		}

		cmd, cleanup = buildShellCommand("/bin/zsh", ManualShellOptions{Workdir: t.TempDir()}, "/tmp/diag.sock", "/tmp/idm.sock")
		if strings.Contains(strings.Join(cmd.Args, " "), "--rcfile") {
			t.Fatalf("zsh cmd.Args = %#v, should not contain --rcfile", cmd.Args)
		}
		foundZdotDir := false
		for _, env := range cmd.Env {
			if env == "ZDOTDIR="+zdotDirPath {
				foundZdotDir = true
				break
			}
		}
		if !foundZdotDir {
			t.Fatalf("zsh cmd.Env = %#v, want contains ZDOTDIR", cmd.Env)
		}
		cleanup()
		if deletedZdotDir != zdotDirPath {
			t.Fatalf("deletedZdotDir = %q, want %q", deletedZdotDir, zdotDirPath)
		}
	})
}

func TestDefaultBashInitRCFileLifecycle(t *testing.T) {
	path, err := defaultCreateBashInitRCFile()
	if err != nil {
		t.Fatalf("defaultCreateBashInitRCFile() error = %v", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	text := string(data)
	if !strings.Contains(text, "neocode shell integration") {
		t.Fatalf("rc file content missing integration marker: %q", text)
	}
	if !strings.Contains(text, ". ~/.bashrc") {
		t.Fatalf("rc file content missing ~/.bashrc include: %q", text)
	}
	if strings.Index(text, ". ~/.bashrc") > strings.Index(text, "neocode shell integration") {
		t.Fatalf("expected ~/.bashrc include before integration hook, got: %q", text)
	}

	defaultDeleteBashInitRCFile(path)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected rc file removed, stat err = %v", statErr)
	}

	defaultDeleteBashInitRCFile("")
}

func TestDefaultZshInitDirLifecycle(t *testing.T) {
	path, err := defaultCreateZshInitDir()
	if err != nil {
		t.Fatalf("defaultCreateZshInitDir() error = %v", err)
	}
	rcPath := filepath.Join(path, ".zshrc")
	data, readErr := os.ReadFile(rcPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	text := string(data)
	if !strings.Contains(text, "neocode shell integration") {
		t.Fatalf("zsh rc content missing integration marker: %q", text)
	}
	if !strings.Contains(text, ". \"${HOME}/.zshrc\"") {
		t.Fatalf("zsh rc content missing ${HOME}/.zshrc include: %q", text)
	}
	if strings.Index(text, ". \"${HOME}/.zshrc\"") > strings.Index(text, "neocode shell integration") {
		t.Fatalf("expected ${HOME}/.zshrc include before integration hook, got: %q", text)
	}

	defaultDeleteZshInitDir(path)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected zsh init dir removed, stat err = %v", statErr)
	}

	defaultDeleteZshInitDir("")
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
