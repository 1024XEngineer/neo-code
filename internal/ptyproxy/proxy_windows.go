//go:build windows

package ptyproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/tools"

	"golang.org/x/sys/windows"
)

const (
	windowsDiagCallTimeout        = 90 * time.Second
	windowsAutoProbeTimeout       = 1500 * time.Millisecond
	windowsShellSessionPrefix     = "shell"
	windowsDiagnosisSessionPrefix = "diag-ask"
	windowsDiagnosisSkillID       = "terminal-diagnosis"

	windowsEnableVirtualTerminalProcessing = 0x0004
	windowsEnableVirtualTerminalInput      = 0x0200
)

var (
	windowsShellSessionSeq     atomic.Uint64
	windowsDiagnosisSessionSeq atomic.Uint64

	proxyInitializedBanner = "[ NeoCode Proxy initialized ]"
	proxyExitedBanner      = "[ NeoCode Proxy exited ]"
)

// RunManualShell 在 Windows 平台启动 shell 并接入网关通知控制链路。
func RunManualShell(ctx context.Context, options ManualShellOptions) error {
	normalized, err := NormalizeShellOptions(options)
	if err != nil {
		return err
	}

	shellPath, shellArgs := resolveWindowsShellCommand(strings.TrimSpace(normalized.Shell))
	sessionID := generateWindowsShellSessionID(os.Getpid())
	logBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity)
	commandLogBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)

	var outputMu sync.Mutex
	synchronizedStdout := &serializedWriter{writer: normalized.Stdout, lock: &outputMu}
	synchronizedStderr := &serializedWriter{writer: normalized.Stderr, lock: &outputMu}
	outputSink := io.MultiWriter(synchronizedStdout, logBuffer)
	printProxyInitializedBanner(synchronizedStdout)
	writeProxyLine(synchronizedStdout, "[ NeoCode shell for Windows ]")
	writeProxyf(synchronizedStdout, "[ shell: %s ]\n", windowsShellDisplayName(shellPath))
	writeProxyf(synchronizedStdout, "[ diagnostics: `%s diag`, `%s diag -i`, `%s diag auto off` ]\n",
		windowsNeoCodeCommandExample(shellPath),
		windowsNeoCodeCommandExample(shellPath),
		windowsNeoCodeCommandExample(shellPath),
	)
	writeProxyLine(synchronizedStdout, "[ IDM: `diag -i` enters follow-up mode; use `@ai <question>`, `exit` to leave ]")

	restoreConsoleMode, modeErr := enableWindowsConsoleModes()
	if modeErr != nil && synchronizedStderr != nil {
		writeProxyf(synchronizedStderr, "neocode shell: enable virtual terminal mode failed: %v\n", modeErr)
	}
	defer restoreConsoleMode()

	shellEnv := MergeEnvVar(os.Environ(), ShellSessionEnv, sessionID)
	conPTY, err := startWindowsConPTYShell(shellPath, shellArgs, normalized.Workdir, shellEnv)
	if err != nil {
		return fmt.Errorf("ptyproxy: start windows conpty shell: %w", err)
	}
	defer conPTY.Close()
	stopResizeWatcher := watchWindowsConPTYResize(conPTY, synchronizedStderr)
	defer stopResizeWatcher()

	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)
	autoState.OSCReady.Store(false)
	printAutoModeBanner(synchronizedStdout, autoState)

	inputCtx, cancelInput := context.WithCancel(context.Background())
	defer cancelInput()

	idm := newIDMController(idmControllerOptions{
		PTYWriter:      conPTY.InputWriter(),
		Output:         synchronizedStdout,
		Stderr:         synchronizedStderr,
		AutoState:      autoState,
		LogBuffer:      logBuffer,
		DefaultCap:     DefaultRingBufferCapacity,
		Workdir:        normalized.Workdir,
		ShellSessionID: sessionID,
	})

	outputDone := make(chan struct{})
	inputTracker := &commandTracker{}
	autoTriggerCh := make(chan diagnoseTrigger, 2)
	recentTriggerStore := &diagnosisTriggerStore{}
	go func() {
		defer close(outputDone)
		streamWindowsShellOutputWithIDM(
			conPTY.OutputReader(),
			outputSink,
			commandLogBuffer,
			inputTracker,
			autoTriggerCh,
			recentTriggerStore,
			autoState,
			idm,
		)
	}()
	go pumpWindowsProxyInput(inputCtx, normalized.Stdin, conPTY.InputWriter(), inputTracker, idm)

	stopInterruptForwarder := watchWindowsInterruptSignals(conPTY.InputWriter(), synchronizedStderr, idm.HandleSignal)
	defer stopInterruptForwarder()

	go func() {
		<-ctx.Done()
		cancelInput()
	}()

	rpcClient, gatewayReady := connectWindowsGateway(normalized, synchronizedStderr)
	if rpcClient != nil {
		defer rpcClient.Close()
	}
	if gatewayReady {
		idm.rpcClient = rpcClient
		cleanupZombieIDMSessions(rpcClient, synchronizedStderr)
	}

	var (
		notificationStopFn          = func() {}
		notificationRelayWG         sync.WaitGroup
		gatewayEventNotifications   <-chan gatewayclient.Notification
		gatewayControlNotifications <-chan gatewayclient.Notification
		publishShellState           = func(bool) {}
	)
	if gatewayReady {
		eventCh := make(chan gatewayclient.Notification, 256)
		controlCh := make(chan gatewayclient.Notification, 64)
		demuxCtx, demuxCancel := context.WithCancel(context.Background())
		notificationStopFn = demuxCancel
		gatewayEventNotifications = eventCh
		gatewayControlNotifications = controlCh
		notificationRelayWG.Add(1)
		go func() {
			defer notificationRelayWG.Done()
			defer close(eventCh)
			defer close(controlCh)
			demuxGatewayNotifications(demuxCtx, rpcClient.Notifications(), eventCh, controlCh)
		}()

		publishShellState = func(autoEnabled bool) {
			if err := updateWindowsShellState(rpcClient, sessionID, autoEnabled); err != nil && synchronizedStderr != nil {
				writeProxyf(synchronizedStderr, "neocode shell: update auto state failed: %v\n", err)
			}
		}
	}
	defer func() {
		notificationStopFn()
		notificationRelayWG.Wait()
	}()
	idm.notificationStream = gatewayEventNotifications

	controlCtx, cancelControl := context.WithCancel(context.Background())
	diagnoseJobCh := make(chan diagnoseJob, 4)
	var controlWG sync.WaitGroup
	if gatewayReady {
		stateCtx, stateCancel := context.WithTimeout(context.Background(), windowsDiagCallTimeout)
		bindErr := bindWindowsShellRoleStream(stateCtx, rpcClient, sessionID, autoState.Enabled.Load())
		stateCancel()
		if bindErr != nil {
			writeProxyf(synchronizedStderr, "neocode shell: bind shell stream failed: %v\n", bindErr)
		} else {
			controlWG.Add(1)
			go func() {
				defer controlWG.Done()
				consumeWindowsGatewayNotifications(
					controlCtx,
					gatewayControlNotifications,
					sessionID,
					diagnoseJobCh,
					idm,
					autoState,
					publishShellState,
					synchronizedStdout,
					synchronizedStderr,
				)
			}()
		}
	}

	diagCtx, cancelDiag := context.WithCancel(context.Background())
	var diagWG sync.WaitGroup
	diagWG.Add(1)
	autoDiagFatalCh := make(chan error, 1)
	diagCoordinator := newDiagnosisCoordinator()
	go func() {
		defer diagWG.Done()
		consumeDiagSignals(
			diagCtx,
			rpcClient,
			gatewayEventNotifications,
			diagnoseJobCh,
			synchronizedStdout,
			logBuffer,
			normalized,
			sessionID,
			recentTriggerStore,
			autoState,
			func(diagnoseErr error) {
				if diagnoseErr == nil {
					return
				}
				select {
				case autoDiagFatalCh <- diagnoseErr:
				default:
				}
			},
			diagCoordinator,
		)
	}()

	go func() {
		probeTimer := time.NewTimer(windowsAutoProbeTimeout)
		defer probeTimer.Stop()
		select {
		case <-probeTimer.C:
			if !autoState.OSCReady.Load() {
				autoState.Enabled.Store(false)
				publishShellState(false)
				writeProxyf(synchronizedStderr, "neocode shell: OSC133 probe timed out, auto diagnosis downgraded\n")
				writeProxyLine(synchronizedStdout, "[ ! ] Auto diagnosis is downgraded because Windows shell integration is unavailable. Use `neocode diag` or `neocode diag -i` manually.")
			}
		case <-diagCtx.Done():
			return
		}
	}()

	var triggerWG sync.WaitGroup
	triggerWG.Add(1)
	go func() {
		defer triggerWG.Done()
		for trigger := range autoTriggerCh {
			select {
			case <-diagCtx.Done():
				return
			case diagnoseJobCh <- diagnoseJob{Trigger: trigger, IsAuto: true}:
			}
		}
	}()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- conPTY.Wait()
	}()

	var waitErr error
	forcedByAutoDiagFailure := false
	select {
	case <-ctx.Done():
		_ = conPTY.Terminate()
		waitErr = <-waitDone
	case diagnoseErr := <-autoDiagFatalCh:
		forcedByAutoDiagFailure = true
		writeProxyLine(synchronizedStdout, "[ x ] Auto diagnosis failed, NeoCode proxy will exit and return to the native shell.")
		writeProxyf(synchronizedStdout, "[ reason: %s ]\n", strings.TrimSpace(diagnoseErr.Error()))
		_ = conPTY.Terminate()
		waitErr = <-waitDone
	case waitErr = <-waitDone:
	}

	idm.Exit()
	cancelControl()
	controlWG.Wait()
	cancelInput()
	cancelDiag()
	_ = conPTY.CloseOutputReader()
	<-outputDone
	close(autoTriggerCh)
	triggerWG.Wait()
	diagWG.Wait()

	printProxyExitedBanner(synchronizedStdout)
	if forcedByAutoDiagFailure {
		return nil
	}
	return normalizeWindowsShellWaitError(ctx, waitErr)
}

