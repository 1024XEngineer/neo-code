package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"neo-code/internal/gateway"
	gatewayclient "neo-code/internal/gateway/client"
	"neo-code/internal/gateway/protocol"
	"neo-code/internal/ptyproxy"
)

const (
	diagCallTimeout          = 90 * time.Second
	diagAskSessionPrefix     = "diag-ask"
	diagControlSessionPrefix = "diag-cli"
	diagAskSkillID           = "terminal-diagnosis"
	diagInputMaxBytes        = 256 * 1024
)

var (
	runShellCommand        = defaultShellCommandRunner
	runShellInitCommand    = defaultShellInitCommandRunner
	runDiagCommand         = defaultDiagCommandRunner
	runDiagInteractive     = defaultDiagInteractiveCommandRunner
	runDiagAutoCommand     = defaultDiagAutoCommandRunner
	runDiagDiagnoseCommand = defaultDiagCommandRunner
	runManualShellProxy    = ptyproxy.RunManualShell
	buildShellInitScript   = ptyproxy.BuildShellInitScript

	sendDiagnoseSignalFn = defaultSendDiagnoseSignal
	sendIDMEnterSignalFn = defaultSendIDMEnterSignal
	sendAutoModeSignalFn = defaultSendAutoModeSignal
	queryAutoModeFn      = defaultQueryAutoMode

	readDiagEnvValue = os.Getenv

	newDiagGatewayClient = defaultNewDiagGatewayClient
	diagSequence         atomic.Uint64
)

type shellCommandOptions struct {
	Workdir              string
	Shell                string
	GatewayListenAddress string
	GatewayTokenFile     string
	Init                 bool
}

type diagCommandOptions struct {
	Interactive bool
	SessionID   string
	ErrorLog    string
}

type diagAutoCommandOptions struct {
	Enabled   bool
	QueryOnly bool
	SessionID string
}

type diagGatewayClient interface {
	Authenticate(ctx context.Context) error
	Ask(
		ctx context.Context,
		params protocol.AskParams,
		result any,
		options ...gatewayclient.GatewayRPCCallOptions,
	) error
	TriggerAction(
		ctx context.Context,
		params protocol.TriggerActionParams,
		result any,
		options ...gatewayclient.GatewayRPCCallOptions,
	) error
	DeleteAskSession(
		ctx context.Context,
		params protocol.DeleteAskSessionParams,
		result any,
		options ...gatewayclient.GatewayRPCCallOptions,
	) error
	CallWithOptions(
		ctx context.Context,
		method string,
		params any,
		result any,
		options gatewayclient.GatewayRPCCallOptions,
	) error
	Notifications() <-chan gatewayclient.Notification
	Close() error
}

// newShellCommand 构建 `neocode shell` 命令并把参数交给 ptyproxy 层处理。
func newShellCommand() *cobra.Command {
	options := &shellCommandOptions{}
	command := &cobra.Command{
		Use:          "shell",
		Short:        "Start terminal proxy shell for neocode diagnose",
		SilenceUsage: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			if len(args) == 1 && options.Init {
				return nil
			}
			return cobra.NoArgs(cmd, args)
		},
		Annotations: map[string]string{
			commandAnnotationSkipGlobalPreload:     "true",
			commandAnnotationSkipSilentUpdateCheck: "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			shellPath := strings.TrimSpace(options.Shell)
			if options.Init && shellPath == "" && len(args) == 1 {
				shellPath = strings.TrimSpace(args[0])
			}
			normalized := shellCommandOptions{
				Workdir:              strings.TrimSpace(mustReadInheritedWorkdir(cmd)),
				Shell:                shellPath,
				GatewayListenAddress: strings.TrimSpace(options.GatewayListenAddress),
				GatewayTokenFile:     strings.TrimSpace(options.GatewayTokenFile),
				Init:                 options.Init,
			}
			if normalized.Init {
				return runShellInitCommand(cmd.Context(), normalized, cmd.OutOrStdout())
			}
			return runShellCommand(
				cmd.Context(),
				normalized,
				cmd.InOrStdin(),
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
			)
		},
	}

	command.Flags().StringVar(&options.Shell, "shell", "", "shell executable path (default $SHELL or /bin/bash)")
	command.Flags().StringVar(&options.GatewayListenAddress, "gateway-listen", "", "gateway listen address override")
	command.Flags().StringVar(&options.GatewayTokenFile, "gateway-token-file", "", "gateway token file override")
	command.Flags().BoolVar(&options.Init, "init", false, "print shell integration script")
	return command
}

