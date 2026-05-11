package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/ptyproxy"
)

func TestShellCommandInitAcceptsPositionalShellArgument(t *testing.T) {
	originalInitRunner := runShellInitCommand
	t.Cleanup(func() { runShellInitCommand = originalInitRunner })

	var captured shellCommandOptions
	runShellInitCommand = func(_ context.Context, options shellCommandOptions, _ io.Writer) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"shell", "--init", "/bin/zsh"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.Shell != "/bin/zsh" {
		t.Fatalf("shell = %q, want /bin/zsh", captured.Shell)
	}
}

func TestShellCommandUsesFlags(t *testing.T) {
	originalRunner := runShellCommand
	t.Cleanup(func() { runShellCommand = originalRunner })

	var captured shellCommandOptions
	runShellCommand = func(_ context.Context, options shellCommandOptions, _ io.Reader, _ io.Writer, _ io.Writer) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{
		"--workdir", " /repo ",
		"shell",
		"--shell", " /bin/zsh ",
		"--gateway-listen", " /tmp/gateway.sock ",
		"--gateway-token-file", " /tmp/auth.json ",
	})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}

	if captured.Workdir != "/repo" {
		t.Fatalf("workdir = %q, want /repo", captured.Workdir)
	}
	if captured.Shell != "/bin/zsh" {
		t.Fatalf("shell = %q, want /bin/zsh", captured.Shell)
	}
	if captured.GatewayListenAddress != "/tmp/gateway.sock" {
		t.Fatalf("gateway-listen = %q, want /tmp/gateway.sock", captured.GatewayListenAddress)
	}
	if captured.GatewayTokenFile != "/tmp/auth.json" {
		t.Fatalf("gateway-token-file = %q, want /tmp/auth.json", captured.GatewayTokenFile)
	}
}

func TestShellCommandInitPrintsScript(t *testing.T) {
	originalInitRunner := runShellInitCommand
	t.Cleanup(func() { runShellInitCommand = originalInitRunner })

	runShellInitCommand = func(_ context.Context, options shellCommandOptions, stdout io.Writer) error {
		if options.Shell != "/bin/bash" {
			t.Fatalf("shell = %q, want /bin/bash", options.Shell)
		}
		_, _ = io.WriteString(stdout, "script-body")
		return nil
	}

	command := NewRootCommand()
	stdout := &bytes.Buffer{}
	command.SetOut(stdout)
	command.SetArgs([]string{"shell", "--init", "--shell", "/bin/bash"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "script-body") {
		t.Fatalf("stdout = %q, want contains script-body", stdout.String())
	}
}

func TestDiagCommandParsesSessionAndErrorLog(t *testing.T) {
	originalRunner := runDiagCommand
	t.Cleanup(func() { runDiagCommand = originalRunner })

	var captured diagCommandOptions
	runDiagCommand = func(_ context.Context, options diagCommandOptions) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"diag", "--session", " shell-session-1 ", "--error-log", " boom "})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.SessionID != "shell-session-1" {
		t.Fatalf("SessionID = %q, want shell-session-1", captured.SessionID)
	}
	if captured.ErrorLog != "boom" {
		t.Fatalf("ErrorLog = %q, want boom", captured.ErrorLog)
	}
}

