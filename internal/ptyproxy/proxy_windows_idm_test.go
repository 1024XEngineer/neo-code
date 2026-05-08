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

func TestWindowsDiagnosisDispatcherFlushesAfterPromptReady(t *testing.T) {
	tracker := &commandTracker{}
	screen := &bytes.Buffer{}
	ptyInput := &bytes.Buffer{}
	dispatcher := newWindowsDiagnosisDispatcher(
		"powershell.exe",
		func(text string) error {
			_, _ = screen.WriteString(text)
			return nil
		},
		ptyInput,
		tracker,
		io.Discard,
	)

	dispatcher.Enqueue([]string{"[NeoCode Diagnosis]", "root cause: typo"})
	if screen.Len() != 0 {
		t.Fatalf("diagnosis should wait for prompt_ready, got %q", screen.String())
	}
	if ptyInput.Len() != 0 {
		t.Fatalf("pty input should not advance prompt before flush, got %q", ptyInput.String())
	}

	dispatcher.MarkPromptReady(true)
	if got := screen.String(); !strings.Contains(got, "\n[NeoCode Diagnosis]\nroot cause: typo\n") {
		t.Fatalf("screen payload = %q, want diagnosis block", got)
	}
	if got := ptyInput.String(); got != "\r\n" {
		t.Fatalf("pty input = %q, want single empty command to refresh prompt", got)
	}
}

func TestWindowsDiagnosisDispatcherDefersWhenTyping(t *testing.T) {
	tracker := &commandTracker{}
	screen := &bytes.Buffer{}
	ptyInput := &bytes.Buffer{}
	dispatcher := newWindowsDiagnosisDispatcher(
		"pwsh.exe",
		func(text string) error {
			_, _ = screen.WriteString(text)
			return nil
		},
		ptyInput,
		tracker,
		io.Discard,
	)

	dispatcher.MarkPromptReady(true)
	tracker.Observe([]byte("dir"))
	dispatcher.Enqueue([]string{"[NeoCode Diagnosis]", "waiting"})
	if screen.Len() != 0 {
		t.Fatalf("diagnosis should not inject while user is typing, got %q", screen.String())
	}

	tracker.Observe([]byte("\r"))
	dispatcher.MarkPromptReady(true)
	if got := screen.String(); !strings.Contains(got, "\n[NeoCode Diagnosis]\nwaiting\n") {
		t.Fatalf("expected diagnosis to flush after next prompt_ready, got %q", got)
	}
	if got := ptyInput.String(); got != "\r\n" {
		t.Fatalf("pty input = %q, want prompt refresh after flush", got)
	}
}

func TestBuildWindowsDiagnosisScreenBlock(t *testing.T) {
	block := buildWindowsDiagnosisScreenBlock([]string{"[NeoCode Diagnosis]", "root cause: A&B|C<1>(ok)% "})
	if block != "\n[NeoCode Diagnosis]\nroot cause: A&B|C<1>(ok)%\n" {
		t.Fatalf("block = %q", block)
	}
}

func TestBuildWindowsDiagnosisPrintCommandUsesBase64(t *testing.T) {
	command := buildWindowsDiagnosisPrintCommand("powershell.exe", "\n[NeoCode Diagnosis]\nroot cause: A&B|C\n")
	if !strings.Contains(command, "FromBase64String(") {
		t.Fatalf("command = %q, want base64 decode", command)
	}
	if !strings.Contains(command, "-replace '\\n'") {
		t.Fatalf("command = %q, want normalized newline replacement", command)
	}
	if strings.Contains(command, "root cause: A&B|C") {
		t.Fatalf("command should not embed raw payload, got %q", command)
	}
}
