//go:build !windows

package ptyproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

func TestIDMControllerEnterExitRingBufferLifecycle(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	socketDir := t.TempDir()
	gatewaySocket := filepath.Join(socketDir, "gateway.sock")
	tokenFile := filepath.Join(socketDir, "auth.json")
	writeGatewayAuthTokenFile(t, tokenFile, "test-token")

	cleanupServer, serverDone := startGatewayRPCMockServer(
		t,
		gatewaySocket,
		func(decoder *json.Decoder, encoder *json.Encoder) error {
			authReq, err := readRPCRequest(decoder)
			if err != nil {
				return err
			}
			if authReq.Method != protocol.MethodGatewayAuthenticate {
				return writeRPCResult(encoder, authReq.ID, gateway.MessageFrame{
					Type: gateway.FrameTypeError,
					Error: &gateway.FrameError{
						Code:    "unexpected_method",
						Message: authReq.Method,
					},
				})
			}
			if err := writeRPCResult(encoder, authReq.ID, gateway.MessageFrame{
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionAuthenticate,
			}); err != nil {
				return err
			}

			createReq, err := readRPCRequest(decoder)
			if err != nil {
				return err
			}
			if createReq.Method != protocol.MethodGatewayCreateSession {
				return writeRPCResult(encoder, createReq.ID, gateway.MessageFrame{
					Type: gateway.FrameTypeError,
					Error: &gateway.FrameError{
						Code:    "unexpected_method",
						Message: createReq.Method,
					},
				})
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
				return writeRPCResult(encoder, activateReq.ID, gateway.MessageFrame{
					Type: gateway.FrameTypeError,
					Error: &gateway.FrameError{
						Code:    "unexpected_method",
						Message: activateReq.Method,
					},
				})
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
				return writeRPCResult(encoder, deleteReq.ID, gateway.MessageFrame{
					Type: gateway.FrameTypeError,
					Error: &gateway.FrameError{
						Code:    "unexpected_method",
						Message: deleteReq.Method,
					},
				})
			}
			return writeRPCResult(encoder, deleteReq.ID, gateway.MessageFrame{
				Type:   gateway.FrameTypeAck,
				Action: gateway.FrameActionDeleteSession,
			})
		},
	)
	defer cleanupServer()

	rpcClient, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress: gatewaySocket,
		TokenFile:     tokenFile,
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

	logBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity)
	_, _ = logBuffer.Write([]byte("initial text"))

	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)
	autoState.OSCReady.Store(true)

	output := &bytes.Buffer{}
	controller := newIDMController(idmControllerOptions{
		PTYWriter:  &bytes.Buffer{},
		Output:     output,
		Stderr:     output,
		RPCClient:  rpcClient,
		AutoState:  autoState,
		LogBuffer:  logBuffer,
		DefaultCap: DefaultRingBufferCapacity,
		Workdir:    "/tmp",
	})

	if err := controller.Enter(); err != nil {
		t.Fatalf("Enter() error = %v", err)
	}
	if got := logBuffer.Capacity(); got != idmExpandedRingBufferCapacity {
		t.Fatalf("capacity after Enter = %d, want %d", got, idmExpandedRingBufferCapacity)
	}
	if autoState.Enabled.Load() {
		t.Fatal("auto mode should be disabled in IDM")
	}

	controller.Exit()
	if got := logBuffer.Capacity(); got != DefaultRingBufferCapacity {
		t.Fatalf("capacity after Exit = %d, want %d", got, DefaultRingBufferCapacity)
	}
	if snapshot := logBuffer.SnapshotString(); snapshot != "" {
		t.Fatalf("snapshot after Exit = %q, want empty", snapshot)
	}
	if !autoState.Enabled.Load() {
		t.Fatal("auto mode should be restored after IDM exit")
	}
	if serverErr := <-serverDone; serverErr != nil {
		t.Fatalf("mock gateway server error = %v", serverErr)
	}
}
