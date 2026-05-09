//go:build windows

package ptyproxy

import (
	"context"
	"encoding/base64"
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
	printProxyInitializedBanner(synchronizedStderr)
	writeProxyLine(synchronizedStderr, "[ NeoCode shell for Windows ]")
	writeProxyf(synchronizedStderr, "[ shell: %s ]\n", windowsShellDisplayName(shellPath))
	writeProxyf(synchronizedStderr, "[ diagnostics: `%s diag`, `%s diag -i`, `%s diag auto off` ]\n",
		windowsNeoCodeCommandExample(shellPath),
		windowsNeoCodeCommandExample(shellPath),
		windowsNeoCodeCommandExample(shellPath),
	)
	writeProxyLine(synchronizedStderr, "[ IDM: `diag -i` enters follow-up mode; use `@ai <question>`, `exit` to leave ]")

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
	var ptyInputMu sync.Mutex
	ptyInput := &serializedWriter{writer: conPTY.InputWriter(), lock: &ptyInputMu}

	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)
	autoState.OSCReady.Store(false)
	printAutoModeBanner(synchronizedStderr, autoState)

	inputCtx, cancelInput := context.WithCancel(context.Background())
	defer cancelInput()

	idm := newIDMController(idmControllerOptions{
		PTYWriter:      ptyInput,
		Output:         synchronizedStderr,
		Stderr:         synchronizedStderr,
		AutoState:      autoState,
		LogBuffer:      logBuffer,
		DefaultCap:     DefaultRingBufferCapacity,
		Workdir:        normalized.Workdir,
		ShellSessionID: sessionID,
	})

	outputDone := make(chan struct{})
	inputTracker := &commandTracker{}
	diagDispatcher := newWindowsDiagnosisDispatcher(
		shellPath,
		func(text string) error { return conPTY.WriteScreenText(text) },
		ptyInput,
		inputTracker,
		synchronizedStderr,
	)
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
			diagDispatcher,
		)
	}()
	go pumpWindowsProxyInput(inputCtx, normalized.Stdin, ptyInput, inputTracker, idm)

	stopInterruptForwarder := watchWindowsInterruptSignals(ptyInput, synchronizedStderr, idm.HandleSignal)
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
					synchronizedStderr,
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
			synchronizedStderr,
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
			diagRuntimeConfig{
				CallTimeout:     windowsDiagCallTimeout,
				AutoCallTimeout: windowsDiagCallTimeout,
				RenderInitial: func(output io.Writer, prepared preparedDiagnosisRequest, isAuto bool) {
					lines, emit := buildDiagnosisInitialFeedbackLines(prepared, isAuto)
					if !emit {
						return
					}
					diagDispatcher.Enqueue(lines)
				},
				RenderFinal: func(output io.Writer, content string, isError bool) {
					diagDispatcher.Enqueue(buildDiagnosisResultLines(content))
				},
				RenderError: func(output io.Writer, message string) {
					diagDispatcher.Enqueue(buildDiagnosisResultLines(strings.TrimSpace(message)))
				},
				RenderCachedHit: func(output io.Writer, isAuto bool) {
					if isAuto {
						return
					}
					diagDispatcher.Enqueue([]string{"[NeoCode Diagnosis] using cached diagnosis result"})
				},
			},
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
				writeProxyLine(synchronizedStderr, "[ ! ] Auto diagnosis is downgraded because Windows shell integration is unavailable. Use `neocode diag` or `neocode diag -i` manually.")
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
		writeProxyLine(synchronizedStderr, "[ x ] Auto diagnosis failed, NeoCode proxy will exit and return to the native shell.")
		writeProxyf(synchronizedStderr, "[ reason: %s ]\n", strings.TrimSpace(diagnoseErr.Error()))
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

	printProxyExitedBanner(synchronizedStderr)
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