// newDiagCommand 构建 `neocode diag` 主命令，支持普通诊断和 IDM 入口。
func newDiagCommand() *cobra.Command {
	options := &diagCommandOptions{}
	command := &cobra.Command{
		Use:          "diag",
		Short:        "Trigger terminal diagnose in current neocode shell",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		Annotations: map[string]string{
			commandAnnotationSkipGlobalPreload:     "true",
			commandAnnotationSkipSilentUpdateCheck: "true",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			errorLog := strings.TrimSpace(options.ErrorLog)
			if errorLog == "" {
				stdinPayload, err := readDiagInputFromStdin(cmd.InOrStdin())
				if err != nil {
					return err
				}
				errorLog = strings.TrimSpace(stdinPayload)
			}

			normalized := diagCommandOptions{
				Interactive: options.Interactive,
				SessionID:   strings.TrimSpace(options.SessionID),
				ErrorLog:    errorLog,
			}
			if normalized.Interactive {
				return runDiagInteractive(cmd.Context(), normalized)
			}
			return runDiagCommand(cmd.Context(), normalized)
		},
	}
	command.Flags().BoolVarP(&options.Interactive, "interactive", "i", false, "enter interactive diagnosis mode (IDM)")
	command.Flags().StringVar(&options.SessionID, "session", "", "target shell session id")
	command.Flags().StringVar(&options.ErrorLog, "error-log", "", "diagnose using direct error log input")
	command.AddCommand(
		newDiagAutoCommand(),
		newDiagDiagnoseCommand(),
	)
	return command
}

// newDiagAutoCommand 构建 `neocode diag auto` 子命令，控制自动诊断开关。
func newDiagAutoCommand() *cobra.Command {
	options := &diagAutoCommandOptions{}
	command := &cobra.Command{
		Use:          "auto <on|off|status>",
		Short:        "Set auto diagnosis mode in current neocode shell",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := strings.ToLower(strings.TrimSpace(args[0]))
			options.QueryOnly = false
			switch mode {
			case "on":
				options.Enabled = true
			case "off":
				options.Enabled = false
			case "status":
				options.QueryOnly = true
			default:
				return fmt.Errorf("unsupported auto mode %q: use on/off/status", mode)
			}
			return runDiagAutoCommand(cmd.Context(), diagAutoCommandOptions{
				Enabled:   options.Enabled,
				QueryOnly: options.QueryOnly,
				SessionID: strings.TrimSpace(options.SessionID),
			}, cmd.OutOrStdout())
		},
	}
	command.Flags().StringVar(&options.SessionID, "session", "", "target shell session id")
	return command
}

// newDiagDiagnoseCommand 构建 `neocode diag diagnose` 子命令，触发一次诊断流程。
func newDiagDiagnoseCommand() *cobra.Command {
	options := &diagCommandOptions{}
	command := &cobra.Command{
		Use:          "diagnose",
		Short:        "Trigger one manual diagnosis in current neocode shell",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			errorLog := strings.TrimSpace(options.ErrorLog)
			if errorLog == "" {
				stdinPayload, err := readDiagInputFromStdin(cmd.InOrStdin())
				if err != nil {
					return err
				}
				errorLog = strings.TrimSpace(stdinPayload)
			}
			return runDiagDiagnoseCommand(cmd.Context(), diagCommandOptions{
				SessionID: strings.TrimSpace(options.SessionID),
				ErrorLog:  errorLog,
			})
		},
	}
	command.Flags().StringVar(&options.SessionID, "session", "", "target shell session id")
	command.Flags().StringVar(&options.ErrorLog, "error-log", "", "diagnose using direct error log input")
	return command
}

// defaultShellCommandRunner 调用 ptyproxy 启动手动 shell 代理。
func defaultShellCommandRunner(
	ctx context.Context,
	options shellCommandOptions,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	return runManualShellProxy(ctx, ptyproxy.ManualShellOptions{
		Workdir:              strings.TrimSpace(options.Workdir),
		Shell:                strings.TrimSpace(options.Shell),
		GatewayListenAddress: strings.TrimSpace(options.GatewayListenAddress),
		GatewayTokenFile:     strings.TrimSpace(options.GatewayTokenFile),
		Stdin:                stdin,
		Stdout:               stdout,
		Stderr:               stderr,
	})
}

// defaultShellInitCommandRunner 输出 shell 初始化脚本。
func defaultShellInitCommandRunner(_ context.Context, options shellCommandOptions, stdout io.Writer) error {
	if stdout == nil {
		return nil
	}
	_, err := io.WriteString(stdout, buildShellInitScript(strings.TrimSpace(options.Shell))+"\n")
	return err
}