func TestDiagCommandReadsErrorLogFromStdin(t *testing.T) {
	originalRunner := runDiagCommand
	t.Cleanup(func() { runDiagCommand = originalRunner })

	var captured diagCommandOptions
	runDiagCommand = func(_ context.Context, options diagCommandOptions) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetIn(strings.NewReader("stdin boom\n"))
	command.SetArgs([]string{"diag", "--session", "shell-session-2"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if captured.ErrorLog != "stdin boom" {
		t.Fatalf("ErrorLog = %q, want stdin boom", captured.ErrorLog)
	}
}

func TestDiagAutoCommandParsesModes(t *testing.T) {
	originalRunner := runDiagAutoCommand
	t.Cleanup(func() { runDiagAutoCommand = originalRunner })

	var captured diagAutoCommandOptions
	runDiagAutoCommand = func(_ context.Context, options diagAutoCommandOptions, _ io.Writer) error {
		captured = options
		return nil
	}

	command := NewRootCommand()
	command.SetArgs([]string{"diag", "auto", "status", "--session", "shell-session-3"})
	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext() error = %v", err)
	}
	if !captured.QueryOnly {
		t.Fatal("QueryOnly = false, want true")
	}
	if captured.SessionID != "shell-session-3" {
		t.Fatalf("SessionID = %q, want shell-session-3", captured.SessionID)
	}
}

func TestDiagCommandBuildersRemoveSocketFlags(t *testing.T) {
	if newShellCommand().Flags().Lookup("socket") != nil {
		t.Fatal("shell command should not expose --socket")
	}
	if newDiagDiagnoseCommand().Flags().Lookup("socket") != nil {
		t.Fatal("diag diagnose command should not expose --socket")
	}
	if newDiagAutoCommand().Flags().Lookup("socket") != nil {
		t.Fatal("diag auto command should not expose --socket")
	}
}

func TestDefaultShellCommandRunner(t *testing.T) {
	originalRun := runManualShellProxy
	t.Cleanup(func() { runManualShellProxy = originalRun })

	var captured ptyproxy.ManualShellOptions
	runManualShellProxy = func(_ context.Context, options ptyproxy.ManualShellOptions) error {
		captured = options
		return nil
	}

	stdin := strings.NewReader("input")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := defaultShellCommandRunner(context.Background(), shellCommandOptions{
		Workdir:              " /repo ",
		Shell:                " /bin/bash ",
		GatewayListenAddress: " /tmp/gateway.sock ",
		GatewayTokenFile:     " /tmp/token.json ",
	}, stdin, stdout, stderr)
	if err != nil {
		t.Fatalf("defaultShellCommandRunner() error = %v", err)
	}
	if captured.Workdir != "/repo" || captured.Shell != "/bin/bash" {
		t.Fatalf("captured options = %+v", captured)
	}
	if captured.GatewayListenAddress != "/tmp/gateway.sock" {
		t.Fatalf("GatewayListenAddress = %q", captured.GatewayListenAddress)
	}
	if captured.GatewayTokenFile != "/tmp/token.json" {
		t.Fatalf("GatewayTokenFile = %q", captured.GatewayTokenFile)
	}
}

func TestDefaultDiagRunnersUseSessionEnvFallback(t *testing.T) {
	originalSendDiagnose := sendDiagnoseSignalFn
	originalSendIDM := sendIDMEnterSignalFn
	originalQuery := queryAutoModeFn
	originalReadEnv := readDiagEnvValue
	t.Cleanup(func() { sendDiagnoseSignalFn = originalSendDiagnose })
	t.Cleanup(func() { sendIDMEnterSignalFn = originalSendIDM })
	t.Cleanup(func() { queryAutoModeFn = originalQuery })
	t.Cleanup(func() { readDiagEnvValue = originalReadEnv })

	readDiagEnvValue = func(key string) string {
		if key == ptyproxy.ShellSessionEnv {
			return "shell-session-env"
		}
		return ""
	}

	var diagnoseSessionID string
	sendDiagnoseSignalFn = func(_ context.Context, sessionID string) error {
		diagnoseSessionID = sessionID
		return nil
	}
	if err := defaultDiagCommandRunner(context.Background(), diagCommandOptions{}); err != nil {
		t.Fatalf("defaultDiagCommandRunner() error = %v", err)
	}
	if diagnoseSessionID != "shell-session-env" {
		t.Fatalf("diagnose session = %q, want shell-session-env", diagnoseSessionID)
	}

	var idmSessionID string
	sendIDMEnterSignalFn = func(_ context.Context, sessionID string) error {
		idmSessionID = sessionID
		return nil
	}
	if err := defaultDiagInteractiveCommandRunner(context.Background(), diagCommandOptions{}); err != nil {
		t.Fatalf("defaultDiagInteractiveCommandRunner() error = %v", err)
	}
	if idmSessionID != "shell-session-env" {
		t.Fatalf("idm session = %q, want shell-session-env", idmSessionID)
	}

	queryAutoModeFn = func(_ context.Context, sessionID string) (bool, error) {
		if sessionID != "shell-session-env" {
			t.Fatalf("query session = %q", sessionID)
		}
		return false, nil
	}
	stdout := &bytes.Buffer{}
	if err := defaultDiagAutoCommandRunner(context.Background(), diagAutoCommandOptions{QueryOnly: true}, stdout); err != nil {
		t.Fatalf("defaultDiagAutoCommandRunner() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "disabled") {
		t.Fatalf("stdout = %q, want disabled", stdout.String())
	}
}

func TestDefaultDiagAutoCommandRunnerErrors(t *testing.T) {
	originalQuery := queryAutoModeFn
	originalSend := sendAutoModeSignalFn
	t.Cleanup(func() { queryAutoModeFn = originalQuery })
	t.Cleanup(func() { sendAutoModeSignalFn = originalSend })

	queryAutoModeFn = func(context.Context, string) (bool, error) { return false, errors.New("query failed") }
	if err := defaultDiagAutoCommandRunner(context.Background(), diagAutoCommandOptions{
		QueryOnly: true,
		SessionID: "shell-session-4",
	}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "query failed") {
		t.Fatalf("defaultDiagAutoCommandRunner(query error) err = %v", err)
	}

	sendAutoModeSignalFn = func(context.Context, string, bool) error { return errors.New("send failed") }
	if err := defaultDiagAutoCommandRunner(context.Background(), diagAutoCommandOptions{
		Enabled:   false,
		SessionID: "shell-session-4",
	}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "send failed") {
		t.Fatalf("defaultDiagAutoCommandRunner(send error) err = %v", err)
	}
}

func TestExtractAskTextPhase5Fields(t *testing.T) {
	t.Run("ask chunk prefers delta", func(t *testing.T) {
		payload := map[string]any{
			"message": "legacy message",
			"delta":   "phase5 delta",
		}
		if got := extractAskText(payload); got != "phase5 delta" {
			t.Fatalf("extractAskText() = %q, want %q", got, "phase5 delta")
		}
	})

	t.Run("ask done supports full_response", func(t *testing.T) {
		payload := map[string]any{
			"full_response": "phase5 full response",
		}
		if got := extractAskText(payload); got != "phase5 full response" {
			t.Fatalf("extractAskText() = %q, want %q", got, "phase5 full response")
		}
	})

	t.Run("nested payload fallback", func(t *testing.T) {
		payload := map[string]any{
			"payload": map[string]any{
				"delta": "nested delta",
			},
		}
		if got := extractAskText(payload); got != "nested delta" {
			t.Fatalf("extractAskText() = %q, want %q", got, "nested delta")
		}
	})
}

func TestTriggerDiagActionBindsCLIRoleBeforeTriggerAction(t *testing.T) {
	originalFactory := newDiagGatewayClient
	t.Cleanup(func() { newDiagGatewayClient = originalFactory })

	client := &stubDiagGatewayClientForTriggerAction{}
	newDiagGatewayClient = func() (diagGatewayClient, error) {
		return client, nil
	}

	frame, err := triggerDiagAction(context.Background(), "shell-session-1", "auto_off")
	if err != nil {
		t.Fatalf("triggerDiagAction() error = %v", err)
	}
	if frame.Type != gateway.FrameTypeAck {
		t.Fatalf("frame type = %q, want %q", frame.Type, gateway.FrameTypeAck)
	}
	if len(client.calls) != 2 {
		t.Fatalf("calls len = %d, want 2", len(client.calls))
	}
	if client.calls[0].method != protocol.MethodGatewayBindStream {
		t.Fatalf("first call method = %q, want %q", client.calls[0].method, protocol.MethodGatewayBindStream)
	}
	if !strings.Contains(client.calls[0].payload, `"role":"cli"`) {
		t.Fatalf("bind payload = %s, want role=cli", client.calls[0].payload)
	}
	if client.calls[1].method != protocol.MethodGatewayExperimentalTriggerAction {
		t.Fatalf("second call method = %q, want %q", client.calls[1].method, protocol.MethodGatewayExperimentalTriggerAction)
	}
}

type stubDiagGatewayClientForTriggerAction struct {
	calls []stubDiagGatewayCall
}

type stubDiagGatewayCall struct {
	method  string
	payload string
}

func (s *stubDiagGatewayClientForTriggerAction) Authenticate(context.Context) error { return nil }
func (s *stubDiagGatewayClientForTriggerAction) Close() error                       { return nil }
func (s *stubDiagGatewayClientForTriggerAction) Notifications() <-chan gatewayclient.Notification {
	return nil
}

func (s *stubDiagGatewayClientForTriggerAction) Ask(
	context.Context,
	protocol.AskParams,
	any,
	...gatewayclient.GatewayRPCCallOptions,
) error {
	return nil
}

func (s *stubDiagGatewayClientForTriggerAction) TriggerAction(
	_ context.Context,
	params protocol.TriggerActionParams,
	result any,
	_ ...gatewayclient.GatewayRPCCallOptions,
) error {
	s.calls = append(s.calls, stubDiagGatewayCall{
		method:  protocol.MethodGatewayExperimentalTriggerAction,
		payload: mustMarshalDiagPayload(params),
	})
	if frame, ok := result.(*gateway.MessageFrame); ok {
		*frame = gateway.MessageFrame{Type: gateway.FrameTypeAck}
	}
	return nil
}

func (s *stubDiagGatewayClientForTriggerAction) DeleteAskSession(
	context.Context,
	protocol.DeleteAskSessionParams,
	any,
	...gatewayclient.GatewayRPCCallOptions,
) error {
	return nil
}

func (s *stubDiagGatewayClientForTriggerAction) CallWithOptions(
	_ context.Context,
	method string,
	params any,
	result any,
	_ gatewayclient.GatewayRPCCallOptions,
) error {
	s.calls = append(s.calls, stubDiagGatewayCall{
		method:  method,
		payload: mustMarshalDiagPayload(params),
	})
	if frame, ok := result.(*gateway.MessageFrame); ok {
		*frame = gateway.MessageFrame{Type: gateway.FrameTypeAck}
	}
	return nil
}

func mustMarshalDiagPayload(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(string(payload))
}

func TestDefaultDiagCommandRunnerNoShellSession(t *testing.T) {
	originalReadEnv := readDiagEnvValue
	t.Cleanup(func() { readDiagEnvValue = originalReadEnv })

	readDiagEnvValue = func(key string) string { return "" }

	err := defaultDiagCommandRunner(context.Background(), diagCommandOptions{})
	if err == nil {
		t.Fatal("expected error when no shell session is available")
	}
	if !strings.Contains(err.Error(), errNoShellSession) {
		t.Fatalf("error = %q, want contains %q", err.Error(), errNoShellSession)
	}
}

func TestDefaultDiagInteractiveCommandRunnerNoShellSession(t *testing.T) {
	originalReadEnv := readDiagEnvValue
	t.Cleanup(func() { readDiagEnvValue = originalReadEnv })

	readDiagEnvValue = func(key string) string { return "" }

	err := defaultDiagInteractiveCommandRunner(context.Background(), diagCommandOptions{})
	if err == nil {
		t.Fatal("expected error when no shell session is available")
	}
	if !strings.Contains(err.Error(), errNoShellSession) {
		t.Fatalf("error = %q, want contains %q", err.Error(), errNoShellSession)
	}
}

func TestDefaultDiagAutoCommandRunnerNoShellSession(t *testing.T) {
	originalReadEnv := readDiagEnvValue
	t.Cleanup(func() { readDiagEnvValue = originalReadEnv })

	readDiagEnvValue = func(key string) string { return "" }

	err := defaultDiagAutoCommandRunner(context.Background(), diagAutoCommandOptions{QueryOnly: true}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error when no shell session is available")
	}
	if !strings.Contains(err.Error(), errNoShellSession) {
		t.Fatalf("error = %q, want contains %q", err.Error(), errNoShellSession)
	}
}

func TestWrapDiagGatewayError(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		if err := wrapDiagGatewayError(nil); err != nil {
			t.Fatalf("wrapDiagGatewayError(nil) = %v", err)
		}
	})

	t.Run("non-shell error passes through unchanged", func(t *testing.T) {
		original := errors.New("some other error")
		if err := wrapDiagGatewayError(original); err != original {
			t.Fatalf("expected unwrapped original error, got %v", err)
		}
	})

	t.Run("shell session error is wrapped", func(t *testing.T) {
		original := errors.New("gateway trigger_action failed (resource_not_found): target role stream is unavailable")
		wrapped := wrapDiagGatewayError(original)
		if wrapped == nil {
			t.Fatal("expected wrapped error")
		}
		if !strings.Contains(wrapped.Error(), errNoShellSession) {
			t.Fatalf("wrapped error = %q, want contains %q", wrapped.Error(), errNoShellSession)
		}
		if !strings.Contains(wrapped.Error(), original.Error()) {
			t.Fatalf("wrapped error = %q, want contains original %q", wrapped.Error(), original.Error())
		}
	})
}

func TestIsNoShellSessionError(t *testing.T) {
	if isNoShellSessionError(nil) {
		t.Fatal("isNoShellSessionError(nil) = true")
	}
	if !isNoShellSessionError(errors.New("target role stream is unavailable")) {
		t.Fatal("isNoShellSessionError should detect 'target role stream is unavailable'")
	}
	if isNoShellSessionError(errors.New("some other error")) {
		t.Fatal("isNoShellSessionError should not match other errors")
	}
}