// windowsCmdShellIntegrationCommand 为 cmd 设置 UTF-8 与稳定提示符，避免注入异常控制序列破坏光标状态。
func windowsCmdShellIntegrationCommand() string {
	return `chcp 65001>nul & prompt $P$G`
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

// windowsDiagnosisDispatcher 负责在 Windows 上缓存诊断文案并在安全时机写入 ConPTY 屏幕缓冲区。
type windowsDiagnosisDispatcher struct {
	mu          sync.Mutex
	shellPath   string
	screenWrite func(string) error
	screenDead  bool
	ptyWriter   io.Writer
	tracker     *commandTracker
	errWriter   io.Writer
	promptReady bool
	pending     []windowsDiagnosisPayload
}

// windowsDiagnosisPayload 描述一次待注入的诊断展示内容。
type windowsDiagnosisPayload struct {
	Lines []string
}

// newWindowsDiagnosisDispatcher 创建 Windows 诊断注入调度器。
func newWindowsDiagnosisDispatcher(
	shellPath string,
	screenWrite func(string) error,
	ptyWriter io.Writer,
	tracker *commandTracker,
	errWriter io.Writer,
) *windowsDiagnosisDispatcher {
	return &windowsDiagnosisDispatcher{
		shellPath:   strings.TrimSpace(shellPath),
		screenWrite: screenWrite,
		ptyWriter:   ptyWriter,
		tracker:     tracker,
		errWriter:   errWriter,
		pending:     make([]windowsDiagnosisPayload, 0, 4),
	}
}

// Enqueue 提交一条诊断消息；若当前处于安全时机则立即注入，否则等待下一次 prompt_ready。
func (d *windowsDiagnosisDispatcher) Enqueue(lines []string) {
	if d == nil {
		return
	}
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		if text == "" {
			continue
		}
		normalized = append(normalized, text)
	}
	if len(normalized) == 0 {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending = append(d.pending, windowsDiagnosisPayload{
		Lines: normalized,
	})
	d.flushLocked()
}

// MarkPromptReady 更新 shell prompt 状态，并在 prompt 就绪时尝试冲刷待注入诊断消息。
func (d *windowsDiagnosisDispatcher) MarkPromptReady(ready bool) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.promptReady = ready
	if ready {
		d.flushLocked()
	}
}

// flushLocked 在互斥锁保护下把可注入的诊断消息写入 ConPTY 屏幕并触发新提示符。
func (d *windowsDiagnosisDispatcher) flushLocked() {
	if d == nil || d.ptyWriter == nil || !d.promptReady || len(d.pending) == 0 {
		return
	}
	if d.tracker != nil && d.tracker.CurrentLine() != "" {
		return
	}

	var blocks []string
	for _, item := range d.pending {
		block := buildWindowsDiagnosisScreenBlock(item.Lines)
		if strings.TrimSpace(block) == "" {
			continue
		}
		blocks = append(blocks, block)
	}
	d.pending = d.pending[:0]
	if len(blocks) == 0 {
		return
	}

	payload := strings.Join(blocks, "\n")
	if d.screenWrite != nil && !d.screenDead {
		if err := d.screenWrite(payload); err == nil {
			_, _ = io.WriteString(d.ptyWriter, "\r\n")
			return
		}
		d.screenDead = true
		if d.errWriter != nil {
			writeProxyf(d.errWriter, "neocode diag: conpty screen write unavailable, fallback to safe shell print\n")
		}
	}
	command := buildWindowsDiagnosisPrintCommand(d.shellPath, payload)
	if strings.TrimSpace(command) == "" {
		return
	}
	_, _ = io.WriteString(d.ptyWriter, command+"\r\n")
}

// buildWindowsDiagnosisScreenBlock 构造诊断文案块，统一使用 LF 避免 CRLF 叠加导致回车错位。
func buildWindowsDiagnosisScreenBlock(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		if text == "" {
			continue
		}
		normalized = append(normalized, text)
	}
	if len(normalized) == 0 {
		return ""
	}
	return "\n" + strings.Join(normalized, "\n") + "\n"
}