// defaultDiagCommandRunner 根据输入分发诊断动作或 Ask 诊断。
func defaultDiagCommandRunner(ctx context.Context, options diagCommandOptions) error {
	if strings.TrimSpace(options.ErrorLog) != "" {
		return runDiagAsk(ctx, options)
	}
	targetSessionID := resolveDiagTargetSessionID(options.SessionID)
	return sendDiagnoseSignalFn(ctx, targetSessionID)
}

// defaultDiagInteractiveCommandRunner 触发 shell 进入 IDM 模式。
func defaultDiagInteractiveCommandRunner(ctx context.Context, options diagCommandOptions) error {
	targetSessionID := resolveDiagTargetSessionID(options.SessionID)
	return sendIDMEnterSignalFn(ctx, targetSessionID)
}

// defaultDiagAutoCommandRunner 处理 auto on/off/status，并输出状态文案。
func defaultDiagAutoCommandRunner(ctx context.Context, options diagAutoCommandOptions, stdout io.Writer) error {
	targetSessionID := resolveDiagTargetSessionID(options.SessionID)
	if options.QueryOnly {
		enabled, err := queryAutoModeFn(ctx, targetSessionID)
		if err != nil {
			return err
		}
		if stdout != nil {
			if enabled {
				_, _ = io.WriteString(stdout, "auto mode enabled\n")
			} else {
				_, _ = io.WriteString(stdout, "auto mode disabled\n")
			}
		}
		return nil
	}

	if err := sendAutoModeSignalFn(ctx, targetSessionID, options.Enabled); err != nil {
		return err
	}
	if stdout != nil {
		if options.Enabled {
			_, _ = io.WriteString(stdout, "auto mode enabled\n")
		} else {
			_, _ = io.WriteString(stdout, "auto mode disabled\n")
		}
	}
	return nil
}

// defaultNewDiagGatewayClient 创建面向诊断命令的网关 RPC 客户端。
func defaultNewDiagGatewayClient() (diagGatewayClient, error) {
	return gatewayclient.NewGatewayRPCClient(gatewayclient.GatewayRPCClientOptions{
		DisableHeartbeatLog: true,
	})
}

// defaultSendDiagnoseSignal 通过 triggerAction 触发诊断动作。
func defaultSendDiagnoseSignal(ctx context.Context, sessionID string) error {
	_, err := triggerDiagAction(ctx, sessionID, "diagnose")
	return err
}

// defaultSendIDMEnterSignal 通过 triggerAction 触发进入 IDM 动作。
func defaultSendIDMEnterSignal(ctx context.Context, sessionID string) error {
	_, err := triggerDiagAction(ctx, sessionID, "idm_enter")
	return err
}

// defaultSendAutoModeSignal 通过 triggerAction 切换 auto 状态。
func defaultSendAutoModeSignal(ctx context.Context, sessionID string, enabled bool) error {
	action := "auto_off"
	if enabled {
		action = "auto_on"
	}
	_, err := triggerDiagAction(ctx, sessionID, action)
	return err
}

// defaultQueryAutoMode 通过 triggerAction 查询 auto 状态。
func defaultQueryAutoMode(ctx context.Context, sessionID string) (bool, error) {
	frame, err := triggerDiagAction(ctx, sessionID, "auto_status")
	if err != nil {
		return false, err
	}
	payloadMap, ok := normalizePayloadMap(frame.Payload)
	if !ok {
		return false, errors.New("gateway auto_status response is invalid")
	}
	return readPayloadBool(payloadMap, "auto_enabled"), nil
}

// triggerDiagAction 建立 RPC 会话并调用 `gateway.experimental.triggerAction`。
func triggerDiagAction(ctx context.Context, sessionID string, action string) (gateway.MessageFrame, error) {
	client, err := newDiagGatewayClient()
	if err != nil {
		return gateway.MessageFrame{}, err
	}
	defer client.Close()

	callCtx, cancel := context.WithTimeout(ctx, diagCallTimeout)
	defer cancel()

	if err := client.Authenticate(callCtx); err != nil {
		return gateway.MessageFrame{}, err
	}

	bindSessionID := strings.TrimSpace(sessionID)
	if bindSessionID == "" {
		bindSessionID = generateDiagSessionID(diagControlSessionPrefix)
	}
	if err := bindDiagStream(callCtx, client, bindSessionID); err != nil {
		return gateway.MessageFrame{}, err
	}

	var frame gateway.MessageFrame
	if err := client.TriggerAction(
		callCtx,
		protocol.TriggerActionParams{
			SessionID: strings.TrimSpace(sessionID),
			Action:    strings.TrimSpace(action),
		},
		&frame,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagCallTimeout,
			Retries: 0,
		},
	); err != nil {
		return gateway.MessageFrame{}, err
	}
	if frame.Type == gateway.FrameTypeError && frame.Error != nil {
		return gateway.MessageFrame{}, fmt.Errorf(
			"gateway trigger_action failed (%s): %s",
			strings.TrimSpace(frame.Error.Code),
			strings.TrimSpace(frame.Error.Message),
		)
	}
	if frame.Type != gateway.FrameTypeAck {
		return gateway.MessageFrame{}, fmt.Errorf("unexpected gateway frame type: %s", strings.TrimSpace(string(frame.Type)))
	}
	return frame, nil
}

