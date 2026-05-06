//go:build !windows

package ptyproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/tools"
)

const (
	diagnoseCallTimeout     = 90 * time.Second
	autoDiagnoseCallTimeout = 60 * time.Second
	diagSocketReadDeadline  = 3 * time.Second
	autoProbeTimeout        = 1500 * time.Millisecond

	proxyInitializedBanner = "[ NeoCode Proxy initialized ]"
	proxyExitedBanner      = "[ NeoCode Proxy exited ]"
)

var (
	hostTerminalInput = os.Stdin
	isTerminalFD      = term.IsTerminal
	makeRawTerminal   = term.MakeRaw
	restoreTerminal   = term.Restore

	proxyOutputLineEndingNormalizer = strings.NewReplacer(
		"\r\n", "\r\n",
		"\r", "\r\n",
		"\n", "\r\n",
	)
)

type diagnoseToolArgs struct {
	ErrorLog    string            `json:"error_log"`
	OSEnv       map[string]string `json:"os_env"`
	CommandText string            `json:"command_text"`
	ExitCode    int               `json:"exit_code"`
}

type diagnoseToolResult struct {
	Confidence            float64  `json:"confidence"`
	RootCause             string   `json:"root_cause"`
	FixCommands           []string `json:"fix_commands"`
	InvestigationCommands []string `json:"investigation_commands"`
}

type diagnoseTrigger struct {
	CommandText string
	ExitCode    int
	OutputText  string
}

type diagnoseJob struct {
	Trigger diagnoseTrigger
	Done    chan diagIPCResponse
	IsAuto  bool
}

type autoRuntimeState struct {
	Enabled  atomic.Bool
	OSCReady atomic.Bool
}

type commandTracker struct {
	mu          sync.Mutex
	lineBuffer  []byte
	lastCommand string
	escapeState uint8
}

// RunManualShell 负责 RunManualShell 相关逻辑。
func RunManualShell(ctx context.Context, options ManualShellOptions) error {
	normalized, err := NormalizeShellOptions(options)
	if err != nil {
		return err
	}

	shellPath := resolveShellPath(normalized.Shell)
	if shellPath == "" {
		return errors.New("ptyproxy: shell executable is empty")
	}

	listener, socketPath, err := listenDiagSocket(normalized.SocketPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()
	idmListener, idmSocketPath, err := listenIDMSocket(socketPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = idmListener.Close()
		_ = os.Remove(idmSocketPath)
	}()

	// 兜底恢复：即使后续流程异常退出，也尽量恢复宿主终端状态。
	restoreGuard := installHostTerminalRestoreGuard()
	defer restoreGuard()

	restoreRawTerminal, err := enableHostTerminalRawMode()
	if err != nil {
		return err
	}
	defer func() {
		_ = restoreRawTerminal()
	}()

	command, cleanupRC := buildShellCommand(shellPath, normalized, socketPath, idmSocketPath)
	defer cleanupRC()
	ptyFile, err := pty.Start(command)
	if err != nil {
		return fmt.Errorf("ptyproxy: start pty shell: %w", err)
	}
	defer func() {
		_ = ptyFile.Close()
	}()

	if err := pty.InheritSize(os.Stdin, ptyFile); err != nil && normalized.Stderr != nil {
		writeProxyf(normalized.Stderr, "neocode shell: inherit terminal size failed: %v\n", err)
	}
	stopResizeWatcher := watchPTYWindowResize(normalized.Stderr, ptyFile)
	defer stopResizeWatcher()

	var outputMu sync.Mutex
	synchronizedOutput := &serializedWriter{writer: normalized.Stdout, lock: &outputMu}
	printProxyInitializedBanner(synchronizedOutput)
	printWelcomeBanner(synchronizedOutput)

	// 预建 Gateway RPC 长连接：若 gateway 已运行则复用，否则自动拉起。
	gwRPCClient, gwErr := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress:       normalized.GatewayListenAddress,
		TokenFile:           normalized.GatewayTokenFile,
		DisableHeartbeatLog: true,
	})
	gatewayReady := false
	if gwErr != nil {
		writeProxyf(normalized.Stderr, "neocode shell: gateway client init failed: %v\n", gwErr)
	} else {
		authCtx, authCancel := context.WithTimeout(context.Background(), diagnoseCallTimeout)
		if authErr := gwRPCClient.Authenticate(authCtx); authErr != nil {
			writeProxyf(normalized.Stderr, "neocode shell: gateway auth failed: %v\n", authErr)
		} else {
			gatewayReady = true
		}
		authCancel()
	}
	if gwRPCClient != nil {
		defer gwRPCClient.Close()
	}

	if skillErr := ensureTerminalDiagnosisSkillFile(); skillErr != nil {
		writeProxyf(normalized.Stderr, "neocode shell: prepare terminal diagnosis skill failed: %v\n", skillErr)
	}
	if gatewayReady {
		cleanupZombieIDMSessions(gwRPCClient, normalized.Stderr)
	}

	logBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity)
	outputSink := io.MultiWriter(synchronizedOutput, logBuffer)
	commandLogBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)

	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)
	autoState.OSCReady.Store(false)

	printAutoModeBanner(synchronizedOutput, autoState)

	idm := newIDMController(idmControllerOptions{
		PTYWriter:  ptyFile,
		Output:     synchronizedOutput,
		Stderr:     normalized.Stderr,
		RPCClient:  gwRPCClient,
		AutoState:  autoState,
		LogBuffer:  logBuffer,
		DefaultCap: DefaultRingBufferCapacity,
		Workdir:    normalized.Workdir,
	})
	stopSignalForwarder := watchForwardSignals(command.Process, normalized.Stderr, idm.HandleSignal)
	defer stopSignalForwarder()

	diagnoseJobCh := make(chan diagnoseJob, 4)
	acceptCtx, cancelAccept := context.WithCancel(context.Background())
	var acceptWG sync.WaitGroup
	acceptWG.Add(2)
	go func() {
		defer acceptWG.Done()
		serveDiagSocket(acceptCtx, listener, diagnoseJobCh, autoState, normalized.Stderr)
	}()
	go func() {
		defer acceptWG.Done()
		serveIDMSocket(acceptCtx, idmListener, idm, normalized.Stderr)
	}()

	diagCtx, cancelDiag := context.WithCancel(context.Background())
	var diagWG sync.WaitGroup
	diagWG.Add(1)
	autoDiagFatalCh := make(chan error, 1)
	go func() {
		defer diagWG.Done()
		consumeDiagSignals(
			diagCtx,
			gwRPCClient,
			diagnoseJobCh,
			synchronizedOutput,
			logBuffer,
			normalized,
			socketPath,
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
		)
	}()

	inputTracker := &commandTracker{}
	inputCtx, cancelInput := context.WithCancel(context.Background())
	go func() {
		pumpProxyInput(inputCtx, normalized.Stdin, ptyFile, inputTracker, idm)
	}()

	autoTriggerCh := make(chan diagnoseTrigger, 2)
	go func() {
		probeTimer := time.NewTimer(autoProbeTimeout)
		defer probeTimer.Stop()
		<-probeTimer.C
		if !autoState.OSCReady.Load() {
			autoState.Enabled.Store(false)
			writeProxyf(normalized.Stderr, "neocode shell: OSC133 probe timed out, auto diagnosis downgraded\n")
			writeProxyLine(
				synchronizedOutput,
				"[ ⚠ 自动诊断已降级不可用：未检测到 shell OSC133 事件。请继续使用 `neocode diag` / `neocode diag -i`。 ]",
			)
		}
	}()

	var streamWG sync.WaitGroup
	streamWG.Add(1)
	go func() {
		defer streamWG.Done()
		streamPTYOutputWithIDM(ptyFile, outputSink, commandLogBuffer, inputTracker, autoTriggerCh, autoState, idm)
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
		waitDone <- command.Wait()
	}()

	var waitErr error
	forcedByAutoDiagFailure := false
	select {
	case <-ctx.Done():
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		waitErr = <-waitDone
	case diagnoseErr := <-autoDiagFatalCh:
		forcedByAutoDiagFailure = true
		writeProxyLine(synchronizedOutput, "[ ❌ 自动诊断调用失败，NeoCode 代理将退出并恢复系统原生 Shell。 ]")
		writeProxyf(synchronizedOutput, "[ 失败原因: %s ]\n", strings.TrimSpace(diagnoseErr.Error()))
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
			time.Sleep(200 * time.Millisecond)
			_ = command.Process.Kill()
		}
		waitErr = <-waitDone
	case waitErr = <-waitDone:
	}

	printProxyExitedBanner(synchronizedOutput)
	_ = ptyFile.Close()
	idm.Exit()

	cancelAccept()
	_ = listener.Close()
	_ = idmListener.Close()
	acceptWG.Wait()

	cancelInput()

	cancelDiag()
	streamWG.Wait()
	close(autoTriggerCh)
	triggerWG.Wait()
	diagWG.Wait()

	if waitErr != nil {
		if forcedByAutoDiagFailure {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return fmt.Errorf("ptyproxy: shell exited with code %d", status.ExitStatus())
			}
		}
		return waitErr
	}
	return nil
}