// buildWindowsDiagnosisPrintCommand 构造只负责打印文本的安全命令，避免把诊断正文当作命令执行。
func buildWindowsDiagnosisPrintCommand(shellPath string, payload string) string {
	trimmedPayload := strings.TrimSpace(payload)
	if trimmedPayload == "" {
		return ""
	}
	normalizedPayload := strings.ReplaceAll(payload, "\r\n", "\n")
	normalizedPayload = strings.ReplaceAll(normalizedPayload, "\r", "\n")
	if !strings.HasSuffix(normalizedPayload, "\n") {
		normalizedPayload += "\n"
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(normalizedPayload))
	if isWindowsCmdShell(shellPath) {
		// cmd 中统一调用 PowerShell 打印 Base64 解码文本，避免 cmd 元字符转义复杂度。
		return fmt.Sprintf(
			`powershell -NoLogo -NoProfile -Command "$t=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s')); [Console]::Out.Write(($t -replace '\n', [Environment]::NewLine))"`,
			encoded,
		)
	}
	// PowerShell / pwsh 直接在当前 shell 解码输出。
	return fmt.Sprintf(
		`$t=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s')); [Console]::Out.Write(($t -replace '\n', [Environment]::NewLine))`,
		encoded,
	)
}

// buildDiagnosisInitialFeedbackLines 构造首响提示文案，保持与既有终端文案一致。
func buildDiagnosisInitialFeedbackLines(prepared preparedDiagnosisRequest, isAuto bool) ([]string, bool) {
	if !IsDiagFastResponseEnabledFromEnv() {
		return nil, false
	}
	hint, ok := buildDiagnosisQuickHint(prepared)
	if isAuto && !ok {
		return nil, false
	}
	if isAuto {
		lines := []string{"[NeoCode Diagnosis] 快速预判（低置信度，完整诊断稍后返回）"}
		if !ok {
			return lines, true
		}
		lines = append(lines,
			fmt.Sprintf("置信度: %.2f", hint.Confidence),
			"可能根因: "+strings.TrimSpace(hint.RootCause),
		)
		if len(hint.InvestigationCommands) > 0 {
			lines = append(lines, "建议先查:")
			for _, command := range hint.InvestigationCommands {
				lines = append(lines, "- "+strings.TrimSpace(command))
			}
		}
		return lines, true
	}

	lines := []string{"[NeoCode Diagnosis] 正在诊断，完整结果稍后返回。"}
	if !ok {
		return lines, true
	}
	lines = append(lines,
		"快速预判（低置信度）：",
		fmt.Sprintf("置信度: %.2f", hint.Confidence),
		"可能根因: "+strings.TrimSpace(hint.RootCause),
	)
	if len(hint.InvestigationCommands) > 0 {
		lines = append(lines, "建议先查:")
		for _, command := range hint.InvestigationCommands {
			lines = append(lines, "- "+strings.TrimSpace(command))
		}
	}
	return lines, true
}