// connectWindowsGateway 为 Windows shell 建立网关 RPC 客户端并完成鉴权。
func connectWindowsGateway(options ManualShellOptions, errWriter io.Writer) (*gatewayclient.GatewayRPCClient, bool) {
	client, err := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress:       strings.TrimSpace(options.GatewayListenAddress),
		TokenFile:           strings.TrimSpace(options.GatewayTokenFile),
		DisableHeartbeatLog: true,
	})
	if err != nil {
		writeProxyf(errWriter, "neocode shell: gateway client init failed: %v\n", err)
		return nil, false
	}
	authCtx, cancel := context.WithTimeout(context.Background(), windowsDiagCallTimeout)
	defer cancel()
	if err := client.Authenticate(authCtx); err != nil {
		writeProxyf(errWriter, "neocode shell: gateway auth failed: %v\n", err)
		return client, false
	}
	return client, true
}

func resolveWindowsShellCommand(configuredShell string) (string, []string) {
	candidate := strings.TrimSpace(configuredShell)
	if candidate == "" {
		candidate = defaultWindowsInteractiveShell()
	}

	base := strings.ToLower(filepath.Base(candidate))
	switch base {
	case "cmd.exe", "cmd":
		return candidate, []string{"/Q", "/K", windowsCmdShellIntegrationCommand()}
	case "powershell.exe", "powershell", "pwsh.exe", "pwsh":
		return candidate, []string{"-NoLogo", "-NoExit", "-Command", windowsPowerShellIntegrationCommand()}
	default:
		return candidate, nil
	}
}

