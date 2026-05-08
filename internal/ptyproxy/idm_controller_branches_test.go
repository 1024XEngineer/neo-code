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
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

func newAuthenticatedIDMRPCClient(
	t *testing.T,
	handler func(decoder *json.Decoder, encoder *json.Encoder) error,
) (*gatewayclient.GatewayRPCClient, func()) {
	t.Helper()

	socketDir := t.TempDir()
	gatewaySocket := filepath.Join(socketDir, "gateway.sock")
	tokenFile := filepath.Join(socketDir, "auth.json")
	writeGatewayAuthTokenFile(t, tokenFile, "test-token")

	cleanupServer, serverDone := startGatewayRPCMockServer(t, gatewaySocket, func(decoder *json.Decoder, encoder *json.Encoder) error {
		authReq, err := readRPCRequest(decoder)
		if err != nil {
			return err
		}
		if authReq.Method != protocol.MethodGatewayAuthenticate {
			return fmt.Errorf("unexpected first method %s", authReq.Method)
		}
		if err := writeRPCResult(encoder, authReq.ID, gateway.MessageFrame{
			Type:   gateway.FrameTypeAck,
			Action: gateway.FrameActionAuthenticate,
		}); err != nil {
			return err
		}
		return handler(decoder, encoder)
	})

	client, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress: gatewaySocket,
		TokenFile:     tokenFile,
	})
	if err != nil {
		cleanupServer()
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}

	authCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	if err := client.Authenticate(authCtx); err != nil {
		cancel()
		_ = client.Close()
		cleanupServer()
		t.Fatalf("Authenticate() error = %v", err)
	}
	cancel()

	cleanup := func() {
		_ = client.Close()
		cleanupServer()
		if serverErr := <-serverDone; serverErr != nil && !errors.Is(serverErr, io.EOF) {
			t.Fatalf("mock gateway server error = %v", serverErr)
		}
	}
	return client, cleanup
}

func TestIDMControllerLifecycleAndStreaming(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	client, cleanup := newAuthenticatedIDMRPCClient(t, func(decoder *json.Decoder, encoder *json.Encoder) error {
		createReq, err := readRPCRequest(decoder)
		if err != nil {
			return err
		}
		if createReq.Method != protocol.MethodGatewayCreateSession {
			return fmt.Errorf("unexpected create method %s", createReq.Method)
		}
		if err := writeRPCResult(encoder, createReq.ID, gateway.MessageFrame{
			Type:   gateway.FrameTypeAck,
			Action: gateway.FrameActionCreateSession,
		}); err != nil {
			return err
		}

		activateReq, err := readRPCRequest(decoder)
		if err != nil {
			return err
		}
		if activateReq.Method != protocol.MethodGatewayActivateSessionSkill {
			return fmt.Errorf("unexpected activate method %s", activateReq.Method)
		}
		if err := writeRPCResult(encoder, activateReq.ID, gateway.MessageFrame{
			Type:   gateway.FrameTypeAck,
			Action: gateway.FrameActionActivateSessionSkill,
		}); err != nil {
			return err
		}

		deleteReq, err := readRPCRequest(decoder)
		if err != nil {
			return err
		}
		if deleteReq.Method != protocol.MethodGatewayDeleteSession {
			return fmt.Errorf("unexpected delete method %s", deleteReq.Method)
		}
		return writeRPCResult(encoder, deleteReq.ID, gateway.MessageFrame{
			Type:   gateway.FrameTypeAck,
			Action: gateway.FrameActionDeleteSession,
		})
	})
	defer cleanup()

	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)
	autoState.OSCReady.Store(true)
	logBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity)
	output := &bytes.Buffer{}

	controller := newIDMController(idmControllerOptions{
		PTYWriter:  &bytes.Buffer{},
		Output:     output,
		RPCClient:  client,
		AutoState:  autoState,
		LogBuffer:  logBuffer,
		DefaultCap: DefaultRingBufferCapacity,
		Workdir:    "/tmp/workdir",
	})

	if err := controller.Enter(); err != nil {
		t.Fatalf("Enter() error = %v", err)
	}
	if !controller.IsActive() {
		t.Fatal("controller should be active after enter")
	}
	if controller.currentSessionID() == "" {
		t.Fatal("currentSessionID() should not be empty")
	}
	if controller.ShouldPassthroughInput() {
		t.Fatal("idle controller should not passthrough input")
	}

	controller.Exit()

	text := output.String()
	if !strings.Contains(text, "已进入 IDM") || !strings.Contains(text, "已退出 IDM") {
		t.Fatalf("output = %q, want lifecycle messages", text)
	}
}

