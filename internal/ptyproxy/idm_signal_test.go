//go:build !windows

package ptyproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestSendIDMEnterSignal(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "idm.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()

		reader := bufio.NewReader(connection)
		line, _ := reader.ReadBytes('\n')
		var request diagIPCRequest
		_ = json.Unmarshal(line, &request)
		if request.Cmd != diagCommandIDMEnter {
			t.Errorf("request cmd = %q, want %q", request.Cmd, diagCommandIDMEnter)
		}
		writeDiagIPCResponse(connection, diagIPCResponse{OK: true, Message: "ok"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := SendIDMEnterSignal(ctx, socketPath); err != nil {
		t.Fatalf("SendIDMEnterSignal() error = %v", err)
	}
	<-done
}

func TestSendIDMEnterSignalRejectsEmptyPath(t *testing.T) {
	err := SendIDMEnterSignal(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected empty path error")
	}
}