// printProxyInitializedBanner 负责 printProxyInitializedBanner 相关逻辑。
func printProxyInitializedBanner(writer io.Writer) {
	if writer == nil {
		return
	}
	writeProxyLine(writer, proxyInitializedBanner)
}

// printWelcomeBanner 负责 printWelcomeBanner 相关逻辑。
func printWelcomeBanner(writer io.Writer) {
	if writer == nil {
		return
	}
	lines := []string{
		"[ 💡 欢迎使用终端诊断代理！您可以像往常一样在终端里工作。 ]",
		"[ 常用指引: ]",
		"[ - 自动诊断: 报错时原位自动输出解析。开关控制: `neocode diag auto off` / `neocode diag auto on` ]",
		"[ - 手动诊断: 报错后输入 `neocode diag` 随时分析。 ]",
		"[ - 沙盒排查: 输入 `neocode diag -i` 进入 IDM 模式，排查完毕后输入 `exit` 退出沙盒。 ]",
		"[ - 帮助手册: 输入 `neocode -h` 查看所有命令与配置项。 ]",
		"[ - 退出代理: 输入 `exit` 或按 `Ctrl+D` 退出 NeoCode 代理外壳，回到系统原生 Shell。 ]",
	}
	for _, line := range lines {
		writeProxyLine(writer, line)
	}
}

// printAutoModeBanner 负责 printAutoModeBanner 相关逻辑。
func printAutoModeBanner(writer io.Writer, autoState *autoRuntimeState) {
	if writer == nil {
		return
	}
	if autoState != nil && autoState.Enabled.Load() {
		writeProxyLine(writer, "[ ✅ 自动诊断模式已开启，执行命令出错时将自动分析根因。 ]")
	} else {
		writeProxyLine(writer, "[ ⚠ 自动诊断模式未开启，报错后请手动输入 `neocode diag` 进行分析。 ]")
	}
}

