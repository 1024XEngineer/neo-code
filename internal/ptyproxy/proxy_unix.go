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
	diagnoseCallTimeout    = 60 * time.Second
	diagSocketReadDeadline = 3 * time.Second
	autoProbeTimeout       = 1500 * time.Millisecond

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
}

// RunManualShell 启动 Phase2 终端代理，提供 Manual/Auto 诊断闭环与 OSC133 事件驱动能力。
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

	command, cleanupRC := buildShellCommand(shellPath, normalized, socketPath)
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

	stopSignalForwarder := watchForwardSignals(command.Process, normalized.Stderr)
	defer stopSignalForwarder()

	var outputMu sync.Mutex
	synchronizedOutput := &serializedWriter{writer: normalized.Stdout, lock: &outputMu}
	printProxyInitializedBanner(synchronizedOutput)
	printWelcomeBanner(synchronizedOutput)

	// 预建立 Gateway RPC 长连接：若 gateway 已运行则直接复用，否则自动拉起。
	gwRPCClient, gwErr := gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		ListenAddress: normalized.GatewayListenAddress,
		TokenFile:     normalized.GatewayTokenFile,
	})
	if gwErr != nil {
		writeProxyf(normalized.Stderr, "neocode shell: gateway client init failed: %v\n", gwErr)
	} else {
		authCtx, authCancel := context.WithTimeout(context.Background(), diagnoseCallTimeout)
		if authErr := gwRPCClient.Authenticate(authCtx); authErr != nil {
			writeProxyf(normalized.Stderr, "neocode shell: gateway auth failed: %v\n", authErr)
		}
		authCancel()
	}
	if gwRPCClient != nil {
		defer gwRPCClient.Close()
	}

	logBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity)
	outputSink := io.MultiWriter(synchronizedOutput, logBuffer)
	commandLogBuffer := NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)

	autoState := &autoRuntimeState{}
	autoState.Enabled.Store(true)
	autoState.OSCReady.Store(false)

	printAutoModeBanner(synchronizedOutput, autoState)

	diagnoseJobCh := make(chan diagnoseJob, 4)
	acceptCtx, cancelAccept := context.WithCancel(context.Background())
	var acceptWG sync.WaitGroup
	acceptWG.Add(1)
	go func() {
		defer acceptWG.Done()
		serveDiagSocket(acceptCtx, listener, diagnoseJobCh, autoState, normalized.Stderr)
	}()

	diagCtx, cancelDiag := context.WithCancel(context.Background())
	var diagWG sync.WaitGroup
	diagWG.Add(1)
	go func() {
		defer diagWG.Done()
		consumeDiagSignals(diagCtx, gwRPCClient, diagnoseJobCh, synchronizedOutput, logBuffer, normalized, socketPath, autoState)
	}()

	inputTracker := &commandTracker{}
	go func() {
		_, _ = copyInputWithTracker(ptyFile, normalized.Stdin, inputTracker)
	}()

	autoTriggerCh := make(chan diagnoseTrigger, 2)
	go func() {
		probeTimer := time.NewTimer(autoProbeTimeout)
		defer probeTimer.Stop()
		<-probeTimer.C
		if !autoState.OSCReady.Load() {
			writeProxyf(normalized.Stderr, "neocode shell: OSC133 probe timed out, fallback to manual mode\n")
		}
	}()

	var streamWG sync.WaitGroup
	streamWG.Add(1)
	go func() {
		defer streamWG.Done()
		streamPTYOutput(ptyFile, outputSink, commandLogBuffer, inputTracker, autoTriggerCh, autoState)
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
	select {
	case <-ctx.Done():
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		waitErr = <-waitDone
	case waitErr = <-waitDone:
	}

	printProxyExitedBanner(synchronizedOutput)
	_ = ptyFile.Close()

	cancelAccept()
	_ = listener.Close()
	acceptWG.Wait()

	cancelDiag()
	streamWG.Wait()
	close(autoTriggerCh)
	triggerWG.Wait()
	diagWG.Wait()

	if waitErr != nil {
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

// printProxyInitializedBanner 在 PTY 会话启动前输出代理已就绪提示。
func printProxyInitializedBanner(writer io.Writer) {
	if writer == nil {
		return
	}
	writeProxyLine(writer, proxyInitializedBanner)
}

// printWelcomeBanner 在 PTY 会话启动后输出指引与提示，帮助用户了解诊断功能的使用方式。
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

// printAutoModeBanner 根据当前 auto 诊断开关状态输出对应提示。
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

// printProxyExitedBanner 在 PTY 会话结束后输出代理退出提示。
func printProxyExitedBanner(writer io.Writer) {
	if writer == nil {
		return
	}
	_, _ = fmt.Fprint(writer, "\r\n[ NeoCode Proxy exited ]\r\n")
}

// enableHostTerminalRawMode 将宿主终端切换到原始模式，并返回可恢复终端状态的函数。
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

// installHostTerminalRestoreGuard 提前抓取终端状态并在退出时兜底恢复。
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

// syncPTYWindowSize 将宿主终端窗口尺寸同步到 PTY，避免默认 80 列导致的提示符错位。
func syncPTYWindowSize(errWriter io.Writer, ptyFile *os.File) {
	if ptyFile == nil {
		return
	}
	if err := pty.InheritSize(os.Stdin, ptyFile); err != nil && errWriter != nil {
		writeProxyf(errWriter, "neocode shell: inherit terminal size failed: %v\n", err)
	}
}

// watchPTYWindowResize 监听 SIGWINCH 并持续同步 PTY 尺寸，返回停止监听函数。
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

// watchForwardSignals 拦截关键信号并透传给 shell 进程组，确保作业控制语义一致。
func watchForwardSignals(process *os.Process, errWriter io.Writer) func() {
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

// writeProxyText 统一将代理自输出的换行规范化为 CRLF，适配 Raw Mode 下的终端显示。
func writeProxyText(writer io.Writer, text string) {
	if writer == nil || text == "" {
		return
	}
	_, _ = io.WriteString(writer, proxyOutputLineEndingNormalizer.Replace(text))
}

// writeProxyLine 向代理输出写入单行文本，并追加 CRLF 换行。
func writeProxyLine(writer io.Writer, text string) {
	writeProxyText(writer, text+"\n")
}

// writeProxyf 使用格式化字符串输出代理信息，并统一换行为 CRLF。
func writeProxyf(writer io.Writer, format string, args ...any) {
	writeProxyText(writer, fmt.Sprintf(format, args...))
}

// SendDiagnoseSignal 连接本地 socket 并触发一次手动诊断请求。
func SendDiagnoseSignal(ctx context.Context, socketPath string) error {
	_, err := sendDiagIPCCommand(ctx, socketPath, diagIPCRequest{Cmd: diagCommandDiagnose})
	return err
}

// SendAutoModeSignal 向代理 shell 发送 Auto 模式开关信令。
func SendAutoModeSignal(ctx context.Context, socketPath string, enabled bool) error {
	command := diagCommandAutoOff
	if enabled {
		command = diagCommandAutoOn
	}
	_, err := sendDiagIPCCommand(ctx, socketPath, diagIPCRequest{Cmd: command})
	return err
}

// QueryAutoMode 查询当前代理 shell 的 Auto 诊断模式开关状态。
func QueryAutoMode(ctx context.Context, socketPath string) (bool, error) {
	response, err := sendDiagIPCCommand(ctx, socketPath, diagIPCRequest{Cmd: diagCommandAutoStatus})
	if err != nil {
		return false, err
	}
	return response.AutoEnabled, nil
}

// sendDiagIPCCommand 通过 JSON-Lines 发送控制命令，并在必要时回退到旧版 tmp socket。
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

// resolveValidatedLegacyDiagSocketPath 基于主 socket 的 PID 选择并校验 legacy socket，避免误连其他会话。
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

// sendDiagIPCCommandToPath 在指定 socket 路径上执行一次 JSON-Lines 请求响应。
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

// buildShellCommand 构建真实 shell 进程，并在子进程环境中注入诊断 socket 变量。
// 对 bash 自动注入 OSC133 init 脚本（--rcfile），使 shell 集成立即可用。
// 返回的 cleanup 函数必须在调用方 defer 中执行，用于清理临时 RC 文件。
func buildShellCommand(shellPath string, options ManualShellOptions, socketPath string) (*exec.Cmd, func()) {
	command := exec.Command(shellPath)
	command.Dir = options.Workdir
	command.Env = MergeEnvVar(os.Environ(), DiagSocketEnv, socketPath)

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

// prepareBashInitRC 当 shell 为 bash 时创建包含 OSC133 init 脚本 + 用户 ~/.bashrc 的临时文件。
// 返回空字符串表示跳过（非 bash 或创建失败）。
// 调用方需确保在进程退出后用 deleteBashInitRC 清理。
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

// prepareZshInitDir 当 shell 为 zsh 时创建临时 ZDOTDIR 目录并注入 .zshrc。
// 返回空字符串表示跳过（非 zsh 或创建失败）。
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

// isZshShell 判断给定 shell 路径是否为 zsh。
func isZshShell(shellPath string) bool {
	base := strings.ToLower(filepath.Base(shellPath))
	return base == "zsh"
}

func defaultCreateBashInitRCFile() (string, error) {
	content := shellInitScript + `
# Load user's original bashrc to preserve custom prompt, aliases, etc.
if [ -f ~/.bashrc ]; then
	. ~/.bashrc
fi
`
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

// defaultCreateZshInitDir 创建临时 ZDOTDIR，并写入注入脚本到 .zshrc。
func defaultCreateZshInitDir() (string, error) {
	content := shellInitScript + `
# Load user's original zshrc to preserve custom prompt, aliases, etc.
if [ -f "${HOME}/.zshrc" ]; then
	. "${HOME}/.zshrc"
fi
`
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

// defaultDeleteZshInitDir 删除临时 ZDOTDIR 目录及其注入文件。
func defaultDeleteZshInitDir(path string) {
	if path != "" {
		_ = os.RemoveAll(path)
	}
}

// resolveShellPath 按“显式参数 -> SHELL 环境变量 -> /bin/bash”顺序选择实际 shell。
func resolveShellPath(shellOption string) string {
	if trimmed := strings.TrimSpace(shellOption); trimmed != "" {
		return trimmed
	}
	if envShell := strings.TrimSpace(os.Getenv("SHELL")); envShell != "" {
		return envShell
	}
	return "/bin/bash"
}

// listenDiagSocket 创建并监听 Unix socket；路径为空时使用 ~/.neocode/run 的统一地址。
func listenDiagSocket(socketOption string) (net.Listener, string, error) {
	socketPath := strings.TrimSpace(socketOption)
	if socketPath == "" {
		resolved, err := ResolveDefaultDiagSocketPath()
		if err != nil {
			return nil, "", err
		}
		socketPath = resolved
	}
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

// cleanupStaleSocket 删除历史遗留 socket，防止异常退出后下次监听失败。
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

// serveDiagSocket 接收 JSON-Lines 信令，转发手动诊断或即时切换 Auto 开关。
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

// handleDiagSocketConnection 处理单连接请求并返回单行 JSON 响应。
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

// writeDiagIPCResponse 写入单行 JSON 响应。
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

// consumeDiagSignals 串行消费诊断任务，复用预建立的 Gateway RPC 长连接。
func consumeDiagSignals(
	ctx context.Context,
	rpcClient *gatewayclient.GatewayRPCClient,
	jobCh <-chan diagnoseJob,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	socketPath string,
	autoState *autoRuntimeState,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobCh:
			if !ok {
				return
			}
			runSingleDiagnosis(rpcClient, output, buffer, options, socketPath, job.Trigger, job.IsAuto, autoState)
			if job.Done != nil {
				job.Done <- diagIPCResponse{OK: true, AutoEnabled: autoState.Enabled.Load(), Message: "diagnosis completed"}
			}
		}
	}
}

// runSingleDiagnosis 执行一次诊断调用，并根据 Auto 开关决定是否渲染结果。
func runSingleDiagnosis(
	rpcClient *gatewayclient.GatewayRPCClient,
	output io.Writer,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	socketPath string,
	trigger diagnoseTrigger,
	isAuto bool,
	autoState *autoRuntimeState,
) {
	if output == nil {
		return
	}

	result, err := callDiagnoseTool(rpcClient, buffer, options, socketPath, trigger)
	if err != nil {
		writeProxyf(output, "\n\033[31m[NeoCode Diagnosis]\033[0m %s\n", strings.TrimSpace(err.Error()))
		return
	}
	if isAuto && autoState != nil && !autoState.Enabled.Load() {
		return
	}
	renderDiagnosis(output, result.Content, result.IsError)
}

// callDiagnoseTool 使用 RunManualShell 预建立的 Gateway RPC 长连接调用 executeSystemTool(diagnose)。
func callDiagnoseTool(
	rpcClient *gatewayclient.GatewayRPCClient,
	buffer *UTF8RingBuffer,
	options ManualShellOptions,
	socketPath string,
	trigger diagnoseTrigger,
) (tools.ToolResult, error) {
	if rpcClient == nil {
		return tools.ToolResult{}, fmt.Errorf("网关客户端未就绪，请检查 gateway 服务状态")
	}

	logSnapshot := buffer.SnapshotString()
	if strings.TrimSpace(trigger.OutputText) != "" {
		logSnapshot = trigger.OutputText
	}

	requestArgs := diagnoseToolArgs{
		ErrorLog: SanitizeDiagnosisText(logSnapshot, defaultDiagnosisPayloadMaxBytes),
		OSEnv: map[string]string{
			"os":     runtime.GOOS,
			"shell":  resolveShellPath(options.Shell),
			"cwd":    options.Workdir,
			"socket": socketPath,
		},
		CommandText: SanitizeDiagnosisText(trigger.CommandText, 1024),
		ExitCode:    trigger.ExitCode,
	}

	requestPayload, err := json.Marshal(requestArgs)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("请求构建失败: %w", err)
	}

	callContext, cancel := context.WithTimeout(context.Background(), diagnoseCallTimeout)
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
			Timeout: diagnoseCallTimeout,
			Retries: 0,
		},
	); err != nil {
		return tools.ToolResult{}, fmt.Errorf("诊断调用失败: %w", err)
	}

	if frame.Type == gateway.FrameTypeError && frame.Error != nil {
		return tools.ToolResult{}, fmt.Errorf(
			"网关返回错误 (%s): %s",
			strings.TrimSpace(frame.Error.Code),
			strings.TrimSpace(frame.Error.Message),
		)
	}
	if frame.Type != gateway.FrameTypeAck {
		return tools.ToolResult{}, fmt.Errorf("网关返回未知帧: %s", frame.Type)
	}

	toolResult, err := decodeToolResult(frame.Payload)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("诊断结果解析失败: %w", err)
	}
	return toolResult, nil
}

// decodeToolResult 将网关 payload 反序列化为统一的 ToolResult 结构。
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

// renderDiagnosis 将工具返回渲染成终端可读格式，并在失败时降级展示原始文本。
func renderDiagnosis(output io.Writer, content string, isError bool) {
	headerColor := "\033[36m"
	if isError {
		headerColor = "\033[31m"
	}
	writeProxyf(output, "\n%s[NeoCode Diagnosis]\033[0m\n", headerColor)

	trimmedContent := strings.TrimSpace(content)
	if trimmedContent == "" {
		writeProxyLine(output, "- 无可用诊断内容")
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

// streamPTYOutput 解析 PTY 输出并分离 OSC133 事件，按规则触发 Auto 诊断任务。
func streamPTYOutput(
	ptyReader io.Reader,
	outputSink io.Writer,
	commandLogBuffer *UTF8RingBuffer,
	tracker *commandTracker,
	autoTriggerCh chan<- diagnoseTrigger,
	autoState *autoRuntimeState,
) {
	if ptyReader == nil || outputSink == nil || commandLogBuffer == nil {
		return
	}
	parser := &OSC133Parser{}
	collectingCommand := false
	pendingTrigger := (*diagnoseTrigger)(nil)

	buffer := make([]byte, 4096)
	for {
		readBytes, err := ptyReader.Read(buffer)
		if readBytes > 0 {
			cleanOutput, events := parser.Feed(buffer[:readBytes])
			if len(cleanOutput) > 0 {
				_, _ = outputSink.Write(cleanOutput)
				if collectingCommand {
					_, _ = commandLogBuffer.Write(cleanOutput)
				}
			}
			for _, event := range events {
				switch event.Type {
				case ShellEventPromptReady:
					autoState.OSCReady.Store(true)
					if pendingTrigger != nil && autoState.Enabled.Load() {
						select {
						case autoTriggerCh <- *pendingTrigger:
						default:
							// 渠道拥塞时直接丢弃本次触发，避免阻塞 PTY 输出主循环。
						}
						pendingTrigger = nil
					}
				case ShellEventCommandStart:
					collectingCommand = true
					commandLogBuffer = NewUTF8RingBuffer(DefaultRingBufferCapacity / 2)
				case ShellEventCommandDone:
					collectingCommand = false
					commandText := ""
					if tracker != nil {
						commandText = tracker.LastCommand()
					}
					outputText := commandLogBuffer.SnapshotString()
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

// copyInputWithTracker 在转发用户输入到 PTY 的同时提取最近命令文本。
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

// Observe 观察输入字节流并维护最新完整命令行。
func (t *commandTracker) Observe(payload []byte) {
	if t == nil || len(payload) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range payload {
		switch b {
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

// LastCommand 返回最近一次完成输入的命令文本。
func (t *commandTracker) LastCommand() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(t.lastCommand)
}

// isClosedNetworkError 识别网络连接已关闭类错误，避免重复打印无效噪声。
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

// serializedWriter 在并发写入场景下串行化输出，避免诊断内容与 shell 输出交错。
type serializedWriter struct {
	writer io.Writer
	lock   *sync.Mutex
}

// Write 实现 io.Writer 并在写入前加锁。
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