func TestIDMControllerPermissionAndWaitBranches(t *testing.T) {
	t.Run("run canceled and runtime error", func(t *testing.T) {
		client := &gatewayclient.GatewayRPCClient{}
		notifications := make(chan gatewayclient.Notification, 4)
		setGatewayClientNotifications(client, notifications)
		controller := newIDMController(idmControllerOptions{Output: &bytes.Buffer{}, RPCClient: client})

		notifications <- gatewayclient.Notification{
			Method: protocol.MethodGatewayEvent,
			Params: mustMarshalJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeEvent,
				SessionID: "session-2",
				RunID:     "run-2",
				Payload:   map[string]any{"runtime_event_type": "run_canceled"},
			}),
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := controller.waitRunStream(ctx, "session-2", "run-2"); !errors.Is(err, context.Canceled) {
			t.Fatalf("waitRunStream canceled err = %v, want context.Canceled", err)
		}

		notifications <- gatewayclient.Notification{
			Method: protocol.MethodGatewayEvent,
			Params: mustMarshalJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeEvent,
				SessionID: "session-3",
				RunID:     "run-3",
				Payload: map[string]any{
					"runtime_event_type": "error",
					"payload":            "runtime boom",
				},
			}),
		}
		ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
		defer cancel2()
		if err := controller.waitRunStream(ctx2, "session-3", "run-3"); err == nil || !strings.Contains(err.Error(), "runtime boom") {
			t.Fatalf("waitRunStream error err = %v, want runtime boom", err)
		}
	})

	t.Run("chunk and done render answer", func(t *testing.T) {
		client := &gatewayclient.GatewayRPCClient{}
		notifications := make(chan gatewayclient.Notification, 4)
		setGatewayClientNotifications(client, notifications)
		output := &bytes.Buffer{}
		controller := newIDMController(idmControllerOptions{Output: output, RPCClient: client})

		notifications <- gatewayclient.Notification{
			Method: protocol.MethodGatewayEvent,
			Params: mustMarshalJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeEvent,
				SessionID: "session-4",
				RunID:     "run-4",
				Payload: map[string]any{
					"runtime_event_type": "agent_chunk",
					"payload":            "# 标题\n\n- 诊断完成",
				},
			}),
		}
		notifications <- gatewayclient.Notification{
			Method: protocol.MethodGatewayEvent,
			Params: mustMarshalJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeEvent,
				SessionID: "session-4",
				RunID:     "run-4",
				Payload:   map[string]any{"runtime_event_type": "agent_done"},
			}),
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := controller.waitRunStream(ctx, "session-4", "run-4"); err != nil {
			t.Fatalf("waitRunStream() error = %v", err)
		}
		if !strings.Contains(output.String(), "标题") {
			t.Fatalf("output = %q, want rendered answer", output.String())
		}
	})
}