// windowsPowerShellIntegrationCommand 安装临时 prompt 钩子，用 OSC133 对齐跨平台命令生命周期。
func windowsPowerShellIntegrationCommand() string {
	return strings.Join([]string{
		"$global:__NeoCodeESC = [string][char]27",
		"$global:__NeoCodeBEL = [string][char]7",
		"function global:__NeoCodeEmitOSC133([string]$payload) { [Console]::Write($global:__NeoCodeESC + ']133;' + $payload + $global:__NeoCodeBEL) }",
		"$global:__NeoCodePromptInitialized = $false",
		"function global:prompt {",
		"  $neoCodeCommandSucceeded = $?",
		"  if ($global:__NeoCodePromptInitialized) { if ($neoCodeCommandSucceeded) { __NeoCodeEmitOSC133 'D;0' } else { __NeoCodeEmitOSC133 'D;1' } }",
		"  $global:__NeoCodePromptInitialized = $true",
		"  __NeoCodeEmitOSC133 'A'",
		"  return \"PS $($executionContext.SessionState.Path.CurrentLocation)$('>' * ($nestedPromptLevel + 1)) \"",
		"}",
		"$Host.UI.RawUI.WindowTitle = 'NeoCode Shell'",
	}, "; ")
}

// windowsCmdShellIntegrationCommand 注入 cmd 的 PROMPT OSC133 钩子，至少为 prompt_ready 提供稳定信号。
func windowsCmdShellIntegrationCommand() string {
	return `for /f "delims=" %A in ('echo prompt $E^| cmd') do set "NEOCODE_ESC=%A" & prompt %NEOCODE_ESC%]133;A$G$_$P$G`
}

var windowsLookPath = exec.LookPath

// defaultWindowsInteractiveShell 优先选择 PowerShell，以保持 Windows shell 中的命令语义和外层 PowerShell 一致。
func defaultWindowsInteractiveShell() string {
	for _, candidate := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := windowsLookPath(candidate); err == nil && strings.TrimSpace(path) != "" {
			return path
		}
	}
	if comspec := strings.TrimSpace(os.Getenv("COMSPEC")); comspec != "" {
		return comspec
	}
	return "powershell.exe"
}

