//go:build windows

package ptyproxy

import (
	"bytes"
	"testing"
)

func TestIDMControllerSendNativeCommandUsesCRLFOnWindows(t *testing.T) {
	ptyBuffer := &bytes.Buffer{}
	controller := newIDMController(idmControllerOptions{
		PTYWriter: ptyBuffer,
		Output:    &bytes.Buffer{},
	})

	controller.mu.Lock()
	controller.active = true
	controller.mode = idmModeIdle
	controller.mu.Unlock()

	if err := controller.sendNativeCommand("dir"); err != nil {
		t.Fatalf("sendNativeCommand() error = %v", err)
	}

	if got := ptyBuffer.String(); got != "dir\r\n" {
		t.Fatalf("pty payload = %q, want %q", got, "dir\\r\\n")
	}

	controller.mu.Lock()
	defer controller.mu.Unlock()
	if got := string(controller.pendingEcho); got != "dir\r\n" {
		t.Fatalf("pendingEcho = %q, want %q", got, "dir\\r\\n")
	}
}