func TestIDMControllerRPCBranches(t *testing.T) {
	t.Run("send ai message success with injected notifications", func(t *testing.T) {
		client, cleanup := newAuthenticatedIDMRPCClient(t, func(decoder *json.Decoder, encoder *json.Encoder) error {
			bindReq, err := readRPCRequest(decoder)
			if err != nil {
				return err
			}
			if bindReq.Method != protocol.MethodGatewayBindStream {
				return fmt.Errorf("unexpected bind method %s", bindReq.Method)
			}
			if err := writeRPCResult(encoder, bindReq.ID, gateway.MessageFrame{
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionBindStream,
			}); err != nil {
				return err
			}

			runReq, err := readRPCRequest(decoder)
			if err != nil {
				return err
			}
			if runReq.Method != protocol.MethodGatewayRun {
				return fmt.Errorf("unexpected run method %s", runReq.Method)
			}
			if err := writeRPCResult(encoder, runReq.ID, gateway.MessageFrame{
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionRun,
			}); err != nil {
				return err
			}

			cancelReq, err := readRPCRequest(decoder)
			if err != nil {
				return err
			}
			if cancelReq.Method != protocol.MethodGatewayCancel {
				return fmt.Errorf("unexpected cancel method %s", cancelReq.Method)
			}
			return writeRPCResult(encoder, cancelReq.ID, gateway.MessageFrame{
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionCancel,
			})
		})
		defer cleanup()

		notifications := make(chan gatewayclient.Notification, 4)
		setGatewayClientNotifications(client, notifications)

		output := &bytes.Buffer{}
		controller := newIDMController(idmControllerOptions{Output: output, RPCClient: client, Workdir: "/tmp"})
		controller.mu.Lock()
		controller.active = true
		controller.sessionID = "session-send"
		controller.sessionReady = true
		controller.mu.Unlock()

		go func() {
			time.Sleep(10 * time.Millisecond)
			notifications <- gatewayclient.Notification{
				Method: protocol.MethodGatewayEvent,
				Params: mustMarshalJSON(t, gateway.MessageFrame{
					Type:      gateway.FrameTypeEvent,
					SessionID: "session-send",
					RunID:     strings.TrimSpace(controller.currentRunID),
					Payload: map[string]any{
						"runtime_event_type": "agent_chunk",
						"payload":            "诊断结果",
					},
				}),
			}
			notifications <- gatewayclient.Notification{
				Method: protocol.MethodGatewayEvent,
				Params: mustMarshalJSON(t, gateway.MessageFrame{
					Type:      gateway.FrameTypeEvent,
					SessionID: "session-send",
					RunID:     strings.TrimSpace(controller.currentRunID),
					Payload:   map[string]any{"runtime_event_type": "agent_done"},
				}),
			}
		}()

		if err := controller.sendAIMessage("请分析"); err != nil {
			t.Fatalf("sendAIMessage() error = %v", err)
		}
		if !strings.Contains(output.String(), "诊断结果") {
			t.Fatalf("output = %q, want diagnosis content", output.String())
		}
	})

	t.Run("create activate resolve and reject branches", func(t *testing.T) {
		client, cleanup := newAuthenticatedIDMRPCClient(t, func(decoder *json.Decoder, encoder *json.Encoder) error {
			requests := []gateway.MessageFrame{
				{Type: gateway.FrameTypeError, Error: &gateway.FrameError{Code: "bad_create", Message: "boom"}},
				{Type: gateway.FrameTypeAck, Action: gateway.FrameActionCreateSession},
				{Type: gateway.FrameTypeEvent},
				{Type: gateway.FrameTypeAck, Action: gateway.FrameActionActivateSessionSkill},
				{Type: gateway.FrameTypeError, Error: &gateway.FrameError{Code: "bad_perm", Message: "denied"}},
				{Type: gateway.FrameTypeAck, Action: gateway.FrameActionResolvePermission},
			}
			for _, frame := range requests {
				req, err := readRPCRequest(decoder)
				if err != nil {
					return err
				}
				if err := writeRPCResult(encoder, req.ID, frame); err != nil {
					return err
				}
			}
			return nil
		})
		defer cleanup()

		controller := newIDMController(idmControllerOptions{Output: &bytes.Buffer{}, RPCClient: client})
		if err := controller.createSession("session-rpc"); err == nil {
			t.Fatal("expected create session error frame")
		}
		if err := controller.createSession("session-rpc"); err != nil {
			t.Fatalf("createSession() error = %v", err)
		}
		if err := controller.activateSessionSkill("session-rpc", idmSkillID); err == nil {
			t.Fatal("expected unexpected frame type error")
		}
		if err := controller.activateSessionSkill("session-rpc", idmSkillID); err != nil {
			t.Fatalf("activateSessionSkill() error = %v", err)
		}
		if err := controller.resolvePermission("req-1", ""); err == nil {
			t.Fatal("expected resolve permission frame error")
		}
		if err := controller.resolvePermission("req-2", ""); err != nil {
			t.Fatalf("resolvePermission() error = %v", err)
		}
	})
}