// windowsShellDisplayName 返回欢迎横幅中展示的 shell 名称，避免把完整路径塞进提示信息。
func windowsShellDisplayName(shellPath string) string {
	base := strings.TrimSpace(filepath.Base(shellPath))
	if base == "" || base == "." {
		return strings.TrimSpace(shellPath)
	}
	return base
}

// windowsNeoCodeCommandExample 根据当前 shell 返回可直接复制执行的 NeoCode 命令前缀。
func windowsNeoCodeCommandExample(shellPath string) string {
	if isWindowsCmdShell(shellPath) {
		return `.\\neocode`
	}
	return "./neocode"
}

// isWindowsCmdShell 判断当前 Windows 交互 shell 是否为 cmd。
func isWindowsCmdShell(shellPath string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(shellPath)))
	return base == "cmd.exe" || base == "cmd"
}

// normalizeWindowsShellWaitError 把用户主动退出交互 shell 的进程码视为正常结束。
func normalizeWindowsShellWaitError(ctx context.Context, waitErr error) error {
	if waitErr == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return waitErr
	}
	var exitErr windowsShellExitError
	if errors.As(waitErr, &exitErr) {
		return nil
	}
	return waitErr
}

// printAutoModeBanner 输出当前自动诊断状态，保持 Windows 与 Unix shell 的提示一致。
func printAutoModeBanner(writer io.Writer, autoState *autoRuntimeState) {
	if writer == nil {
		return
	}
	if autoState != nil && autoState.Enabled.Load() {
		writeProxyLine(writer, "[ auto diagnosis enabled ]")
		return
	}
	writeProxyLine(writer, "[ i ] Auto diagnosis is disabled. Use `neocode diag` for manual analysis.")
}

func generateWindowsShellSessionID(pid int) string {
	if pid <= 0 {
		pid = os.Getpid()
	}
	sequence := windowsShellSessionSeq.Add(1)
	return fmt.Sprintf("%s-%d-%d", windowsShellSessionPrefix, pid, sequence)
}

func generateWindowsDiagnosisSessionID() string {
	sequence := windowsDiagnosisSessionSeq.Add(1)
	return fmt.Sprintf("%s-%d-%d", windowsDiagnosisSessionPrefix, os.Getpid(), sequence)
}

func bindWindowsShellRoleStream(
	ctx context.Context,
	rpcClient *gatewayclient.GatewayRPCClient,
	sessionID string,
	autoEnabled bool,
) error {
	if rpcClient == nil {
		return errors.New("gateway rpc client is not ready")
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return errors.New("shell session id is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return bindWindowsShellRoleStreamWithCaller(ctx, normalizedSessionID, autoEnabled, func(
		callCtx context.Context,
		params protocol.BindStreamParams,
		ack *gateway.MessageFrame,
	) error {
		return rpcClient.CallWithOptions(
			callCtx,
			protocol.MethodGatewayBindStream,
			params,
			ack,
			gatewayclient.GatewayRPCCallOptions{
				Timeout: windowsDiagCallTimeout,
				Retries: 0,
			},
		)
	})
}

// bindWindowsShellRoleStreamWithCaller 复用 Windows 端 bind_stream 兼容回退逻辑。
func bindWindowsShellRoleStreamWithCaller(
	ctx context.Context,
	sessionID string,
	autoEnabled bool,
	caller func(context.Context, protocol.BindStreamParams, *gateway.MessageFrame) error,
) error {
	if caller == nil {
		return errors.New("bind stream caller is nil")
	}
	primaryParams := protocol.BindStreamParams{
		SessionID: strings.TrimSpace(sessionID),
		Channel:   "all",
		Role:      "shell",
		State: map[string]any{
			"auto_enabled": autoEnabled,
		},
	}
	legacyParams := primaryParams
	legacyParams.State = nil

	var ack gateway.MessageFrame
	if err := caller(ctx, primaryParams, &ack); err != nil {
		if !shouldFallbackWindowsBindStreamState(err) {
			return err
		}
		ack = gateway.MessageFrame{}
		if retryErr := caller(ctx, legacyParams, &ack); retryErr != nil {
			return retryErr
		}
		return validateWindowsBindStreamAckFrame(ack)
	}
	return validateWindowsBindStreamAckFrame(ack)
}

// shouldFallbackWindowsBindStreamState 判断 bind_stream 是否需要回退到无 state 形式。
func shouldFallbackWindowsBindStreamState(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *gatewayclient.GatewayRPCError
	if errors.As(err, &rpcErr) {
		if rpcErr != nil && rpcErr.Code == protocol.JSONRPCCodeInvalidParams {
			return true
		}
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "invalid params")
}

