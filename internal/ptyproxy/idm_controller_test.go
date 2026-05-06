//go:build !windows

package ptyproxy

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestIDMControllerSignalSemantics(t *testing.T) {
	t.Run("idle mode consumes ctrl+c and exits", func(t *testing.T) {
		controller := newIDMController(idmControllerOptions{
			PTYWriter: &bytes.Buffer{},
			Output:    &bytes.Buffer{},
			AutoState: &autoRuntimeState{},
			LogBuffer: NewUTF8RingBuffer(DefaultRingBufferCapacity),
		})
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeIdle
		controller.autoSnapshot = true
		controller.mu.Unlock()

		handled := controller.HandleSignal(syscall.SIGINT)
		if !handled {
			t.Fatal("ctrl+c should be consumed in idle mode")
		}
		if controller.IsActive() {
			t.Fatal("controller should exit IDM on idle ctrl+c")
		}
	})

	t.Run("streaming mode cancels current run and stays active", func(t *testing.T) {
		controller := newIDMController(idmControllerOptions{
			PTYWriter: &bytes.Buffer{},
			Output:    &bytes.Buffer{},
		})
		var canceled atomic.Bool
		streamCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeStreaming
		controller.streamCancel = func() {
			canceled.Store(true)
			cancel()
		}
		controller.currentRunID = "run-1"
		controller.sessionID = "idm-1-1"
		controller.mu.Unlock()

		handled := controller.HandleSignal(syscall.SIGINT)
		if !handled {
			t.Fatal("ctrl+c should be consumed in streaming mode")
		}
		if !canceled.Load() {
			t.Fatal("stream cancel should be triggered")
		}
		controller.mu.Lock()
		defer controller.mu.Unlock()
		if !controller.active {
			t.Fatal("streaming cancel should keep IDM active")
		}
		if controller.mode != idmModeIdle {
			t.Fatalf("mode = %v, want idle", controller.mode)
		}
		if streamCtx.Err() == nil {
			t.Fatal("stream context should be canceled")
		}
	})

	t.Run("native cmd mode delegates ctrl+c", func(t *testing.T) {
		controller := newIDMController(idmControllerOptions{
			PTYWriter: &bytes.Buffer{},
			Output:    &bytes.Buffer{},
		})
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeNativeCmd
		controller.mu.Unlock()

		handled := controller.HandleSignal(syscall.SIGINT)
		if handled {
			t.Fatal("ctrl+c should be forwarded in native cmd mode")
		}
	})
}

func TestIDMControllerOnShellEventRestoresIdle(t *testing.T) {
	controller := newIDMController(idmControllerOptions{
		PTYWriter: &bytes.Buffer{},
		Output:    &bytes.Buffer{},
	})
	controller.mu.Lock()
	controller.active = true
	controller.mode = idmModeNativeCmd
	controller.mu.Unlock()

	controller.OnShellEvent(ShellEvent{Type: ShellEventCommandDone, ExitCode: 0})

	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.mode != idmModeIdle {
		t.Fatalf("mode = %v, want idle", controller.mode)
	}
}

func TestIDMControllerHandleInputByteCtrlSignals(t *testing.T) {
	t.Run("ctrl+c byte exits in idle mode", func(t *testing.T) {
		controller := newIDMController(idmControllerOptions{
			PTYWriter: &bytes.Buffer{},
			Output:    &bytes.Buffer{},
			AutoState: &autoRuntimeState{},
			LogBuffer: NewUTF8RingBuffer(DefaultRingBufferCapacity),
		})
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeIdle
		controller.autoSnapshot = true
		controller.mu.Unlock()

		controller.HandleInputByte(0x03)
		if controller.IsActive() {
			t.Fatal("ctrl+c byte should exit IDM in idle mode")
		}
	})

	t.Run("ctrl+c byte cancels streaming mode", func(t *testing.T) {
		controller := newIDMController(idmControllerOptions{
			PTYWriter: &bytes.Buffer{},
			Output:    &bytes.Buffer{},
		})
		var canceled atomic.Bool
		streamCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeStreaming
		controller.streamCancel = func() {
			canceled.Store(true)
			cancel()
		}
		controller.currentRunID = "run-ctrlc"
		controller.sessionID = "idm-1-ctrlc"
		controller.mu.Unlock()

		controller.HandleInputByte(0x03)

		if !canceled.Load() {
			t.Fatal("ctrl+c byte should cancel streaming request")
		}
		controller.mu.Lock()
		if controller.mode != idmModeIdle {
			t.Fatalf("mode = %v, want idle", controller.mode)
		}
		controller.mu.Unlock()
		if streamCtx.Err() == nil {
			t.Fatal("stream context should be canceled")
		}
	})

	t.Run("ctrl+z byte follows ctrl+c semantics in idle mode", func(t *testing.T) {
		controller := newIDMController(idmControllerOptions{
			PTYWriter: &bytes.Buffer{},
			Output:    &bytes.Buffer{},
			AutoState: &autoRuntimeState{},
			LogBuffer: NewUTF8RingBuffer(DefaultRingBufferCapacity),
		})
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeIdle
		controller.autoSnapshot = true
		controller.mu.Unlock()

		controller.HandleInputByte(0x1A)
		if controller.IsActive() {
			t.Fatal("ctrl+z byte should not trap IDM in idle mode")
		}
	})
}

