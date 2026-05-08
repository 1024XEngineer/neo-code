//go:build windows

package ptyproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
)

func TestDemuxGatewayNotificationsWindows(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	source := make(chan gatewayclient.Notification, 4)
	eventSink := make(chan gatewayclient.Notification, 2)
	controlSink := make(chan gatewayclient.Notification, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		demuxGatewayNotifications(ctx, source, eventSink, controlSink)
	}()

	source <- gatewayclient.Notification{Method: protocol.MethodGatewayEvent, Params: json.RawMessage(`{"k":"event"}`)}
	source <- gatewayclient.Notification{Method: protocol.MethodGatewayNotification, Params: json.RawMessage(`{"k":"control"}`)}
	close(source)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("demuxGatewayNotifications should exit after source closed")
	}

	select {
	case item := <-eventSink:
		if item.Method != protocol.MethodGatewayEvent {
			t.Fatalf("event sink method = %q, want %q", item.Method, protocol.MethodGatewayEvent)
		}
	default:
		t.Fatal("expected gateway.event to be forwarded to event sink")
	}
	select {
	case item := <-controlSink:
		if item.Method != protocol.MethodGatewayNotification {
			t.Fatalf("control sink method = %q, want %q", item.Method, protocol.MethodGatewayNotification)
		}
	default:
		t.Fatal("expected gateway.notification to be forwarded to control sink")
	}
}

func TestConsumeWindowsGatewayNotificationsIDMEnter(t *testing.T) {
	autoState := &autoRuntimeState{}

	output := &bytes.Buffer{}
	idm := newIDMController(idmControllerOptions{
		PTYWriter:      &bytes.Buffer{},
		Output:         output,
		Stderr:         io.Discard,
		AutoState:      autoState,
		LogBuffer:      NewUTF8RingBuffer(DefaultRingBufferCapacity),
		DefaultCap:     DefaultRingBufferCapacity,
		Workdir:        t.TempDir(),
		ShellSessionID: "shell-session-1",
	})
	t.Cleanup(func() { idm.Exit() })

	controlNotifications := make(chan gatewayclient.Notification, 2)
	eventNotifications := make(chan gatewayclient.Notification, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		consumeWindowsGatewayNotifications(
			ctx,
			controlNotifications,
			"shell-session-1",
			nil,
			idm,
			autoState,
			nil,
			output,
			io.Discard,
		)
	}()

	controlNotifications <- gatewayclient.Notification{
		Method: protocol.MethodGatewayNotification,
		Params: json.RawMessage(`{
			"session_id":"shell-session-1",
			"action":"idm_enter",
			"payload":{}
		}`),
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if idm.IsActive() {
			cancel()
			close(controlNotifications)
			close(eventNotifications)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("consumeWindowsGatewayNotifications should exit after cancellation")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected idm.Enter() to activate IDM mode")
}

func TestConsumeWindowsGatewayNotificationsAutoState(t *testing.T) {
	autoState := &autoRuntimeState{}
	autoState.OSCReady.Store(true)
	controlNotifications := make(chan gatewayclient.Notification, 4)
	eventNotifications := make(chan gatewayclient.Notification, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		publishedMu sync.Mutex
		published   []bool
	)
	publish := func(enabled bool) {
		publishedMu.Lock()
		defer publishedMu.Unlock()
		published = append(published, enabled)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		consumeWindowsGatewayNotifications(
			ctx,
			controlNotifications,
			"shell-session-2",
			nil,
			nil,
			autoState,
			publish,
			io.Discard,
			io.Discard,
		)
	}()

	controlNotifications <- gatewayclient.Notification{
		Method: protocol.MethodGatewayNotification,
		Params: json.RawMessage(`{
			"session_id":"shell-session-2",
			"action":"auto_on"
		}`),
	}
	controlNotifications <- gatewayclient.Notification{
		Method: protocol.MethodGatewayNotification,
		Params: json.RawMessage(`{
			"session_id":"shell-session-2",
			"action":"auto_off"
		}`),
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		publishedMu.Lock()
		count := len(published)
		publishedMu.Unlock()
		if count >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	publishedMu.Lock()
	recorded := append([]bool(nil), published...)
	publishedMu.Unlock()
	if len(recorded) != 2 || !recorded[0] || recorded[1] {
		t.Fatalf("published auto states = %#v, want [true false]", recorded)
	}
	if autoState.Enabled.Load() {
		t.Fatal("auto state should end in disabled after auto_off")
	}

	cancel()
	close(controlNotifications)
	close(eventNotifications)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumeWindowsGatewayNotifications should exit after cancellation")
	}
}

func TestConsumeWindowsGatewayNotificationsQueuesDiagnose(t *testing.T) {
	controlNotifications := make(chan gatewayclient.Notification, 1)
	diagnoseJobs := make(chan diagnoseJob, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		consumeWindowsGatewayNotifications(
			ctx,
			controlNotifications,
			"shell-session-3",
			diagnoseJobs,
			nil,
			&autoRuntimeState{},
			nil,
			io.Discard,
			io.Discard,
		)
	}()

	controlNotifications <- gatewayclient.Notification{
		Method: protocol.MethodGatewayNotification,
		Params: json.RawMessage(`{
			"session_id":"shell-session-3",
			"action":"diagnose"
		}`),
	}

	select {
	case job := <-diagnoseJobs:
		if job.IsAuto {
			t.Fatal("manual diagnose notification should queue non-auto job")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected diagnose job to be queued")
	}

	cancel()
	close(controlNotifications)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumeWindowsGatewayNotifications should exit after cancellation")
	}
}

func TestStreamWindowsShellOutputWithIDMTriggersAutoDiagnosis(t *testing.T) {
	reader := strings.NewReader("bad command\r\ncommand not found: bad command\r\n\x1b]133;D;1\a\x1b]133;A\a")
	output := &bytes.Buffer{}
	tracker := &commandTracker{}
	tracker.Observe([]byte("bad command\r"))
	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)
	autoTriggerCh := make(chan diagnoseTrigger, 1)

	streamWindowsShellOutputWithIDM(
		reader,
		output,
		NewUTF8RingBuffer(DefaultRingBufferCapacity/2),
		tracker,
		autoTriggerCh,
		&diagnosisTriggerStore{},
		autoState,
		nil,
	)

	if !autoState.OSCReady.Load() {
		t.Fatal("OSCReady should become true after prompt_ready event")
	}
	select {
	case trigger := <-autoTriggerCh:
		if trigger.ExitCode != 1 || trigger.CommandText != "bad command" {
			t.Fatalf("trigger = %#v, want exit=1 command=bad command", trigger)
		}
		if !strings.Contains(trigger.OutputText, "not found") {
			t.Fatalf("trigger output = %q, want command output", trigger.OutputText)
		}
	default:
		t.Fatal("expected auto diagnosis trigger")
	}
	if strings.Contains(output.String(), "]133;") {
		t.Fatalf("OSC133 control bytes should be stripped, got %q", output.String())
	}
}