// validateWindowsBindStreamAckFrame 校验 bind_stream ACK 帧。
func validateWindowsBindStreamAckFrame(ack gateway.MessageFrame) error {
	if ack.Type == gateway.FrameTypeError && ack.Error != nil {
		return fmt.Errorf(
			"gateway bind_stream failed (%s): %s",
			strings.TrimSpace(ack.Error.Code),
			strings.TrimSpace(ack.Error.Message),
		)
	}
	if ack.Type != gateway.FrameTypeAck {
		return fmt.Errorf("unexpected gateway frame type for bind_stream: %s", ack.Type)
	}
	return nil
}

func consumeWindowsGatewayNotifications(
	ctx context.Context,
	controlNotifications <-chan gatewayclient.Notification,
	shellSessionID string,
	diagnoseJobCh chan<- diagnoseJob,
	idm *idmController,
	autoState *autoRuntimeState,
	publishShellState func(bool),
	output io.Writer,
	errWriter io.Writer,
) {
	if controlNotifications == nil {
		return
	}
	targetSessionID := strings.TrimSpace(shellSessionID)
	for {
		select {
		case <-ctx.Done():
			return
		case notification, ok := <-controlNotifications:
			if !ok {
				return
			}
			payload, ok := decodeGatewayNotificationPayload(notification.Params)
			if !ok {
				continue
			}
			sessionID := strings.TrimSpace(readMapString(payload, "session_id"))
			if sessionID != "" && !strings.EqualFold(sessionID, targetSessionID) {
				continue
			}
			action := strings.ToLower(strings.TrimSpace(readMapString(payload, "action")))
			switch action {
			case protocol.TriggerActionDiagnose:
				if diagnoseJobCh == nil {
					continue
				}
				select {
				case diagnoseJobCh <- diagnoseJob{IsAuto: false}:
				case <-ctx.Done():
					return
				default:
					writeProxyLine(output, "[ i ] Diagnosis is already queued; please wait for the current request to finish.")
				}
			case protocol.TriggerActionIDMEnter:
				if idm == nil {
					continue
				}
				if err := idm.Enter(); err != nil && errWriter != nil {
					writeProxyf(errWriter, "neocode shell: idm enter rejected: %v\n", err)
				}
			case protocol.TriggerActionAutoOn:
				if autoState != nil {
					autoState.Enabled.Store(true)
				}
				if publishShellState != nil {
					publishShellState(true)
				}
				writeProxyLine(output, "[ auto diagnosis enabled ]")
			case protocol.TriggerActionAutoOff:
				if autoState != nil {
					autoState.Enabled.Store(false)
				}
				if publishShellState != nil {
					publishShellState(false)
				}
				writeProxyLine(output, "[ auto diagnosis disabled ]")
			}
		}
	}
}

// demuxGatewayNotifications 将网关通知拆分为 runtime 事件流与控制通知流，避免多方竞争同一通知通道。
func demuxGatewayNotifications(
	ctx context.Context,
	source <-chan gatewayclient.Notification,
	eventSink chan<- gatewayclient.Notification,
	controlSink chan<- gatewayclient.Notification,
) {
	if source == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case notification, ok := <-source:
			if !ok {
				return
			}
			switch strings.TrimSpace(notification.Method) {
			case protocol.MethodGatewayEvent:
				if !forwardGatewayNotification(ctx, eventSink, notification) {
					return
				}
			case protocol.MethodGatewayNotification:
				if !forwardGatewayNotification(ctx, controlSink, notification) {
					return
				}
			}
		}
	}
}

// forwardGatewayNotification 在上下文可用时转发通知，避免退出阶段 goroutine 堵塞。
func forwardGatewayNotification(
	ctx context.Context,
	target chan<- gatewayclient.Notification,
	notification gatewayclient.Notification,
) bool {
	if target == nil {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case target <- notification:
		return true
	}
}

