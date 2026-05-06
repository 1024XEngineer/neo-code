package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
)

func TestGatewayRPCClientAuthenticateCallAndNotification(t *testing.T) {
	tokenFile, token := createTestAuthTokenFile(t)

	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     tokenFile,
		Dial: func(_ string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)

				request := readRPCRequestOrFail(decoder)
				if request.Method != protocol.MethodGatewayAuthenticate {
					panicf("authenticate method = %q", request.Method)
				}
				var params protocol.AuthenticateParams
				if err := json.Unmarshal(request.Params, &params); err != nil {
					panicf("decode authenticate params: %v", err)
				}
				if params.Token != token {
					panicf("authenticate token = %q, want %q", params.Token, token)
				}
				writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
					Type:   gateway.FrameTypeAck,
					Action: gateway.FrameActionAuthenticate,
				})

				request = readRPCRequestOrFail(decoder)
				if request.Method != protocol.MethodGatewayPing {
					panicf("call method = %q, want %q", request.Method, protocol.MethodGatewayPing)
				}
				writeRPCNotificationOrFail(encoder, protocol.MethodGatewayEvent, gateway.MessageFrame{
					Type:      gateway.FrameTypeEvent,
					Action:    gateway.FrameActionRun,
					SessionID: "session-1",
					RunID:     "run-1",
					Payload: map[string]any{
						"runtime_event_type": string("agent_chunk"),
						"payload":            "hello",
					},
				})
				writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
					Type:      gateway.FrameTypeAck,
					Action:    gateway.FrameActionPing,
					SessionID: "session-1",
					RunID:     "run-1",
				})
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	var frame gateway.MessageFrame
	if err := client.Call(context.Background(), protocol.MethodGatewayPing, map[string]any{}, &frame); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if frame.Type != gateway.FrameTypeAck || frame.Action != gateway.FrameActionPing {
		t.Fatalf("unexpected rpc result frame: %#v", frame)
	}

	select {
	case notification := <-client.Notifications():
		if notification.Method != protocol.MethodGatewayEvent {
			t.Fatalf("notification method = %q, want %q", notification.Method, protocol.MethodGatewayEvent)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for notification")
	}
}

func TestGatewayRPCClientRetriesAfterTransportError(t *testing.T) {
	tokenFile, _ := createTestAuthTokenFile(t)

	var dialCount int32
	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     tokenFile,
		Dial: func(_ string) (net.Conn, error) {
			attempt := atomic.AddInt32(&dialCount, 1)
			if attempt == 1 {
				return nil, errors.New("dial failed once")
			}

			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)
				request := readRPCRequestOrFail(decoder)
				writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
					Type:   gateway.FrameTypeAck,
					Action: gateway.FrameActionPing,
				})
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var frame gateway.MessageFrame
	err = client.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayPing,
		map[string]any{},
		&frame,
		GatewayRPCCallOptions{
			Timeout: 2 * time.Second,
			Retries: 1,
		},
	)
	if err != nil {
		t.Fatalf("CallWithOptions() error = %v", err)
	}
	if atomic.LoadInt32(&dialCount) != 2 {
		t.Fatalf("dial count = %d, want %d", atomic.LoadInt32(&dialCount), 2)
	}
	if frame.Action != gateway.FrameActionPing {
		t.Fatalf("unexpected frame: %#v", frame)
	}
}

func TestGatewayRPCClientUsesDefaultRetryCountWhenOptionIsZero(t *testing.T) {
	tokenFile, _ := createTestAuthTokenFile(t)

	var dialCount int32
	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     tokenFile,
		RetryCount:    0,
		Dial: func(_ string) (net.Conn, error) {
			attempt := atomic.AddInt32(&dialCount, 1)
			if attempt == 1 {
				return nil, errors.New("dial failed once")
			}

			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)
				request := readRPCRequestOrFail(decoder)
				writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
					Type:   gateway.FrameTypeAck,
					Action: gateway.FrameActionPing,
				})
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if client.retryCount != defaultGatewayRPCRetryCount {
		t.Fatalf("retryCount = %d, want %d", client.retryCount, defaultGatewayRPCRetryCount)
	}

	var frame gateway.MessageFrame
	if err := client.Call(context.Background(), protocol.MethodGatewayPing, map[string]any{}, &frame); err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if atomic.LoadInt32(&dialCount) != 2 {
		t.Fatalf("dial count = %d, want %d", atomic.LoadInt32(&dialCount), 2)
	}
	if frame.Action != gateway.FrameActionPing {
		t.Fatalf("unexpected frame: %#v", frame)
	}
}