// printProxyExitedBanner 负责 printProxyExitedBanner 相关逻辑。
func printProxyExitedBanner(writer io.Writer) {
	if writer == nil {
		return
	}
	_, _ = fmt.Fprint(writer, "\r\n[ NeoCode Proxy exited ]\r\n")
}

// enableHostTerminalRawMode 负责 enableHostTerminalRawMode 相关逻辑。
func enableHostTerminalRawMode() (func() error, error) {
	if hostTerminalInput == nil {
		return func() error { return nil }, nil
	}

	fd := int(hostTerminalInput.Fd())
	if !isTerminalFD(fd) {
		return func() error { return nil }, nil
	}

	originalState, err := makeRawTerminal(fd)
	if err != nil {
		return nil, fmt.Errorf("ptyproxy: set host terminal raw mode: %w", err)
	}
	return func() error {
		if restoreErr := restoreTerminal(fd, originalState); restoreErr != nil {
			return fmt.Errorf("ptyproxy: restore host terminal state: %w", restoreErr)
		}
		return nil
	}, nil
}

// installHostTerminalRestoreGuard 负责 installHostTerminalRestoreGuard 相关逻辑。
func installHostTerminalRestoreGuard() func() {
	if hostTerminalInput == nil {
		return func() {}
	}
	fd := int(hostTerminalInput.Fd())
	if !isTerminalFD(fd) {
		return func() {}
	}
	state, err := term.GetState(fd)
	if err != nil {
		return func() {}
	}
	return func() {
		_ = restoreTerminal(fd, state)
	}
}

// syncPTYWindowSize 负责 syncPTYWindowSize 相关逻辑。
func syncPTYWindowSize(errWriter io.Writer, ptyFile *os.File) {
	if ptyFile == nil {
		return
	}
	if err := pty.InheritSize(os.Stdin, ptyFile); err != nil && errWriter != nil {
		writeProxyf(errWriter, "neocode shell: inherit terminal size failed: %v\n", err)
	}
}

// watchPTYWindowResize 负责 watchPTYWindowResize 相关逻辑。
func watchPTYWindowResize(errWriter io.Writer, ptyFile *os.File) func() {
	if ptyFile == nil {
		return func() {}
	}

	winchSignals := make(chan os.Signal, 1)
	signal.Notify(winchSignals, syscall.SIGWINCH)

	stopCh := make(chan struct{})
	var stopOnce sync.Once
	var watcherWG sync.WaitGroup
	watcherWG.Add(1)
	go func() {
		defer watcherWG.Done()
		for {
			select {
			case <-stopCh:
				return
			case _, ok := <-winchSignals:
				if !ok {
					return
				}
				syncPTYWindowSize(errWriter, ptyFile)
			}
		}
	}()

	return func() {
		stopOnce.Do(func() {
			signal.Stop(winchSignals)
			close(stopCh)
			watcherWG.Wait()
		})
	}
}

// watchForwardSignals 负责 watchForwardSignals 相关逻辑。
func watchForwardSignals(process *os.Process, errWriter io.Writer, interceptor func(os.Signal) bool) func() {
	if process == nil {
		return func() {}
	}
	proxySignals := make(chan os.Signal, 1)
	signal.Notify(proxySignals, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTSTP, syscall.SIGCONT)
	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				return
			case signalValue, ok := <-proxySignals:
				if !ok {
					return
				}
				if interceptor != nil && interceptor(signalValue) {
					continue
				}
				sysSignal, ok := signalValue.(syscall.Signal)
				if !ok {
					continue
				}
				if process.Pid <= 0 {
					continue
				}
				if err := syscall.Kill(-process.Pid, sysSignal); err != nil && errWriter != nil {
					writeProxyf(errWriter, "neocode shell: forward signal %d failed: %v\n", sysSignal, err)
				}
			}
		}
	}()
	return func() {
		signal.Stop(proxySignals)
		close(stopCh)
		wg.Wait()
	}
}

// writeProxyText 负责 writeProxyText 相关逻辑。
func writeProxyText(writer io.Writer, text string) {
	if writer == nil || text == "" {
		return
	}
	_, _ = io.WriteString(writer, proxyOutputLineEndingNormalizer.Replace(text))
}

// writeProxyLine 负责 writeProxyLine 相关逻辑。
func writeProxyLine(writer io.Writer, text string) {
	writeProxyText(writer, text+"\n")
}

// writeProxyf 负责 writeProxyf 相关逻辑。
func writeProxyf(writer io.Writer, format string, args ...any) {
	writeProxyText(writer, fmt.Sprintf(format, args...))
}

// SendDiagnoseSignal 负责 SendDiagnoseSignal 相关逻辑。
func SendDiagnoseSignal(ctx context.Context, socketPath string) error {
	_, err := sendDiagIPCCommand(ctx, socketPath, diagIPCRequest{Cmd: diagCommandDiagnose})
	return err
}

// SendIDMEnterSignal 负责 SendIDMEnterSignal 相关逻辑。
func SendIDMEnterSignal(ctx context.Context, socketPath string) error {
	resolvedPath := filepath.Clean(strings.TrimSpace(socketPath))
	if resolvedPath == "." || strings.TrimSpace(resolvedPath) == "" {
		return errors.New("ptyproxy: idm socket path is empty")
	}
	response, err := sendDiagIPCCommandToPath(ctx, resolvedPath, diagIPCRequest{
		Cmd: diagCommandIDMEnter,
	})
	if err != nil {
		return err
	}
	if !response.OK {
		return errors.New("ptyproxy: idm enter rejected")
	}
	return nil
}