// streamWindowsShellOutputWithIDM 持续读取 ConPTY 输出，并在 IDM 激活时过滤原生回显。
func streamWindowsShellOutputWithIDM(
	ptyReader io.Reader,
	outputSink io.Writer,
	commandLogBuffer *UTF8RingBuffer,
	tracker *commandTracker,
	autoTriggerCh chan<- diagnoseTrigger,
	recentTriggerStore *diagnosisTriggerStore,
	autoState *autoRuntimeState,
	idm *idmController,
) {
	if ptyReader == nil || outputSink == nil || commandLogBuffer == nil {
		return
	}
	parser := &OSC133Parser{}
	altScreen := newAltScreenState(IsAltScreenGuardEnabledFromEnv())
	collectingCommand := false
	pendingTrigger := (*diagnoseTrigger)(nil)
	fallbackCommandBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)

	buffer := make([]byte, 4096)
	for {
		readBytes, err := ptyReader.Read(buffer)
		if readBytes > 0 {
			altScreen.Observe(buffer[:readBytes])
			cleanOutput, events := parser.Feed(buffer[:readBytes])
			if idm != nil && len(cleanOutput) > 0 {
				cleanOutput = idm.FilterPTYOutput(cleanOutput)
			}
			if len(cleanOutput) > 0 {
				_, _ = outputSink.Write(cleanOutput)
				_, _ = fallbackCommandBuffer.Write(cleanOutput)
				if collectingCommand {
					_, _ = commandLogBuffer.Write(cleanOutput)
				}
			}
			for _, event := range events {
				if idm != nil {
					idm.OnShellEvent(event)
				}
				switch event.Type {
				case ShellEventPromptReady:
					if autoState != nil {
						autoState.OSCReady.Store(true)
					}
					if pendingTrigger != nil && autoState != nil && autoState.Enabled.Load() {
						if altScreen.ShouldSuppressAutoTrigger(true) {
							pendingTrigger = nil
							fallbackCommandBuffer.Reset()
							continue
						}
						select {
						case autoTriggerCh <- *pendingTrigger:
						default:
						}
						pendingTrigger = nil
					}
					fallbackCommandBuffer.Reset()
				case ShellEventCommandStart:
					collectingCommand = true
					commandLogBuffer = NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)
					fallbackCommandBuffer.Reset()
				case ShellEventCommandDone:
					collectingCommand = false
					commandText := ""
					if tracker != nil {
						commandText = tracker.LastCommand()
					}
					outputText := commandLogBuffer.SnapshotString()
					if !hasMeaningfulOutput(outputText) {
						outputText = fallbackCommandBuffer.SnapshotString()
					}
					trigger := diagnoseTrigger{
						CommandText: commandText,
						ExitCode:    event.ExitCode,
						OutputText:  outputText,
					}
					if recentTriggerStore != nil {
						recentTriggerStore.Remember(trigger)
					}
					if ShouldTriggerAutoDiagnosis(event.ExitCode, commandText, outputText) {
						if altScreen.ShouldSuppressAutoTrigger(true) {
							pendingTrigger = nil
							continue
						}
						pendingTrigger = &trigger
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// pumpWindowsProxyInput 读取终端输入并在 IDM 激活时切换到本地拦截输入逻辑。
func pumpWindowsProxyInput(
	ctx context.Context,
	src io.Reader,
	ptyWriter io.Writer,
	tracker *commandTracker,
	idm *idmController,
) {
	if src == nil || ptyWriter == nil {
		return
	}
	buffer := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		readCount, err := src.Read(buffer)
		if readCount > 0 {
			payload := buffer[:readCount]
			for _, item := range payload {
				if idm != nil && idm.IsActive() {
					if idm.ShouldPassthroughInput() {
						if tracker != nil {
							tracker.Observe([]byte{item})
						}
						_, _ = ptyWriter.Write([]byte{item})
						continue
					}
					idm.HandleInputByte(item)
					continue
				}
				if tracker != nil {
					tracker.Observe([]byte{item})
				}
				_, _ = ptyWriter.Write([]byte{item})
			}
		}
		if err != nil {
			return
		}
	}
}

// watchWindowsInterruptSignals 监听 Ctrl+C 并优先交给 IDM 处理，未拦截时转发到 ConPTY 输入端。
func watchWindowsInterruptSignals(
	ptyWriter io.Writer,
	errWriter io.Writer,
	interceptor func(os.Signal) bool,
) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				return
			case signalValue, ok := <-signals:
				if !ok {
					return
				}
				if interceptor != nil && interceptor(signalValue) {
					continue
				}
				if ptyWriter == nil {
					continue
				}
				if _, err := ptyWriter.Write([]byte{0x03}); err != nil && errWriter != nil {
					writeProxyf(errWriter, "neocode shell: forward ctrl+c failed: %v\n", err)
				}
			}
		}
	}()
	return func() {
		close(stopCh)
		signal.Stop(signals)
		wg.Wait()
	}
}

type windowsConsoleModeSnapshot struct {
	handle           windows.Handle
	mode             uint32
	restoreTransform func(uint32) uint32
}