func TestGatewayRPCClientHeartbeatSendsPingAndStopsAfterClose(t *testing.T) {
	tokenFile, _ := createTestAuthTokenFile(t)

	var pingCount int32
	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress:     "test://gateway",
		TokenFile:         tokenFile,
		RequestTimeout:    200 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		HeartbeatTimeout:  120 * time.Millisecond,
		Dial: func(_ string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)
				for {
					var request protocol.JSONRPCRequest
					if err := decoder.Decode(&request); err != nil {
						if errors.Is(err, io.EOF) {
							return
						}
						return
					}
					if request.Method != protocol.MethodGatewayPing {
						panicf("unexpected method = %q", request.Method)
					}
					atomic.AddInt32(&pingCount, 1)
					writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
						Type:   gateway.FrameTypeAck,
						Action: gateway.FrameActionPing,
					})
				}
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}

	var frame gateway.MessageFrame
	if err := client.Call(context.Background(), protocol.MethodGatewayPing, map[string]any{}, &frame); err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	waitForCondition(
		t,
		500*time.Millisecond,
		func() bool { return atomic.LoadInt32(&pingCount) >= 2 },
		"ping count should include manual ping and at least one heartbeat ping",
	)

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	afterClose := atomic.LoadInt32(&pingCount)
	assertConditionStaysTrue(
		t,
		200*time.Millisecond,
		func() bool { return atomic.LoadInt32(&pingCount) == afterClose },
		"ping count changed after close",
	)
}

func TestGatewayRPCClientHeartbeatDoesNotRedialAfterConnectionDrops(t *testing.T) {
	tokenFile, _ := createTestAuthTokenFile(t)

	var dialCount int32
	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress:     "test://gateway",
		TokenFile:         tokenFile,
		RequestTimeout:    200 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		HeartbeatTimeout:  120 * time.Millisecond,
		Dial: func(_ string) (net.Conn, error) {
			atomic.AddInt32(&dialCount, 1)
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)
				var request protocol.JSONRPCRequest
				if err := decoder.Decode(&request); err != nil {
					if errors.Is(err, io.EOF) {
						return
					}
					return
				}
				if request.Method != protocol.MethodGatewayPing {
					panicf("unexpected method = %q", request.Method)
				}
				writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
					Type:   gateway.FrameTypeAck,
					Action: gateway.FrameActionPing,
				})
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var frame gateway.MessageFrame
	if err := client.Call(context.Background(), protocol.MethodGatewayPing, map[string]any{}, &frame); err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	assertConditionStaysTrue(
		t,
		300*time.Millisecond,
		func() bool { return atomic.LoadInt32(&dialCount) == 1 },
		"heartbeat should not trigger re-dial after connection drop",
	)
}

func TestGatewayRPCClientHeartbeatDoesNotInterruptInFlightCall(t *testing.T) {
	tokenFile, _ := createTestAuthTokenFile(t)

	var pingCount int32
	requestStarted := make(chan struct{})
	releaseSlowRequest := make(chan struct{})
	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress:     "test://gateway",
		TokenFile:         tokenFile,
		RequestTimeout:    600 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		HeartbeatTimeout:  40 * time.Millisecond,
		Dial: func(_ string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)
				for {
					var request protocol.JSONRPCRequest
					if err := decoder.Decode(&request); err != nil {
						if errors.Is(err, io.EOF) {
							return
						}
						return
					}
					switch request.Method {
					case "test.slow":
						select {
						case <-requestStarted:
						default:
							close(requestStarted)
						}
						<-releaseSlowRequest
						writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionRun,
						})
					case protocol.MethodGatewayPing:
						atomic.AddInt32(&pingCount, 1)
						writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionPing,
						})
					default:
						panicf("unexpected method = %q", request.Method)
					}
				}
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var frame gateway.MessageFrame
	callDone := make(chan error, 1)
	go func() {
		callDone <- client.CallWithOptions(
			ctx,
			"test.slow",
			map[string]any{},
			&frame,
			GatewayRPCCallOptions{Timeout: 500 * time.Millisecond, Retries: 0},
		)
	}()

	select {
	case <-requestStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for slow request to start")
	}

	assertConditionStaysTrue(
		t,
		120*time.Millisecond,
		func() bool { return atomic.LoadInt32(&pingCount) == 0 },
		"heartbeat ping should be skipped while in-flight request is pending",
	)

	close(releaseSlowRequest)
	if err := <-callDone; err != nil {
		t.Fatalf("CallWithOptions(test.slow) error = %v", err)
	}
	if frame.Type != gateway.FrameTypeAck {
		t.Fatalf("frame type = %q, want %q", frame.Type, gateway.FrameTypeAck)
	}
}

func TestGatewayRPCClientCallWithEmptyMethodReturnsError(t *testing.T) {
	tokenFile, _ := createTestAuthTokenFile(t)
	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     tokenFile,
		Dial: func(_ string) (net.Conn, error) {
			return nil, errors.New("should not dial")
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	err = client.CallWithOptions(context.Background(), "   ", nil, nil, GatewayRPCCallOptions{})
	if err == nil || !strings.Contains(err.Error(), "method is empty") {
		t.Fatalf("expected method empty error, got %v", err)
	}
}

func TestGatewayRPCClientReadLoopSustainsBackpressureWhenNotificationsAreConsumed(t *testing.T) {
	tokenFile, _ := createTestAuthTokenFile(t)

	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     tokenFile,
		Dial: func(_ string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)

				request := readRPCRequestOrFail(decoder)
				for idx := 0; idx < defaultGatewayNotificationQueue+defaultGatewayNotificationBuffer+128; idx++ {
					writeRPCNotificationOrFail(encoder, protocol.MethodGatewayEvent, gateway.MessageFrame{
						Type:      gateway.FrameTypeEvent,
						Action:    gateway.FrameActionRun,
						SessionID: "session-1",
						RunID:     "run-1",
						Payload: map[string]any{
							"index": idx,
						},
					})
				}
				writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
					Type:   gateway.FrameTypeAck,
					Action: gateway.FrameActionPing,
				})
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	go func() {
		for range client.Notifications() {
		}
	}()

	callErr := client.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayPing,
		map[string]any{},
		&gateway.MessageFrame{},
		GatewayRPCCallOptions{Timeout: 2 * time.Second},
	)
	if callErr != nil {
		t.Fatalf("CallWithOptions() should succeed when notifications are back-pressured, got %v", callErr)
	}
}