// buildDiagnosisResultLines 构造最终诊断文案，解析成功时输出结构化字段，失败时输出原始文本。
func buildDiagnosisResultLines(content string) []string {
	lines := []string{"[NeoCode Diagnosis]"}
	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		return append(lines, "- no diagnosis output")
	}
	var parsed diagnoseToolResult
	if err := json.Unmarshal([]byte(trimmedContent), &parsed); err != nil || strings.TrimSpace(parsed.RootCause) == "" {
		return append(lines, trimmedContent)
	}
	lines = append(lines,
		fmt.Sprintf("confidence: %.2f", parsed.Confidence),
		"root cause: "+strings.TrimSpace(parsed.RootCause),
	)
	if len(parsed.InvestigationCommands) > 0 {
		lines = append(lines, "investigation commands:")
		for _, command := range parsed.InvestigationCommands {
			lines = append(lines, "- "+strings.TrimSpace(command))
		}
	}
	if len(parsed.FixCommands) > 0 {
		lines = append(lines, "fix commands:")
		for _, command := range parsed.FixCommands {
			lines = append(lines, "- "+strings.TrimSpace(command))
		}
	}
	return lines
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
func bindWindowsShellRoleStreamWithCallerLegacy(
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
func shouldFallbackWindowsBindStreamStateLegacy(err error) bool {
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
func validateWindowsBindStreamAckFrameLegacy(ack gateway.MessageFrame) error {
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
func demuxGatewayNotificationsLegacy(
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
func forwardGatewayNotificationLegacy(
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
	diagDispatcher *windowsDiagnosisDispatcher,
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
					if diagDispatcher != nil {
						diagDispatcher.MarkPromptReady(true)
					}
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
					if diagDispatcher != nil {
						diagDispatcher.MarkPromptReady(false)
					}
					collectingCommand = true
					commandLogBuffer = NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)
					fallbackCommandBuffer.Reset()
				case ShellEventCommandDone:
					if diagDispatcher != nil {
						diagDispatcher.MarkPromptReady(false)
					}
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
				normalizedInput := normalizeWindowsConPTYInputByte(item)
				if idm != nil && idm.IsActive() {
					if idm.ShouldPassthroughInput() {
						if tracker != nil {
							tracker.Observe([]byte{normalizedInput})
						}
						_, _ = ptyWriter.Write([]byte{normalizedInput})
						continue
					}
					idm.HandleInputByte(item)
					continue
				}
				if tracker != nil {
					tracker.Observe([]byte{normalizedInput})
				}
				_, _ = ptyWriter.Write([]byte{normalizedInput})
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
		return sanitizeWindowsOutputConsoleMode(mode)
	}, nil)
	applyMode(os.Stderr, func(mode uint32) uint32 {
		return sanitizeWindowsOutputConsoleMode(mode)
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

// sanitizeWindowsInputConsoleMode 关闭 VT 输入与本地行回显，使键盘输入仅由 ConPTY 子 shell 负责回显与编辑。
func sanitizeWindowsInputConsoleMode(mode uint32) uint32 {
	return mode &^ (windowsEnableVirtualTerminalInput | windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT)
}

// normalizeWindowsConPTYInputByte 统一 Windows 输入字节到 ConPTY 兼容形式。
// 在部分 ConPTY 版本中，0x08 可能被解释为按词删除路径，这里转换为 0x7F 避免该问题。
func normalizeWindowsConPTYInputByte(inputByte byte) byte {
	if inputByte == 0x08 {
		return 0x7F
	}
	return inputByte
}

// sanitizeWindowsOutputConsoleMode 为代理输出启用稳定的换行滚屏语义与 VT 渲染。
func sanitizeWindowsOutputConsoleMode(mode uint32) uint32 {
	return mode | windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_WRAP_AT_EOL_OUTPUT | windowsEnableVirtualTerminalProcessing
}

// consumeDiagSignals 消费手动与自动诊断任务，复用统一诊断协调器避免重复请求。
func consumeDiagSignalsLegacy(
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
					diagnoseErr := runSingleDiagnosisWithCoordinatorLegacy(
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
					if diagnoseErr != nil && onAutoDiagnoseFailure != nil && shouldTerminateShellOnAutoDiagnoseErrorLegacy(diagnoseErr) {
						onAutoDiagnoseFailure(diagnoseErr)
					}
				}(job)
				continue
			}
			_ = runSingleDiagnosisWithCoordinatorLegacy(
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
func shouldTerminateShellOnAutoDiagnoseErrorLegacy(err error) bool {
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
func runSingleDiagnosisWithCoordinatorLegacy(
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

func decodeGatewayNotificationPayloadLegacy(raw json.RawMessage) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

func readMapStringLegacy(container map[string]any, key string) string {
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
	withDiagnosisCursorGuard(output, func() {
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
	})
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