// bindDiagStream 以 CLI 角色绑定事件流，接收后续 RPC 事件和通知。
func bindDiagStream(ctx context.Context, client diagGatewayClient, sessionID string) error {
	var frame gateway.MessageFrame
	if err := client.CallWithOptions(
		ctx,
		protocol.MethodGatewayBindStream,
		protocol.BindStreamParams{
			SessionID: strings.TrimSpace(sessionID),
			Channel:   "all",
			Role:      "cli",
		},
		&frame,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagCallTimeout,
			Retries: 0,
		},
	); err != nil {
		return err
	}
	if frame.Type == gateway.FrameTypeError && frame.Error != nil {
		return fmt.Errorf(
			"gateway bind_stream failed (%s): %s",
			strings.TrimSpace(frame.Error.Code),
			strings.TrimSpace(frame.Error.Message),
		)
	}
	if frame.Type != gateway.FrameTypeAck {
		return fmt.Errorf("unexpected gateway frame type for bind_stream: %s", strings.TrimSpace(string(frame.Type)))
	}
	return nil
}

// runDiagAsk 通过 `gateway.ask` 发起 Ask 诊断并消费流式返回。
func runDiagAsk(ctx context.Context, options diagCommandOptions) error {
	if err := ptyproxy.EnsureTerminalDiagnosisSkillFile(); err != nil {
		return err
	}

	client, err := newDiagGatewayClient()
	if err != nil {
		return err
	}
	defer client.Close()

	callCtx, cancel := context.WithTimeout(ctx, diagCallTimeout)
	defer cancel()

	if err := client.Authenticate(callCtx); err != nil {
		return err
	}

	askSessionID := strings.TrimSpace(options.SessionID)
	generatedSessionID := false
	if askSessionID == "" {
		askSessionID = generateDiagSessionID(diagAskSessionPrefix)
		generatedSessionID = true
	}
	if err := bindDiagStream(callCtx, client, askSessionID); err != nil {
		return err
	}

	var askAck gateway.MessageFrame
	if err := client.Ask(
		callCtx,
		protocol.AskParams{
			SessionID: askSessionID,
			UserQuery: buildDiagAskQuery(options.ErrorLog),
			Skills:    []string{diagAskSkillID},
		},
		&askAck,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: diagCallTimeout,
			Retries: 0,
		},
	); err != nil {
		return err
	}
	if askAck.Type == gateway.FrameTypeError && askAck.Error != nil {
		return fmt.Errorf(
			"gateway ask failed (%s): %s",
			strings.TrimSpace(askAck.Error.Code),
			strings.TrimSpace(askAck.Error.Message),
		)
	}
	if askAck.Type != gateway.FrameTypeAck {
		return fmt.Errorf("unexpected gateway frame type for ask: %s", strings.TrimSpace(string(askAck.Type)))
	}

	waitErr := waitDiagAskStream(callCtx, client, askSessionID)
	if generatedSessionID {
		deleteDiagAskSessionQuiet(context.Background(), client, askSessionID)
	}
	return waitErr
}

// buildDiagAskQuery 组装诊断提示词，把错误日志作为主体输入给 Ask。
func buildDiagAskQuery(errorLog string) string {
	trimmed := strings.TrimSpace(errorLog)
	if trimmed == "" {
		return ""
	}
	return "请根据以下终端错误日志进行诊断，给出根因和可执行修复步骤：\n\n" + trimmed
}