func TestIDMControllerInputAndSocketBranches(t *testing.T) {
	t.Run("input editing helpers", func(t *testing.T) {
		output := &bytes.Buffer{}
		ptyWriter := &bytes.Buffer{}
		controller := newIDMController(idmControllerOptions{PTYWriter: ptyWriter, Output: output})
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeIdle
		controller.mu.Unlock()

		controller.handleUTF8Byte('a')
		controller.handleUTF8Byte(0xe4)
		controller.handleUTF8Byte(0xb8)
		controller.handleUTF8Byte(0xad)
		controller.handleBackspace()
		controller.flushPendingUTF8()
		controller.HandleInputByte('\n')

		if !strings.Contains(output.String(), "a中") {
			t.Fatalf("output = %q, want echoed utf8 input", output.String())
		}
	})

	t.Run("send native command branches", func(t *testing.T) {
		controller := newIDMController(idmControllerOptions{PTYWriter: &bytes.Buffer{}, Output: &bytes.Buffer{}})
		if err := controller.sendNativeCommand("  "); err != nil {
			t.Fatalf("sendNativeCommand(empty) error = %v", err)
		}

		controller.mu.Lock()
		controller.active = true
		controller.mu.Unlock()
		if err := controller.sendNativeCommand("echo hi"); err != nil {
			t.Fatalf("sendNativeCommand() error = %v", err)
		}
		if !controller.ShouldPassthroughInput() {
			t.Fatal("native command should switch controller to passthrough mode")
		}

		controller = newIDMController(idmControllerOptions{
			PTYWriter: failingWriter{err: errors.New("write failed")},
			Output:    &bytes.Buffer{},
		})
		controller.mu.Lock()
		controller.active = true
		controller.mu.Unlock()
		if err := controller.sendNativeCommand("echo boom"); err == nil {
			t.Fatal("expected write error")
		}
	})

	t.Run("handle input line branches", func(t *testing.T) {
		ptyWriter := &bytes.Buffer{}
		controller := newIDMController(idmControllerOptions{PTYWriter: ptyWriter, Output: &bytes.Buffer{}})
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeIdle
		controller.autoSnapshot = true
		controller.mu.Unlock()

		controller.handleInputLine("")
		controller.handleInputLine("\\@ai literal")
		controller.OnShellEvent(ShellEvent{Type: ShellEventCommandDone})
		controller.handleInputLine("exit")

		if !strings.Contains(ptyWriter.String(), "@ai literal\n") {
			t.Fatalf("ptyWriter = %q, want passthrough command", ptyWriter.String())
		}
		if controller.IsActive() {
			t.Fatal("exit route should deactivate controller")
		}
	})

	t.Run("handle socket connection responses", func(t *testing.T) {
		run := func(request []byte, controller *idmController) diagIPCResponse {
			serverConn, clientConn := net.Pipe()
			defer clientConn.Close()

			done := make(chan struct{})
			go func() {
				handleIDMSocketConnection(serverConn, controller)
				close(done)
			}()
			_, _ = clientConn.Write(request)
			line, _ := bufio.NewReader(clientConn).ReadBytes('\n')
			<-done

			var response diagIPCResponse
			_ = json.Unmarshal(line, &response)
			return response
		}

		if response := run([]byte("{bad\n"), nil); response.Message != "invalid request" {
			t.Fatalf("invalid request response = %#v", response)
		}
		raw, _ := json.Marshal(diagIPCRequest{Cmd: "unsupported"})
		if response := run(append(raw, '\n'), nil); response.Message != "unsupported command" {
			t.Fatalf("unsupported response = %#v", response)
		}
		raw, _ = json.Marshal(diagIPCRequest{Cmd: diagCommandIDMEnter})
		if response := run(append(raw, '\n'), nil); response.Message != "idm controller is unavailable" {
			t.Fatalf("nil controller response = %#v", response)
		}

		controller := newIDMController(idmControllerOptions{Output: &bytes.Buffer{}})
		controller.mu.Lock()
		controller.active = true
		controller.mu.Unlock()
		if response := run(append(raw, '\n'), controller); !response.OK {
			t.Fatalf("success response = %#v, want ok", response)
		}
	})

	t.Run("serve idm socket handles accept errors", func(t *testing.T) {
		listener := &scriptedIDMListener{
			accepts: []idmAcceptResult{
				{err: errors.New("temporary accept failure")},
				{err: net.ErrClosed},
			},
		}
		errBuffer := &bytes.Buffer{}
		serveIDMSocket(context.Background(), listener, nil, errBuffer)
		if !strings.Contains(errBuffer.String(), "accept signal error") {
			t.Fatalf("errBuffer = %q, want accept error log", errBuffer.String())
		}
	})
}