// enableWindowsConsoleModes 为输出启用 VT 渲染，并确保输入保持普通字符模式。
func enableWindowsConsoleModes() (func(), error) {
	snapshots := make([]windowsConsoleModeSnapshot, 0, 3)
	var firstErr error
	applyMode := func(file *os.File, transform func(uint32) uint32, restoreTransform func(uint32) uint32) {
		if file == nil {
			return
		}
		handle := windows.Handle(file.Fd())
		var currentMode uint32
		if err := windows.GetConsoleMode(handle, &currentMode); err != nil {
			return
		}
		snapshots = append(snapshots, windowsConsoleModeSnapshot{
			handle:           handle,
			mode:             currentMode,
			restoreTransform: restoreTransform,
		})
		targetMode := transform(currentMode)
		if err := windows.SetConsoleMode(handle, targetMode); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	applyMode(os.Stdout, func(mode uint32) uint32 {
		return mode | windowsEnableVirtualTerminalProcessing
	}, nil)
	applyMode(os.Stderr, func(mode uint32) uint32 {
		return mode | windowsEnableVirtualTerminalProcessing
	}, nil)
	applyMode(os.Stdin, sanitizeWindowsInputConsoleMode, sanitizeWindowsInputConsoleMode)

	restore := func() {
		for index := len(snapshots) - 1; index >= 0; index-- {
			mode := snapshots[index].mode
			if snapshots[index].restoreTransform != nil {
				mode = snapshots[index].restoreTransform(mode)
			}
			_ = windows.SetConsoleMode(snapshots[index].handle, mode)
		}
	}
	return restore, firstErr
}

// sanitizeWindowsInputConsoleMode 清除 VT 输入，避免按键被编码成 `^[[...` 序列传给子 shell。
func sanitizeWindowsInputConsoleMode(mode uint32) uint32 {
	return mode &^ windowsEnableVirtualTerminalInput
}

// consumeDiagSignals 消费手动与自动诊断任务，复用统一诊断协调器避免重复请求。
func consumeDiagSignals(
	ctx context.Context,
	rpcClient *gatewayclient.GatewayRPCClient,
	_ <-chan gatewayclient.Notification,
	jobCh <-chan diagnoseJob,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	recentTriggerStore *diagnosisTriggerStore,
	autoState *autoRuntimeState,
	onAutoDiagnoseFailure func(error),
	coordinator *diagnosisCoordinator,
) {
	var autoWG sync.WaitGroup
	autoSlots := make(chan struct{}, 1)
	defer autoWG.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobCh:
			if !ok {
				return
			}
			if job.IsAuto {
				if coordinator != nil {
					prepared, prepareErr := prepareDiagnoseRequest(buffer, options, shellSessionID, job.Trigger)
					if prepareErr == nil && coordinator.shouldDropAuto(prepared.Fingerprint) {
						continue
					}
				}
				select {
				case autoSlots <- struct{}{}:
				case <-ctx.Done():
					return
				default:
					continue
				}
				autoWG.Add(1)
				go func(autoJob diagnoseJob) {
					defer autoWG.Done()
					defer func() { <-autoSlots }()
					diagnoseErr := runSingleDiagnosisWithCoordinator(
						ctx,
						coordinator,
						rpcClient,
						nil,
						output,
						buffer,
						options,
						shellSessionID,
						autoJob.Trigger,
						true,
						autoState,
					)
					if diagnoseErr != nil && onAutoDiagnoseFailure != nil && shouldTerminateShellOnAutoDiagnoseError(diagnoseErr) {
						onAutoDiagnoseFailure(diagnoseErr)
					}
				}(job)
				continue
			}
			_ = runSingleDiagnosisWithCoordinator(
				ctx,
				coordinator,
				rpcClient,
				nil,
				output,
				buffer,
				options,
				shellSessionID,
				resolveManualDiagnoseTrigger(job.Trigger, recentTriggerStore),
				false,
				autoState,
			)
		}
	}
}

// shouldTerminateShellOnAutoDiagnoseError 判断自动诊断错误是否代表代理链路不可恢复。
func shouldTerminateShellOnAutoDiagnoseError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	if strings.Contains(message, "context deadline exceeded") {
		return false
	}
	if strings.Contains(message, "rate limit") || strings.Contains(message, "rate_limited") {
		return false
	}
	if strings.Contains(message, "provider generate") || strings.Contains(message, "sdk stream error") {
		return false
	}
	if strings.Contains(message, "unauthorized") {
		return true
	}
	if strings.Contains(message, "transport error") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "no such file") ||
		strings.Contains(message, "use of closed network connection") {
		return true
	}
	return false
}