// waitDiagAskStream 监听 ask_chunk/ask_done/ask_error 事件并渲染输出。
func waitDiagAskStream(ctx context.Context, client diagGatewayClient, sessionID string) error {
	notifications := client.Notifications()
	streamed := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case notification, ok := <-notifications:
			if !ok {
				return errors.New("gateway notification channel closed")
			}
			if !strings.EqualFold(strings.TrimSpace(notification.Method), protocol.MethodGatewayEvent) {
				continue
			}

			var frame gateway.MessageFrame
			if err := json.Unmarshal(notification.Params, &frame); err != nil {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(frame.SessionID), strings.TrimSpace(sessionID)) {
				continue
			}

			payloadMap, ok := normalizePayloadMap(frame.Payload)
			if !ok {
				continue
			}
			eventType := strings.ToLower(strings.TrimSpace(readPayloadString(payloadMap, "event_type")))
			nestedPayload, _ := payloadMap["payload"]
			switch eventType {
			case string(gateway.RuntimeEventTypeAskChunk):
				chunk := extractAskText(nestedPayload)
				if chunk == "" {
					continue
				}
				_, _ = io.WriteString(os.Stdout, chunk)
				streamed = true
			case string(gateway.RuntimeEventTypeAskDone):
				if !streamed {
					if message := extractAskText(nestedPayload); message != "" {
						_, _ = io.WriteString(os.Stdout, message)
					}
				}
				_, _ = io.WriteString(os.Stdout, "\n")
				return nil
			case string(gateway.RuntimeEventTypeAskError):
				message := extractAskText(nestedPayload)
				if message == "" {
					message = "ask failed"
				}
				return errors.New(strings.TrimSpace(message))
			}
		}
	}
}

// deleteDiagAskSessionQuiet 在 Ask 会话由 CLI 临时创建时静默清理会话。
func deleteDiagAskSessionQuiet(ctx context.Context, client diagGatewayClient, sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var frame gateway.MessageFrame
	_ = client.DeleteAskSession(
		callCtx,
		protocol.DeleteAskSessionParams{SessionID: strings.TrimSpace(sessionID)},
		&frame,
		gatewayclient.GatewayRPCCallOptions{
			Timeout: 10 * time.Second,
			Retries: 0,
		},
	)
}

// normalizePayloadMap 将通用 payload 标准化为 `map[string]any` 方便读取字段。
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

// readPayloadString 从 payload 中读取字符串字段并执行 trim。
func readPayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, exists := payload[strings.TrimSpace(key)]
	if !exists || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

// readPayloadBool 从 payload 中读取布尔字段，兼容字符串和通用值。
func readPayloadBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	value, exists := payload[strings.TrimSpace(key)]
	if !exists || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err != nil {
			return false
		}
		return parsed
	default:
		parsed, err := strconv.ParseBool(strings.TrimSpace(fmt.Sprint(typed)))
		if err != nil {
			return false
		}
		return parsed
	}
}

// extractAskText 从多种事件负载结构中提取可展示文本。
func extractAskText(payload any) string {
	switch typed := payload.(type) {
	case nil:
		return ""
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"delta", "full_response", "message", "text", "content", "summary", "chunk"} {
			if text := strings.TrimSpace(readPayloadString(typed, key)); text != "" {
				return text
			}
		}
		if nested, exists := typed["payload"]; exists {
			return extractAskText(nested)
		}
		return ""
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(typed))
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return strings.TrimSpace(fmt.Sprint(typed))
		}
		return extractAskText(decoded)
	}
}

// resolveDiagTargetSessionID 优先使用显式 session，其次读取 shell 会话环境变量。
func resolveDiagTargetSessionID(explicitSessionID string) string {
	sessionID := strings.TrimSpace(explicitSessionID)
	if sessionID != "" {
		return sessionID
	}
	return strings.TrimSpace(readDiagEnvValue(ptyproxy.ShellSessionEnv))
}

// generateDiagSessionID 生成仅用于诊断链路的短生命周期会话 ID。
func generateDiagSessionID(prefix string) string {
	if strings.TrimSpace(prefix) == "" {
		prefix = diagControlSessionPrefix
	}
	sequence := diagSequence.Add(1)
	return fmt.Sprintf("%s-%d-%d", strings.TrimSpace(prefix), os.Getpid(), sequence)
}

// readDiagInputFromStdin 在 stdin 是管道输入时读取错误日志内容。
func readDiagInputFromStdin(stdin io.Reader) (string, error) {
	if stdin == nil {
		return "", nil
	}
	if file, ok := stdin.(*os.File); ok {
		info, err := file.Stat()
		if err == nil && (info.Mode()&os.ModeCharDevice) != 0 {
			return "", nil
		}
	}
	content, err := io.ReadAll(io.LimitReader(stdin, diagInputMaxBytes))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}