// SendAutoModeSignal 负责 SendAutoModeSignal 相关逻辑。
func SendAutoModeSignal(ctx context.Context, socketPath string, enabled bool) error {
	command := diagCommandAutoOff
	if enabled {
		command = diagCommandAutoOn
	}
	_, err := sendDiagIPCCommand(ctx, socketPath, diagIPCRequest{Cmd: command})
	return err
}

// QueryAutoMode 负责 QueryAutoMode 相关逻辑。
func QueryAutoMode(ctx context.Context, socketPath string) (bool, error) {
	response, err := sendDiagIPCCommand(ctx, socketPath, diagIPCRequest{Cmd: diagCommandAutoStatus})
	if err != nil {
		return false, err
	}
	return response.AutoEnabled, nil
}

// sendDiagIPCCommand 负责 sendDiagIPCCommand 相关逻辑。
func sendDiagIPCCommand(ctx context.Context, socketPath string, request diagIPCRequest) (diagIPCResponse, error) {
	primaryPath := filepath.Clean(strings.TrimSpace(socketPath))
	if primaryPath == "." || strings.TrimSpace(primaryPath) == "" {
		return diagIPCResponse{}, errors.New("ptyproxy: diagnose socket path is empty")
	}

	response, err := sendDiagIPCCommandToPath(ctx, primaryPath, request)
	if err == nil {
		return response, nil
	}
	if !strings.Contains(filepath.ToSlash(primaryPath), "/.neocode/run/") {
		return diagIPCResponse{}, err
	}

	legacyPath, fallbackErr := resolveValidatedLegacyDiagSocketPath(primaryPath)
	if fallbackErr != nil {
		return diagIPCResponse{}, err
	}
	if strings.EqualFold(filepath.Clean(legacyPath), primaryPath) {
		return diagIPCResponse{}, err
	}
	fallbackResponse, fallbackCallErr := sendDiagIPCCommandToPath(ctx, legacyPath, request)
	if fallbackCallErr != nil {
		return diagIPCResponse{}, err
	}
	_, _ = fmt.Fprintf(
		os.Stderr,
		"warning: diagnose socket fallback to legacy tmp path: %s (from %s)\n",
		strings.TrimSpace(legacyPath),
		strings.TrimSpace(primaryPath),
	)
	return fallbackResponse, nil
}

// resolveValidatedLegacyDiagSocketPath 负责 resolveValidatedLegacyDiagSocketPath 相关逻辑。
func resolveValidatedLegacyDiagSocketPath(primaryPath string) (string, error) {
	expectedPID, err := parseDiagSocketPIDFromPath(primaryPath)
	if err != nil {
		return "", err
	}
	legacyPath, err := ResolveLegacyTmpDiagSocketPathForPID(expectedPID)
	if err != nil {
		return "", err
	}
	legacyPID, err := parseDiagSocketPIDFromPath(legacyPath)
	if err != nil {
		return "", err
	}
	if legacyPID != expectedPID {
		return "", fmt.Errorf("ptyproxy: legacy diag socket pid mismatch, want %d got %d", expectedPID, legacyPID)
	}
	return legacyPath, nil
}

// sendDiagIPCCommandToPath 负责 sendDiagIPCCommandToPath 相关逻辑。
func sendDiagIPCCommandToPath(ctx context.Context, socketPath string, request diagIPCRequest) (diagIPCResponse, error) {
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, "unix", strings.TrimSpace(socketPath))
	if err != nil {
		return diagIPCResponse{}, fmt.Errorf("ptyproxy: connect diagnose socket: %w", err)
	}
	defer connection.Close()

	payload, err := json.Marshal(request)
	if err != nil {
		return diagIPCResponse{}, fmt.Errorf("ptyproxy: encode diag request: %w", err)
	}
	payload = append(payload, '\n')
	_ = connection.SetWriteDeadline(time.Now().Add(diagSocketReadDeadline))
	if _, err := connection.Write(payload); err != nil {
		return diagIPCResponse{}, fmt.Errorf("ptyproxy: send diag request: %w", err)
	}
	_ = connection.SetWriteDeadline(time.Time{})

	readDone := make(chan struct {
		response diagIPCResponse
		err      error
	}, 1)
	go func() {
		reader := bufio.NewReader(connection)
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			readDone <- struct {
				response diagIPCResponse
				err      error
			}{err: readErr}
			return
		}
		var response diagIPCResponse
		if unmarshalErr := json.Unmarshal(line, &response); unmarshalErr != nil {
			readDone <- struct {
				response diagIPCResponse
				err      error
			}{err: fmt.Errorf("decode diag response: %w", unmarshalErr)}
			return
		}
		if !response.OK {
			message := strings.TrimSpace(response.Message)
			if message == "" {
				message = "diagnose command rejected"
			}
			readDone <- struct {
				response diagIPCResponse
				err      error
			}{response: response, err: errors.New("ptyproxy: " + message)}
			return
		}
		readDone <- struct {
			response diagIPCResponse
			err      error
		}{response: response}
	}()

	select {
	case <-ctx.Done():
		_ = connection.SetReadDeadline(time.Now())
		<-readDone
		return diagIPCResponse{}, fmt.Errorf("ptyproxy: wait diagnose completion: %w", ctx.Err())
	case result := <-readDone:
		if result.err != nil && !isClosedNetworkError(result.err) {
			return diagIPCResponse{}, fmt.Errorf("ptyproxy: wait diagnose completion: %w", result.err)
		}
		return result.response, nil
	}
}