func TestIDMControllerFilterPTYOutput(t *testing.T) {
	t.Run("active idle suppresses all pty output", func(t *testing.T) {
		controller := newIDMController(idmControllerOptions{
			PTYWriter: &bytes.Buffer{},
			Output:    &bytes.Buffer{},
		})
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeIdle
		controller.mu.Unlock()

		filtered := controller.FilterPTYOutput([]byte("dust@host$ "))
		if len(filtered) != 0 {
			t.Fatalf("expected output to be suppressed, got %q", string(filtered))
		}
	})

	t.Run("native command mode keeps output and strips pending echo", func(t *testing.T) {
		controller := newIDMController(idmControllerOptions{
			PTYWriter: &bytes.Buffer{},
			Output:    &bytes.Buffer{},
		})
		controller.mu.Lock()
		controller.active = true
		controller.mode = idmModeNativeCmd
		controller.pendingEcho = []byte("ls\r\n")
		controller.mu.Unlock()

		filtered := controller.FilterPTYOutput([]byte("ls\r\nfile1\nfile2\n"))
		if string(filtered) != "file1\nfile2\n" {
			t.Fatalf("filtered output = %q, want %q", string(filtered), "file1\nfile2\n")
		}
	})
}

func TestIDMControllerHandleInputLineAskAIAsync(t *testing.T) {
	controller := newIDMController(idmControllerOptions{
		PTYWriter: &bytes.Buffer{},
		Output:    &bytes.Buffer{},
	})
	controller.mu.Lock()
	controller.active = true
	controller.mode = idmModeIdle
	controller.mu.Unlock()

	done := make(chan struct{})
	go func() {
		controller.handleInputLine("@ai 测试异步")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("handleInputLine should return quickly for @ai route")
	}
}

func TestReadPermissionRequestFromPayload(t *testing.T) {
	requestID, toolName := readPermissionRequestFromPayload(map[string]any{
		"request_id": "req-1",
		"tool_name":  "bash",
	})
	if requestID != "req-1" || toolName != "bash" {
		t.Fatalf("requestID/toolName = %q/%q, want req-1/bash", requestID, toolName)
	}

	requestID, toolName = readPermissionRequestFromPayload(map[string]any{
		"RequestID": "req-2",
		"ToolName":  "filesystem_write_file",
	})
	if requestID != "req-2" || toolName != "filesystem_write_file" {
		t.Fatalf("requestID/toolName = %q/%q, want req-2/filesystem_write_file", requestID, toolName)
	}
}

func TestRenderIDMMarkdown(t *testing.T) {
	rendered, err := renderIDMMarkdown("# Title\n\n- item")
	if err != nil {
		t.Fatalf("renderIDMMarkdown() error = %v", err)
	}
	if strings.TrimSpace(rendered) == "" {
		t.Fatal("rendered markdown should not be empty")
	}
}

func TestRejectPermissionInIDMWithoutRequestID(t *testing.T) {
	controller := newIDMController(idmControllerOptions{
		PTYWriter: &bytes.Buffer{},
		Output:    &bytes.Buffer{},
	})
	err := controller.rejectPermissionInIDM(map[string]any{"tool_name": "bash"})
	if err == nil || !strings.Contains(err.Error(), "request_id") {
		t.Fatalf("err = %v, want request_id error", err)
	}
}