func TestGatewayRPCClientCallReauthOnUnauthorized(t *testing.T) {
	tokenFile, _ := createTestAuthTokenFile(t)

	var pingAttempt int32
	var authAttempt int32
	client, err := NewGatewayRPCClient(GatewayRPCClientOptions{
		ListenAddress: "test://gateway",
		TokenFile:     tokenFile,
		Dial: func(_ string) (net.Conn, error) {
			clientConn, serverConn := net.Pipe()
			go func() {
				defer serverConn.Close()
				decoder := json.NewDecoder(serverConn)
				encoder := json.NewEncoder(serverConn)
				for {
					var request protocol.JSONRPCRequest
					if err := decoder.Decode(&request); err != nil {
						if errors.Is(err, io.EOF) {
							return
						}
						panicf("decode rpc request: %v", err)
					}
					switch request.Method {
					case protocol.MethodGatewayPing:
						current := atomic.AddInt32(&pingAttempt, 1)
						if current == 1 {
							rpcErr := protocol.NewJSONRPCError(
								protocol.JSONRPCCodeInvalidParams,
								"unauthorized",
								protocol.GatewayCodeUnauthorized,
							)
							if err := encoder.Encode(protocol.NewJSONRPCErrorResponse(request.ID, rpcErr)); err != nil {
								panicf("encode unauthorized response: %v", err)
							}
							continue
						}
						writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionPing,
						})
					case protocol.MethodGatewayAuthenticate:
						atomic.AddInt32(&authAttempt, 1)
						writeRPCResultOrFail(encoder, request.ID, gateway.MessageFrame{
							Type:   gateway.FrameTypeAck,
							Action: gateway.FrameActionAuthenticate,
						})
					default:
						panicf("unexpected method = %q", request.Method)
					}
				}
			}()
			return clientConn, nil
		},
	})
	if err != nil {
		t.Fatalf("NewGatewayRPCClient() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var frame gateway.MessageFrame
	err = client.CallWithOptions(
		context.Background(),
		protocol.MethodGatewayPing,
		map[string]any{},
		&frame,
		GatewayRPCCallOptions{
			Timeout: 2 * time.Second,
			Retries: 0,
		},
	)
	if err != nil {
		t.Fatalf("CallWithOptions() error = %v", err)
	}
	if frame.Action != gateway.FrameActionPing {
		t.Fatalf("frame action = %v, want %v", frame.Action, gateway.FrameActionPing)
	}
	if atomic.LoadInt32(&authAttempt) == 0 {
		t.Fatal("expected re-authenticate attempt after unauthorized")
	}
	if atomic.LoadInt32(&pingAttempt) < 2 {
		t.Fatalf("ping attempt = %d, want >= 2", atomic.LoadInt32(&pingAttempt))
	}
}

func createTestAuthTokenFile(t *testing.T) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	token := "test-token"
	writeTestAuthTokenFile(t, path, token)
	return path, token
}

func writeTestAuthTokenFile(t *testing.T, path string, token string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create auth dir: %v", err)
	}
	payload := map[string]any{
		"version":    1,
		"token":      token,
		"created_at": time.Now().UTC(),
		"updated_at": time.Now().UTC(),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal auth token: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write auth token: %v", err)
	}
}

func readRPCRequestOrFail(decoder *json.Decoder) protocol.JSONRPCRequest {
	var request protocol.JSONRPCRequest
	if err := decoder.Decode(&request); err != nil {
		panicf("decode rpc request: %v", err)
	}
	return request
}

func writeRPCResultOrFail(encoder *json.Encoder, id json.RawMessage, result any) {
	response, rpcErr := protocol.NewJSONRPCResultResponse(id, result)
	if rpcErr != nil {
		panicf("build jsonrpc result: %+v", rpcErr)
	}
	if err := encoder.Encode(response); err != nil {
		panicf("encode jsonrpc result: %v", err)
	}
}

func writeRPCNotificationOrFail(encoder *json.Encoder, method string, params any) {
	notification := protocol.NewJSONRPCNotification(method, params)
	if err := encoder.Encode(notification); err != nil {
		panicf("encode notification: %v", err)
	}
}

func panicf(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !condition() {
		t.Fatalf("condition not met within %s: %s", timeout, message)
	}
}

func assertConditionStaysTrue(t *testing.T, duration time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if !condition() {
			t.Fatalf("%s", message)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