// buildShellCommand 负责 buildShellCommand 相关逻辑。
func buildShellCommand(shellPath string, options ManualShellOptions, socketPath string, idmSocketPath string) (*exec.Cmd, func()) {
	command := exec.Command(shellPath)
	command.Dir = options.Workdir
	command.Env = MergeEnvVar(os.Environ(), DiagSocketEnv, socketPath)
	command.Env = MergeEnvVar(command.Env, IDMDiagSocketEnv, idmSocketPath)

	cleanupTasks := make([]func(), 0, 2)
	cleanup := func() {
		for index := len(cleanupTasks) - 1; index >= 0; index-- {
			cleanupTasks[index]()
		}
	}
	if rcFile := prepareBashInitRC(shellPath); rcFile != "" {
		command.Args = append(command.Args, "--rcfile", rcFile)
		cleanupTasks = append(cleanupTasks, func() { deleteBashInitRCFile(rcFile) })
	}
	if zdotDir := prepareZshInitDir(shellPath); zdotDir != "" {
		command.Env = MergeEnvVar(command.Env, "ZDOTDIR", zdotDir)
		cleanupTasks = append(cleanupTasks, func() { deleteZshInitDir(zdotDir) })
	}
	return command, cleanup
}

// prepareBashInitRC 负责 prepareBashInitRC 相关逻辑。
var (
	createBashInitRCFile = defaultCreateBashInitRCFile
	deleteBashInitRCFile = defaultDeleteBashInitRCFile
	createZshInitDir     = defaultCreateZshInitDir
	deleteZshInitDir     = defaultDeleteZshInitDir
)

func prepareBashInitRC(shellPath string) string {
	if !isBashShell(shellPath) {
		return ""
	}
	path, err := createBashInitRCFile()
	if err != nil {
		return ""
	}
	return path
}

// prepareZshInitDir 负责 prepareZshInitDir 相关逻辑。
func prepareZshInitDir(shellPath string) string {
	if !isZshShell(shellPath) {
		return ""
	}
	path, err := createZshInitDir()
	if err != nil {
		return ""
	}
	return path
}

func isBashShell(shellPath string) bool {
	base := strings.ToLower(filepath.Base(shellPath))
	return base == "bash"
}

// isZshShell 负责 isZshShell 相关逻辑。
func isZshShell(shellPath string) bool {
	base := strings.ToLower(filepath.Base(shellPath))
	return base == "zsh"
}

func defaultCreateBashInitRCFile() (string, error) {
	content := `
# Load user's original bashrc to preserve custom prompt, aliases, etc.
if [ -f ~/.bashrc ]; then
	. ~/.bashrc
fi
` + shellInitScript + "\n"
	tmpFile, err := os.CreateTemp("", "neocode-bash-rc-*.sh")
	if err != nil {
		return "", fmt.Errorf("ptyproxy: create bash rc file: %w", err)
	}
	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("ptyproxy: write bash rc file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("ptyproxy: close bash rc file: %w", err)
	}
	return tmpFile.Name(), nil
}

func defaultDeleteBashInitRCFile(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// defaultCreateZshInitDir 负责 defaultCreateZshInitDir 相关逻辑。
func defaultCreateZshInitDir() (string, error) {
	content := `
# Load user's original zshrc to preserve custom prompt, aliases, etc.
if [ -f "${HOME}/.zshrc" ]; then
	. "${HOME}/.zshrc"
fi
` + shellInitScript + "\n"
	directory, err := os.MkdirTemp("", "neocode-zsh-*")
	if err != nil {
		return "", fmt.Errorf("ptyproxy: create zsh init directory: %w", err)
	}
	rcPath := filepath.Join(directory, ".zshrc")
	if writeErr := os.WriteFile(rcPath, []byte(content), 0o600); writeErr != nil {
		_ = os.RemoveAll(directory)
		return "", fmt.Errorf("ptyproxy: write zsh rc file: %w", writeErr)
	}
	return directory, nil
}

// defaultDeleteZshInitDir 负责 defaultDeleteZshInitDir 相关逻辑。
func defaultDeleteZshInitDir(path string) {
	if path != "" {
		_ = os.RemoveAll(path)
	}
}

// resolveShellPath 负责 resolveShellPath 相关逻辑。
func resolveShellPath(shellOption string) string {
	if trimmed := strings.TrimSpace(shellOption); trimmed != "" {
		return trimmed
	}
	if envShell := strings.TrimSpace(os.Getenv("SHELL")); envShell != "" {
		return envShell
	}
	return "/bin/bash"
}

// listenDiagSocket 负责 listenDiagSocket 相关逻辑。
func listenDiagSocket(socketOption string) (net.Listener, string, error) {
	socketPath := strings.TrimSpace(socketOption)
	if socketPath == "" {
		resolved, err := ResolveDefaultDiagSocketPath()
		if err != nil {
			return nil, "", err
		}
		socketPath = resolved
	}
	return listenSocketByPath(socketPath)
}

// listenIDMSocket 负责 listenIDMSocket 相关逻辑。
func listenIDMSocket(diagSocketPath string) (net.Listener, string, error) {
	if trimmedDiagPath := strings.TrimSpace(diagSocketPath); trimmedDiagPath != "" {
		derivedPath, err := DeriveIDMSocketPathFromDiagSocketPath(trimmedDiagPath)
		if err != nil {
			return nil, "", err
		}
		return listenSocketByPath(derivedPath)
	}

	socketPath, err := ResolveDefaultIDMDiagSocketPath()
	if err != nil {
		return nil, "", err
	}
	return listenSocketByPath(socketPath)
}

// listenSocketByPath 负责 listenSocketByPath 相关逻辑。
func listenSocketByPath(socketPath string) (net.Listener, string, error) {
	socketPath = filepath.Clean(socketPath)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, "", fmt.Errorf("ptyproxy: create socket directory: %w", err)
	}
	if err := cleanupStaleSocket(socketPath); err != nil {
		return nil, "", err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, "", fmt.Errorf("ptyproxy: listen diagnose socket: %w", err)
	}
	if chmodErr := os.Chmod(socketPath, 0o600); chmodErr != nil {
		_ = listener.Close()
		return nil, "", fmt.Errorf("ptyproxy: set diagnose socket permission: %w", chmodErr)
	}
	return listener, socketPath, nil
}