func TestIDMControllerUtilityBranches(t *testing.T) {
	t.Run("runtime payload helpers", func(t *testing.T) {
		if _, ok := extractIDMRuntimeEnvelope(nil); ok {
			t.Fatal("nil payload should not extract envelope")
		}
		if envelope, ok := extractIDMRuntimeEnvelope(map[string]any{
			"runtime_event_type": "agent_chunk",
			"payload":            "direct",
		}); !ok || readMapStringValue(envelope, "runtime_event_type") != "agent_chunk" {
			t.Fatalf("direct envelope = %#v ok=%v", envelope, ok)
		}
		if envelope, ok := extractIDMRuntimeEnvelope(map[string]any{
			"payload": map[string]any{"runtime_event_type": "agent_done"},
		}); !ok || readMapStringValue(envelope, "runtime_event_type") != "agent_done" {
			t.Fatalf("nested envelope = %#v ok=%v", envelope, ok)
		}
		if got := readMapStringValue(map[string]any{"n": 7}, "n"); got != "7" {
			t.Fatalf("readMapStringValue() = %q, want 7", got)
		}
		if got := stringifyRuntimePayload(map[string]any{"k": "v"}); !strings.Contains(got, `"k":"v"`) {
			t.Fatalf("stringifyRuntimePayload() = %q", got)
		}
		if got := stringifyRuntimePayload(make(chan int)); !strings.Contains(got, "0x") {
			t.Fatalf("stringifyRuntimePayload(stringer fallback) = %q", got)
		}
	})

	t.Run("render fallback and prompt messages", func(t *testing.T) {
		originalOnce := idmMarkdownRendererOnce
		originalRenderer := idmMarkdownRenderer
		originalErr := idmMarkdownRendererErr
		t.Cleanup(func() {
			idmMarkdownRendererOnce = originalOnce
			idmMarkdownRenderer = originalRenderer
			idmMarkdownRendererErr = originalErr
		})

		idmMarkdownRendererOnce = sync.Once{}
		idmMarkdownRenderer = nil
		idmMarkdownRendererErr = errors.New("render boom")

		output := &bytes.Buffer{}
		controller := newIDMController(idmControllerOptions{Output: output})
		controller.renderIDMAnswer("raw answer")
		controller.writeSystemMessage(" system ")
		controller.writeFriendlyMessage(" friendly ")
		controller.writeRawOutput(nil)

		text := output.String()
		if !strings.Contains(text, "raw") || !strings.Contains(text, "answer") ||
			!strings.Contains(text, "system") || !strings.Contains(text, "friendly") {
			t.Fatalf("output = %q, want fallback/system/friendly text", text)
		}
	})

	t.Run("skill file and pid helpers", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

		if err := ensureTerminalDiagnosisSkillFile(); err != nil {
			t.Fatalf("ensureTerminalDiagnosisSkillFile() error = %v", err)
		}
		if err := ensureTerminalDiagnosisSkillFile(); err != nil {
			t.Fatalf("ensureTerminalDiagnosisSkillFile() second call error = %v", err)
		}
		if _, ok := parseIDMSessionPID("idm-bad"); ok {
			t.Fatal("invalid idm session should not parse")
		}
		if pid, ok := parseIDMSessionPID(generateIDMSessionID(1234)); !ok || pid != 1234 {
			t.Fatalf("parseIDMSessionPID() = (%d,%v), want (1234,true)", pid, ok)
		}
		if isProcessAlive(-1) {
			t.Fatal("negative pid should not be alive")
		}
		if !isProcessAlive(syscall.Getpid()) {
			t.Fatal("current pid should be alive")
		}
	})

	t.Run("finish streaming and resolve permission validation", func(t *testing.T) {
		controller := newIDMController(idmControllerOptions{Output: &bytes.Buffer{}})
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeStreaming
		controller.currentRunID = "run-1"
		controller.streamCancel = func() {}
		controller.sessionID = "session-1"
		controller.mu.Unlock()

		controller.finishStreaming("other")
		if controller.currentSessionID() != "session-1" {
			t.Fatalf("currentSessionID() = %q, want session-1", controller.currentSessionID())
		}
		controller.finishStreaming("run-1")
		controller.mu.Lock()
		defer controller.mu.Unlock()
		if controller.mode != idmModeIdle || controller.currentRunID != "" || controller.streamCancel != nil {
			t.Fatalf("controller not reset after finishStreaming: %+v", controller)
		}

		if err := (*idmController)(nil).resolvePermission("req", "reject"); err == nil {
			t.Fatal("nil controller should reject resolvePermission")
		}
	})

	t.Run("rollback and permission payload helpers", func(t *testing.T) {
		autoState := &autoRuntimeState{}
		autoState.Enabled.Store(false)
		logBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity)
		controller := newIDMController(idmControllerOptions{Output: &bytes.Buffer{}, AutoState: autoState, LogBuffer: logBuffer})
		controller.autoSnapshot = true
		controller.defaultRingCapacity = DefaultRingBufferCapacity
		controller.active = true
		controller.sessionID = "session-rollback"
		controller.rollbackEnter("")

		if !autoState.Enabled.Load() {
			t.Fatal("rollbackEnter() should restore auto state")
		}
		if requestID, toolName := readPermissionRequestFromPayload(struct {
			RequestID string `json:"RequestID"`
			ToolName  string `json:"ToolName"`
		}{RequestID: "req-x", ToolName: "filesystem"}); requestID != "req-x" || toolName != "filesystem" {
			t.Fatalf("requestID/toolName = %q/%q", requestID, toolName)
		}
	})

	t.Run("reject permission helper succeeds", func(t *testing.T) {
		client, cleanup := newAuthenticatedIDMRPCClient(t, func(decoder *json.Decoder, encoder *json.Encoder) error {
			req, err := readRPCRequest(decoder)
			if err != nil {
				return err
			}
			if req.Method != protocol.MethodGatewayResolvePermission {
				return fmt.Errorf("unexpected method %s", req.Method)
			}
			return writeRPCResult(encoder, req.ID, gateway.MessageFrame{
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionResolvePermission,
			})
		})
		defer cleanup()

		controller := newIDMController(idmControllerOptions{Output: &bytes.Buffer{}, RPCClient: client})
		err := controller.rejectPermissionInIDM(map[string]any{
			"request_id": "perm-ok",
			"tool_name":  "",
		})
		if err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("err = %v, want unknown tool reject message", err)
		}
	})
}