// runSingleDiagnosisWithCoordinator 执行一次诊断，并统一渲染快速反馈与最终结果。
func runSingleDiagnosisWithCoordinator(
	ctx context.Context,
	coordinator *diagnosisCoordinator,
	rpcClient *gatewayclient.GatewayRPCClient,
	eventStream <-chan gatewayclient.Notification,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	shellSessionID string,
	trigger diagnoseTrigger,
	isAuto bool,
	autoState *autoRuntimeState,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if autoState != nil && isAuto && !autoState.Enabled.Load() {
		return nil
	}
	prepared, err := prepareDiagnoseRequest(buffer, options, shellSessionID, trigger)
	if err != nil {
		return err
	}
	renderDiagnosisInitialFeedback(output, prepared, isAuto)

	timeout := windowsDiagCallTimeout
	execute := func() (tools.ToolResult, error) {
		result, runErr := executePreparedDiagnoseToolWithTimeout(
			rpcClient,
			nil,
			options,
			prepared,
			timeout,
		)
		return result, runErr
	}
	outcome := diagnosisOutcome{}
	if coordinator != nil {
		outcome = coordinator.run(ctx, prepared.Fingerprint, execute)
	} else {
		result, runErr := execute()
		outcome = diagnosisOutcome{Result: result, Err: runErr}
	}
	if outcome.Err != nil {
		if !isAuto {
			renderDiagnosis(output, outcome.Err.Error(), true)
		}
		return outcome.Err
	}
	renderDiagnosis(output, outcome.Result.Content, outcome.Result.IsError)
	return nil
}

func updateWindowsShellState(rpcClient *gatewayclient.GatewayRPCClient, sessionID string, autoEnabled bool) error {
	callCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return bindWindowsShellRoleStream(callCtx, rpcClient, sessionID, autoEnabled)
}

func decodeGatewayNotificationPayload(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

func readMapString(container map[string]any, key string) string {
	if container == nil {
		return ""
	}
	value, exists := container[strings.TrimSpace(key)]
	if !exists || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func normalizePayloadMap(payload any) (map[string]any, bool) {
	switch typed := payload.(type) {
	case map[string]any:
		return typed, true
	case nil:
		return nil, false
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return nil, false
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, false
		}
		return decoded, true
	}
}

// renderDiagnosis 以统一格式展示诊断最终结果。
func renderDiagnosis(output io.Writer, content string, isError bool) {
	headerColor := "\033[36m"
	if isError {
		headerColor = "\033[31m"
	}
	writeProxyf(output, "\n%s[NeoCode Diagnosis]\033[0m\n", headerColor)

	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		writeProxyLine(output, "- no diagnosis output")
		return
	}

	var parsed diagnoseToolResult
	if err := json.Unmarshal([]byte(trimmedContent), &parsed); err != nil || strings.TrimSpace(parsed.RootCause) == "" {
		writeProxyLine(output, trimmedContent)
		return
	}

	writeProxyf(output, "confidence: %.2f\n", parsed.Confidence)
	writeProxyf(output, "root cause: %s\n", strings.TrimSpace(parsed.RootCause))
	if len(parsed.InvestigationCommands) > 0 {
		writeProxyLine(output, "investigation commands:")
		for _, command := range parsed.InvestigationCommands {
			writeProxyf(output, "- %s\n", strings.TrimSpace(command))
		}
	}
	if len(parsed.FixCommands) > 0 {
		writeProxyLine(output, "fix commands:")
		for _, command := range parsed.FixCommands {
			writeProxyf(output, "- %s\n", strings.TrimSpace(command))
		}
	}
}

func renderWindowsDiagnosis(output io.Writer, content string) {
	if output == nil {
		return
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		writeProxyLine(output, "\n[NeoCode Diagnosis]")
		writeProxyLine(output, "- no diagnosis output")
		return
	}
	writeProxyLine(output, "\n[NeoCode Diagnosis]")
	writeProxyLine(output, trimmed)
}

func printProxyInitializedBanner(writer io.Writer) {
	writeProxyLine(writer, proxyInitializedBanner)
}

func printProxyExitedBanner(writer io.Writer) {
	writeProxyLine(writer, proxyExitedBanner)
}

func writeProxyLine(writer io.Writer, text string) {
	if writer == nil {
		return
	}
	_, _ = io.WriteString(writer, strings.TrimRight(text, "\r\n")+"\r\n")
}

func writeProxyf(writer io.Writer, format string, args ...any) {
	writeProxyLine(writer, fmt.Sprintf(strings.TrimRight(format, "\r\n"), args...))
}

type serializedWriter struct {
	writer io.Writer
	lock   *sync.Mutex
}

func (w *serializedWriter) Write(payload []byte) (int, error) {
	if w == nil || w.writer == nil {
		return len(payload), nil
	}
	if w.lock == nil {
		return w.writer.Write(payload)
	}
	w.lock.Lock()
	defer w.lock.Unlock()
	return w.writer.Write(payload)
}