// cleanupStaleSocket 负责 cleanupStaleSocket 相关逻辑。
func cleanupStaleSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("ptyproxy: stat diagnose socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("ptyproxy: diagnose socket path exists and is not socket: %s", socketPath)
	}
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("ptyproxy: remove stale diagnose socket: %w", err)
	}
	return nil
}

// serveDiagSocket 负责 serveDiagSocket 相关逻辑。
func serveDiagSocket(
	ctx context.Context,
	listener net.Listener,
	jobCh chan<- diagnoseJob,
	autoState *autoRuntimeState,
	errWriter io.Writer,
) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || isClosedNetworkError(err) {
				return
			}
			if errWriter != nil {
				writeProxyf(errWriter, "neocode diag: accept signal error: %v\n", err)
			}
			continue
		}
		handleDiagSocketConnection(ctx, connection, jobCh, autoState)
	}
}

// handleDiagSocketConnection 负责 handleDiagSocketConnection 相关逻辑。
func handleDiagSocketConnection(
	ctx context.Context,
	connection net.Conn,
	jobCh chan<- diagnoseJob,
	autoState *autoRuntimeState,
) {
	if connection == nil {
		return
	}
	defer connection.Close()

	_ = connection.SetReadDeadline(time.Now().Add(diagSocketReadDeadline))
	reader := bufio.NewReader(io.LimitReader(connection, 8*1024))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		writeDiagIPCResponse(connection, diagIPCResponse{OK: false, Message: "read request failed"})
		return
	}
	_ = connection.SetReadDeadline(time.Time{})

	var request diagIPCRequest
	if unmarshalErr := json.Unmarshal(line, &request); unmarshalErr != nil {
		writeDiagIPCResponse(connection, diagIPCResponse{OK: false, Message: "invalid request"})
		return
	}

	switch normalizeDiagIPCCommand(request.Cmd) {
	case diagCommandDiagnose:
		done := make(chan diagIPCResponse, 1)
		job := diagnoseJob{Done: done, IsAuto: false}
		select {
		case <-ctx.Done():
			writeDiagIPCResponse(connection, diagIPCResponse{OK: false, Message: "proxy shutting down"})
			return
		case jobCh <- job:
		}
		select {
		case <-ctx.Done():
			writeDiagIPCResponse(connection, diagIPCResponse{OK: false, Message: "proxy shutting down"})
		case response := <-done:
			writeDiagIPCResponse(connection, response)
		}
	case diagCommandAutoOn:
		if autoState != nil && !autoState.OSCReady.Load() {
			autoState.Enabled.Store(false)
			writeDiagIPCResponse(connection, diagIPCResponse{
				OK:          true,
				AutoEnabled: false,
				Message:     "auto mode unavailable: shell osc133 is not ready",
			})
			return
		}
		autoState.Enabled.Store(true)
		writeDiagIPCResponse(connection, diagIPCResponse{OK: true, AutoEnabled: true, Message: "auto mode enabled"})
	case diagCommandAutoOff:
		autoState.Enabled.Store(false)
		writeDiagIPCResponse(connection, diagIPCResponse{OK: true, AutoEnabled: false, Message: "auto mode disabled"})
	case diagCommandAutoStatus:
		writeDiagIPCResponse(connection, diagIPCResponse{
			OK:          true,
			AutoEnabled: autoState.Enabled.Load(),
			Message:     "auto mode status",
		})
	default:
		writeDiagIPCResponse(connection, diagIPCResponse{OK: false, Message: "unsupported command"})
	}
}

// writeDiagIPCResponse 负责 writeDiagIPCResponse 相关逻辑。
func writeDiagIPCResponse(connection net.Conn, response diagIPCResponse) {
	if connection == nil {
		return
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		encoded = []byte(`{"ok":false,"message":"encode response failed"}`)
	}
	encoded = append(encoded, '\n')
	_, _ = connection.Write(encoded)
}

// consumeDiagSignals 负责 consumeDiagSignals 相关逻辑。
func consumeDiagSignals(
	ctx context.Context,
	rpcClient *gatewayclient.GatewayRPCClient,
	jobCh <-chan diagnoseJob,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	socketPath string,
	autoState *autoRuntimeState,
	onAutoDiagnoseFailure func(error),
) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobCh:
			if !ok {
				return
			}
			diagnoseErr := runSingleDiagnosis(rpcClient, output, buffer, options, socketPath, job.Trigger, job.IsAuto, autoState)
			if job.IsAuto && diagnoseErr != nil && onAutoDiagnoseFailure != nil && shouldTerminateShellOnAutoDiagnoseError(diagnoseErr) {
				onAutoDiagnoseFailure(diagnoseErr)
			}
			if job.Done != nil {
				job.Done <- diagIPCResponse{OK: true, AutoEnabled: autoState.Enabled.Load(), Message: "diagnosis completed"}
			}
		}
	}
}