func TestIDMControllerPhase4RunModeAndAckFailures(t *testing.T) {
	t.Run("done payload fallback renders answer", func(t *testing.T) {
		client := &gatewayclient.GatewayRPCClient{}
		notifications := make(chan gatewayclient.Notification, 4)
		setGatewayClientNotifications(client, notifications)
		output := &bytes.Buffer{}
		controller := newIDMController(idmControllerOptions{Output: output, RPCClient: client})

		notifications <- gatewayclient.Notification{
			Method: protocol.MethodGatewayEvent,
			Params: mustMarshalJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeEvent,
				SessionID: "session-fallback",
				RunID:     "run-fallback",
				Payload: map[string]any{
					"runtime_event_type": "agent_done",
					"payload": map[string]any{
						"parts": []any{
							map[string]any{"kind": "text", "text": "done payload fallback"},
						},
					},
				},
			}),
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := controller.waitRunStream(ctx, "session-fallback", "run-fallback"); err != nil {
			t.Fatalf("waitRunStream() error = %v", err)
		}
		visibleOutput := normalizeWhitespace(stripANSI(output.String()))
		if !strings.Contains(visibleOutput, "done payload fallback") {
			t.Fatalf("output = %q, want done payload fallback", output.String())
		}
	})

	t.Run("chunk renders before done", func(t *testing.T) {
		client := &gatewayclient.GatewayRPCClient{}
		notifications := make(chan gatewayclient.Notification, 4)
		setGatewayClientNotifications(client, notifications)
		output := &synchronizedBuffer{}
		controller := newIDMController(idmControllerOptions{Output: output, RPCClient: client})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		waitDone := make(chan error, 1)
		go func() {
			waitDone <- controller.waitRunStream(ctx, "session-stream", "run-stream")
		}()

		notifications <- gatewayclient.Notification{
			Method: protocol.MethodGatewayEvent,
			Params: mustMarshalJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeEvent,
				SessionID: "session-stream",
				RunID:     "run-stream",
				Payload: map[string]any{
					"runtime_event_type": "agent_chunk",
					"payload":            "streaming chunk",
				},
			}),
		}

		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case err := <-waitDone:
				t.Fatalf("waitRunStream returned before agent_done: %v", err)
			case <-ticker.C:
				if strings.Contains(output.String(), "streaming chunk") {
					goto chunkRendered
				}
			case <-ctx.Done():
				t.Fatal("chunk was not rendered before agent_done")
			}
		}

	chunkRendered:
		notifications <- gatewayclient.Notification{
			Method: protocol.MethodGatewayEvent,
			Params: mustMarshalJSON(t, gateway.MessageFrame{
				Type:      gateway.FrameTypeEvent,
				SessionID: "session-stream",
				RunID:     "run-stream",
				Payload:   map[string]any{"runtime_event_type": "agent_done"},
			}),
		}

		select {
		case err := <-waitDone:
			if err != nil {
				t.Fatalf("waitRunStream() error = %v", err)
			}
		case <-ctx.Done():
			t.Fatal("waitRunStream did not finish after agent_done")
		}
	})

	t.Run("run mode follows env and run ack failures reset state", func(t *testing.T) {
		tests := []struct {
			name     string
			envValue string
			runFrame gateway.MessageFrame
			wantMode string
			wantErr  string
		}{
			{
				name:     "plan enabled with run error",
				envValue: "",
				runFrame: gateway.MessageFrame{
					Type:  gateway.FrameTypeError,
					Error: &gateway.FrameError{Code: "bad_run", Message: "run failed"},
				},
				wantMode: idmPlanMode,
				wantErr:  "gateway run failed",
			},
			{
				name:     "plan disabled with run error",
				envValue: "1",
				runFrame: gateway.MessageFrame{
					Type:  gateway.FrameTypeError,
					Error: &gateway.FrameError{Code: "bad_run", Message: "run failed"},
				},
				wantMode: "",
				wantErr:  "gateway run failed",
			},
			{
				name:     "unexpected run frame",
				envValue: "",
				runFrame: gateway.MessageFrame{
					Type: gateway.FrameTypeEvent,
				},
				wantMode: idmPlanMode,
				wantErr:  "unexpected gateway frame type for run",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Setenv(IDMSessionPlanModeDisableEnv, tt.envValue)
				modeSeen := make(chan string, 1)
				client, cleanup := newAuthenticatedIDMRPCClient(t, func(decoder *json.Decoder, encoder *json.Encoder) error {
					bindReq, err := readRPCRequest(decoder)
					if err != nil {
						return err
					}
					if bindReq.Method != protocol.MethodGatewayBindStream {
						return fmt.Errorf("unexpected bind method %s", bindReq.Method)
					}
					if err := writeRPCResult(encoder, bindReq.ID, gateway.MessageFrame{
						Type:   gateway.FrameTypeAck,
						Action: gateway.FrameActionBindStream,
					}); err != nil {
						return err
					}

					runReq, err := readRPCRequest(decoder)
					if err != nil {
						return err
					}
					if runReq.Method != protocol.MethodGatewayRun {
						return fmt.Errorf("unexpected run method %s", runReq.Method)
					}
					var runParams protocol.RunParams
					if err := json.Unmarshal(runReq.Params, &runParams); err != nil {
						return fmt.Errorf("unmarshal run params: %w", err)
					}
					modeSeen <- runParams.Mode
					return writeRPCResult(encoder, runReq.ID, tt.runFrame)
				})
				defer cleanup()

				controller := newIDMController(idmControllerOptions{Output: &bytes.Buffer{}, RPCClient: client, Workdir: "/tmp"})
				controller.mu.Lock()
				controller.active = true
				controller.sessionID = "session-send"
				controller.sessionReady = true
				controller.mu.Unlock()

				err := controller.sendAIMessage("please analyze")
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("sendAIMessage() error = %v, want contains %q", err, tt.wantErr)
				}
				if got := <-modeSeen; got != tt.wantMode {
					t.Fatalf("run mode = %q, want %q", got, tt.wantMode)
				}
				controller.mu.Lock()
				defer controller.mu.Unlock()
				if controller.mode != idmModeIdle || controller.currentRunID != "" || controller.streamCancel != nil {
					t.Fatalf(
						"controller did not reset streaming state: mode=%q run_id=%q cancel_nil=%v",
						controller.mode,
						controller.currentRunID,
						controller.streamCancel == nil,
					)
				}
			})
		}
	})

	t.Run("bind ack failures reset state", func(t *testing.T) {
		tests := []struct {
			name    string
			frame   gateway.MessageFrame
			wantErr string
		}{
			{
				name: "bind error frame",
				frame: gateway.MessageFrame{
					Type:  gateway.FrameTypeError,
					Error: &gateway.FrameError{Code: "bad_bind", Message: "bind failed"},
				},
				wantErr: "gateway bind_stream failed",
			},
			{
				name:    "unexpected bind frame",
				frame:   gateway.MessageFrame{Type: gateway.FrameTypeEvent},
				wantErr: "unexpected gateway frame type for bind_stream",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				client, cleanup := newAuthenticatedIDMRPCClient(t, func(decoder *json.Decoder, encoder *json.Encoder) error {
					bindReq, err := readRPCRequest(decoder)
					if err != nil {
						return err
					}
					if bindReq.Method != protocol.MethodGatewayBindStream {
						return fmt.Errorf("unexpected bind method %s", bindReq.Method)
					}
					return writeRPCResult(encoder, bindReq.ID, tt.frame)
				})
				defer cleanup()

				controller := newIDMController(idmControllerOptions{Output: &bytes.Buffer{}, RPCClient: client, Workdir: "/tmp"})
				controller.mu.Lock()
				controller.active = true
				controller.sessionID = "session-bind"
				controller.sessionReady = true
				controller.mu.Unlock()

				err := controller.sendAIMessage("please analyze")
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("sendAIMessage() error = %v, want contains %q", err, tt.wantErr)
				}
				controller.mu.Lock()
				defer controller.mu.Unlock()
				if controller.mode != idmModeIdle || controller.currentRunID != "" || controller.streamCancel != nil {
					t.Fatalf(
						"controller did not reset streaming state: mode=%q run_id=%q cancel_nil=%v",
						controller.mode,
						controller.currentRunID,
						controller.streamCancel == nil,
					)
				}
			})
		}
	})
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

type synchronizedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func mustMarshalJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return raw
}

func setGatewayClientNotifications(client *gatewayclient.GatewayRPCClient, ch chan gatewayclient.Notification) {
	field := reflect.ValueOf(client).Elem().FieldByName("notifications")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(ch))
}

type idmAcceptResult struct {
	conn net.Conn
	err  error
}

type scriptedIDMListener struct {
	accepts []idmAcceptResult
	index   int
}

func (l *scriptedIDMListener) Accept() (net.Conn, error) {
	if l.index >= len(l.accepts) {
		return nil, net.ErrClosed
	}
	result := l.accepts[l.index]
	l.index++
	return result.conn, result.err
}

func (l *scriptedIDMListener) Close() error { return nil }

func (l *scriptedIDMListener) Addr() net.Addr { return &net.UnixAddr{} }