// shouldTerminateShellOnAutoDiagnoseError 判断自动诊断失败后是否必须终止代理壳。
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

// runSingleDiagnosis 负责 runSingleDiagnosis 相关逻辑。
func runSingleDiagnosis(
	rpcClient *gatewayclient.GatewayRPCClient,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	socketPath string,
	trigger diagnoseTrigger,
	isAuto bool,
	autoState *autoRuntimeState,
) error {
	if output == nil {
		return nil
	}

	var (
		result tools.ToolResult
		err    error
	)
	if isAuto {
		result, err = callDiagnoseToolWithTimeout(
			rpcClient,
			buffer,
			options,
			socketPath,
			trigger,
			autoDiagnoseCallTimeout,
		)
	} else {
		result, err = callDiagnoseTool(rpcClient, buffer, options, socketPath, trigger)
	}
	if err != nil {
		writeProxyf(output, "\n\033[31m[NeoCode Diagnosis]\033[0m %s\n", strings.TrimSpace(err.Error()))
		return err
	}
	if isAuto && autoState != nil && !autoState.Enabled.Load() {
		return nil
	}
	renderDiagnosis(output, result.Content, result.IsError)
	return nil
}

// callDiagnoseTool 负责 callDiagnoseTool 相关逻辑。
func callDiagnoseTool(
	rpcClient *gatewayclient.GatewayRPCClient,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	socketPath string,
	trigger diagnoseTrigger,
) (tools.ToolResult, error) {
	return callDiagnoseToolWithTimeout(rpcClient, buffer, options, socketPath, trigger, diagnoseCallTimeout)
}

// callDiagnoseToolWithTimeout 负责 callDiagnoseToolWithTimeout 相关逻辑。
func callDiagnoseToolWithTimeout(
	rpcClient *gatewayclient.GatewayRPCClient,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	socketPath string,
	trigger diagnoseTrigger,
	timeout time.Duration,
) (tools.ToolResult, error) {
	if rpcClient == nil {
		return tools.ToolResult{}, errors.New("诊断服务未就绪，请确认 gateway 已连接后重试")
	}

	logSnapshot := buffer.SnapshotString()
	if strings.TrimSpace(trigger.OutputText) != "" {
		logSnapshot = trigger.OutputText
	}
	sanitizedErrorLog := SanitizeDiagnosisText(logSnapshot, defaultDiagnosisPayloadMaxBytes)
	if strings.TrimSpace(sanitizedErrorLog) == "" {
		sanitizedErrorLog = "no terminal output captured"
	}
	sanitizedCommand := SanitizeDiagnosisText(trigger.CommandText, 1024)

	requestArgs := diagnoseToolArgs{
		ErrorLog: sanitizedErrorLog,
		OSEnv: map[string]string{
			"os":     runtime.GOOS,
			"shell":  resolveShellPath(options.Shell),
			"cwd":    options.Workdir,
			"socket": socketPath,
		},
		CommandText: sanitizedCommand,
		ExitCode:    trigger.ExitCode,
	}

	requestPayload, err := json.Marshal(requestArgs)
	if err != nil {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: build diagnose payload failed: %v\n", err)
		}
		return tools.ToolResult{}, errors.New("诊断请求构建失败，请稍后重试")
	}

	callContext, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var frame gateway.MessageFrame
	if err := rpcClient.CallWithOptions(
		callContext,
		protocol.MethodGatewayExecuteSystemTool,
		protocol.ExecuteSystemToolParams{
			Workdir:   options.Workdir,
			ToolName:  tools.ToolNameDiagnose,
			Arguments: requestPayload,
		},
		&frame,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: timeout,
			Retries: 1,
		},
	); err != nil {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: executeSystemTool rpc failed: %v\n", err)
		}
		return tools.ToolResult{}, errors.New("诊断调用失败，请检查 gateway 连接后重试，或使用 `neocode diag -i` 继续排查")
	}

	if frame.Type == gateway.FrameTypeError && frame.Error != nil {
		if options.Stderr != nil {
			writeProxyf(
				options.Stderr,
				"neocode diag: gateway returned frame error code=%s message=%s\n",
				strings.TrimSpace(frame.Error.Code),
				strings.TrimSpace(frame.Error.Message),
			)
		}
		return tools.ToolResult{}, errors.New("诊断服务暂不可用，请稍后重试，或使用 `neocode diag -i` 继续排查")
	}
	if frame.Type != gateway.FrameTypeAck {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: unexpected gateway frame type: %s\n", frame.Type)
		}
		return tools.ToolResult{}, errors.New("诊断服务返回异常响应，请稍后重试")
	}

	toolResult, err := decodeToolResult(frame.Payload)
	if err != nil {
		if options.Stderr != nil {
			writeProxyf(options.Stderr, "neocode diag: decode diagnose payload failed: %v\n", err)
		}
		return tools.ToolResult{}, errors.New("诊断结果解析失败，请重试或更新 NeoCode")
	}
	return toolResult, nil
}

// decodeToolResult 负责 decodeToolResult 相关逻辑。
func decodeToolResult(payload any) (tools.ToolResult, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("encode tool payload: %w", err)
	}
	var result tools.ToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return tools.ToolResult{}, fmt.Errorf("decode tool payload: %w", err)
	}
	return result, nil
}

// renderDiagnosis 负责 renderDiagnosis 相关逻辑。
func renderDiagnosis(output io.Writer, content string, isError bool) {
	headerColor := "\033[36m"
	if isError {
		headerColor = "\033[31m"
	}
	writeProxyf(output, "\n%s[NeoCode Diagnosis]\033[0m\n", headerColor)

	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		writeProxyLine(output, "- 无可用诊断内容。")
		return
	}

	var parsed diagnoseToolResult
	if err := json.Unmarshal([]byte(trimmedContent), &parsed); err != nil || strings.TrimSpace(parsed.RootCause) == "" {
		writeProxyLine(output, trimmedContent)
		return
	}

	writeProxyf(output, "置信度: %.2f\n", parsed.Confidence)
	writeProxyf(output, "根因: %s\n", strings.TrimSpace(parsed.RootCause))
	if len(parsed.InvestigationCommands) > 0 {
		writeProxyLine(output, "建议排查命令:")
		for _, command := range parsed.InvestigationCommands {
			writeProxyf(output, "- %s\n", strings.TrimSpace(command))
		}
	}
	if len(parsed.FixCommands) > 0 {
		writeProxyLine(output, "建议修复命令:")
		for _, command := range parsed.FixCommands {
			writeProxyf(output, "- %s\n", strings.TrimSpace(command))
		}
	}
}

// streamPTYOutput 负责 streamPTYOutput 相关逻辑。
func streamPTYOutput(
	ptyReader io.Reader,
	outputSink io.Writer,
	commandLogBuffer *UTF8RingBuffer,
	tracker *commandTracker,
	autoTriggerCh chan<- diagnoseTrigger,
	autoState *autoRuntimeState,
) {
	streamPTYOutputWithIDM(ptyReader, outputSink, commandLogBuffer, tracker, autoTriggerCh, autoState, nil)
}

// streamPTYOutputWithIDM 负责 streamPTYOutputWithIDM 相关逻辑。
func streamPTYOutputWithIDM(
	ptyReader io.Reader,
	outputSink io.Writer,
	commandLogBuffer *UTF8RingBuffer,
	tracker *commandTracker,
	autoTriggerCh chan<- diagnoseTrigger,
	autoState *autoRuntimeState,
	idm *idmController,
) {
	if ptyReader == nil || outputSink == nil || commandLogBuffer == nil {
		return
	}
	parser := &OSC133Parser{}
	collectingCommand := false
	pendingTrigger := (*diagnoseTrigger)(nil)
	fallbackCommandBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)

	buffer := make([]byte, 4096)
	for {
		readBytes, err := ptyReader.Read(buffer)
		if readBytes > 0 {
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
					autoState.OSCReady.Store(true)
					if pendingTrigger != nil && autoState.Enabled.Load() {
						select {
						case autoTriggerCh <- *pendingTrigger:
						default:
							// 通道拥塞时直接丢弃本次触发，避免阻塞 PTY 输出主循环。
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
					if ShouldTriggerAutoDiagnosis(event.ExitCode, commandText, outputText) {
						pendingTrigger = &diagnoseTrigger{
							CommandText: commandText,
							ExitCode:    event.ExitCode,
							OutputText:  outputText,
						}
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// pumpProxyInput 负责 pumpProxyInput 相关逻辑。
func pumpProxyInput(
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

// copyInputWithTracker 负责 copyInputWithTracker 相关逻辑。
func copyInputWithTracker(dst io.Writer, src io.Reader, tracker *commandTracker) (int64, error) {
	if dst == nil || src == nil {
		return 0, nil
	}
	written := int64(0)
	buffer := make([]byte, 4096)
	for {
		n, err := src.Read(buffer)
		if n > 0 {
			payload := buffer[:n]
			if tracker != nil {
				tracker.Observe(payload)
			}
			m, writeErr := dst.Write(payload)
			written += int64(m)
			if writeErr != nil {
				return written, writeErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return written, nil
			}
			return written, err
		}
	}
}

// Observe 负责 Observe 相关逻辑。
func (t *commandTracker) Observe(payload []byte) {
	if t == nil || len(payload) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range payload {
		switch t.escapeState {
		case 1:
			switch b {
			case '[':
				t.escapeState = 2
			case ']':
				t.escapeState = 3
			default:
				t.escapeState = 0
			}
			continue
		case 2:
			if b >= 0x40 && b <= 0x7e {
				t.escapeState = 0
			}
			continue
		case 3:
			if b == 0x07 {
				t.escapeState = 0
				continue
			}
			if b == 0x1b {
				t.escapeState = 4
				continue
			}
			continue
		case 4:
			if b == '\\' {
				t.escapeState = 0
				continue
			}
			t.escapeState = 3
			continue
		}

		switch b {
		case 0x1b:
			t.escapeState = 1
		case '\r', '\n':
			current := strings.TrimSpace(string(t.lineBuffer))
			if current != "" {
				t.lastCommand = current
			}
			t.lineBuffer = t.lineBuffer[:0]
		case 0x08, 0x7f:
			if len(t.lineBuffer) > 0 {
				t.lineBuffer = t.lineBuffer[:len(t.lineBuffer)-1]
			}
		default:
			if b >= 0x20 {
				t.lineBuffer = append(t.lineBuffer, b)
			}
		}
	}
}

// LastCommand 负责 LastCommand 相关逻辑。
func (t *commandTracker) LastCommand() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(t.lastCommand)
}

// isClosedNetworkError 负责 isClosedNetworkError 相关逻辑。
func isClosedNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(lower, "use of closed network connection")
}

// serializedWriter 负责 serializedWriter 相关逻辑。
type serializedWriter struct {
	writer io.Writer
	lock   *sync.Mutex
}

// Write 负责 Write 相关逻辑。
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
