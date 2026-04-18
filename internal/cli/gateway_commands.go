package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"neo-code/internal/config"
	"neo-code/internal/gateway"
	"neo-code/internal/gateway/adapters/urlscheme"
	gatewayauth "neo-code/internal/gateway/auth"
)

const (
	defaultGatewayLogLevel    = "info"
	fallbackDispatchErrorJSON = `{"status":"error","code":"internal_error","message":"failed to encode or write error output"}`
)

var (
	runGatewayCommand       = defaultGatewayCommandRunner
	runURLDispatchCommand   = defaultURLDispatchCommandRunner
	newGatewayServer        = defaultNewGatewayServer
	newGatewayNetwork       = defaultNewGatewayNetworkServer
	dispatchURLThroughIPC   = urlscheme.Dispatch
	newAuthManager          = gatewayauth.NewManager
	loadAuthToken           = loadGatewayAuthToken
	exitProcess             = os.Exit
	writeDispatchError      = writeURLDispatchErrorOutput
	writeDispatchSuccess    = writeURLDispatchSuccessOutput
	buildGatewayRuntimePort = defaultBuildGatewayRuntimePort
)

type gatewayCommandOptions struct {
	ListenAddress string
	HTTPAddress   string
	LogLevel      string
	TokenFile     string
	ACLMode       string
	Workdir       string

	MaxFrameBytes            int
	IPCMaxConnections        int
	HTTPMaxRequestBytes      int
	HTTPMaxStreamConnections int

	IPCReadSec      int
	IPCWriteSec     int
	HTTPReadSec     int
	HTTPWriteSec    int
	HTTPShutdownSec int

	MetricsEnabled           bool
	MetricsEnabledOverridden bool
}

type urlDispatchCommandOptions struct {
	URL           string
	ListenAddress string
	TokenFile     string
}

type urlDispatchSuccessOutput struct {
	Status        string `json:"status"`
	ListenAddress string `json:"listen_address"`
	Action        string `json:"action"`
	RequestID     string `json:"request_id,omitempty"`
	Payload       any    `json:"payload,omitempty"`
}

type urlDispatchErrorOutput struct {
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type gatewayServer interface {
	ListenAddress() string
	Serve(ctx context.Context, runtimePort gateway.RuntimePort) error
	Close(ctx context.Context) error
}

type gatewayNetworkServer interface {
	ListenAddress() string
	Serve(ctx context.Context, runtimePort gateway.RuntimePort) error
	Close(ctx context.Context) error
}

// newGatewayCommand 创建并返回网关子命令，负责启动本地 Gateway 进程。
func newGatewayCommand() *cobra.Command {
	options := &gatewayCommandOptions{}

	cmd := &cobra.Command{
		Use:          "gateway",
		Short:        "Start local gateway server",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizedLogLevel, err := normalizeGatewayLogLevel(options.LogLevel)
			if err != nil {
				return err
			}

			return runGatewayCommand(cmd.Context(), gatewayCommandOptions{
				ListenAddress: strings.TrimSpace(options.ListenAddress),
				HTTPAddress:   strings.TrimSpace(options.HTTPAddress),
				LogLevel:      normalizedLogLevel,
				TokenFile:     strings.TrimSpace(options.TokenFile),
				ACLMode:       strings.TrimSpace(options.ACLMode),
				Workdir:       strings.TrimSpace(mustReadInheritedWorkdir(cmd)),

				MaxFrameBytes:            options.MaxFrameBytes,
				IPCMaxConnections:        options.IPCMaxConnections,
				HTTPMaxRequestBytes:      options.HTTPMaxRequestBytes,
				HTTPMaxStreamConnections: options.HTTPMaxStreamConnections,

				IPCReadSec:      options.IPCReadSec,
				IPCWriteSec:     options.IPCWriteSec,
				HTTPReadSec:     options.HTTPReadSec,
				HTTPWriteSec:    options.HTTPWriteSec,
				HTTPShutdownSec: options.HTTPShutdownSec,

				MetricsEnabled:           options.MetricsEnabled,
				MetricsEnabledOverridden: cmd.Flags().Changed("metrics-enabled"),
			})
		},
	}

	cmd.Flags().StringVar(&options.ListenAddress, "listen", "", "gateway listen address (optional override)")
	cmd.Flags().StringVar(
		&options.HTTPAddress,
		"http-listen",
		gateway.DefaultNetworkListenAddress,
		"gateway network listen address (loopback only)",
	)
	cmd.Flags().StringVar(&options.LogLevel, "log-level", defaultGatewayLogLevel, "gateway log level: debug|info|warn|error")
	cmd.Flags().StringVar(&options.TokenFile, "token-file", "", "gateway auth token file path (default ~/.neocode/auth.json)")
	cmd.Flags().StringVar(&options.ACLMode, "acl-mode", "", "gateway acl mode override (strict)")
	cmd.Flags().IntVar(&options.MaxFrameBytes, "max-frame-bytes", 0, "gateway max frame bytes override")
	cmd.Flags().IntVar(&options.IPCMaxConnections, "ipc-max-connections", 0, "gateway ipc max connections override")
	cmd.Flags().IntVar(&options.HTTPMaxRequestBytes, "http-max-request-bytes", 0, "gateway http max request bytes override")
	cmd.Flags().IntVar(
		&options.HTTPMaxStreamConnections,
		"http-max-stream-connections",
		0,
		"gateway http max stream connections override",
	)
	cmd.Flags().IntVar(&options.IPCReadSec, "ipc-read-sec", 0, "gateway ipc read timeout seconds override")
	cmd.Flags().IntVar(&options.IPCWriteSec, "ipc-write-sec", 0, "gateway ipc write timeout seconds override")
	cmd.Flags().IntVar(&options.HTTPReadSec, "http-read-sec", 0, "gateway http read timeout seconds override")
	cmd.Flags().IntVar(&options.HTTPWriteSec, "http-write-sec", 0, "gateway http write timeout seconds override")
	cmd.Flags().IntVar(
		&options.HTTPShutdownSec,
		"http-shutdown-sec",
		0,
		"gateway http shutdown timeout seconds override",
	)
	cmd.Flags().BoolVar(&options.MetricsEnabled, "metrics-enabled", false, "gateway metrics enable override")

	return cmd
}

// normalizeGatewayLogLevel 对网关日志级别做归一化并校验合法值。
func normalizeGatewayLogLevel(logLevel string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(logLevel))
	switch normalized {
	case "debug", "info", "warn", "error":
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid --log-level %q: must be debug|info|warn|error", logLevel)
	}
}

// mustReadInheritedWorkdir 在子命令中安全读取继承的 --workdir，读取失败时回退为空值。
func mustReadInheritedWorkdir(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	workdir, err := cmd.Flags().GetString("workdir")
	if err != nil {
		return ""
	}
	return workdir
}

// defaultGatewayCommandRunner 使用网关服务骨架启动本地 IPC 监听并处理信号退出。
func defaultGatewayCommandRunner(ctx context.Context, options gatewayCommandOptions) error {
	logger := log.New(os.Stderr, "neocode-gateway: ", log.LstdFlags)
	logger.Printf("starting gateway (log-level=%s)", options.LogLevel)

	signalContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	gatewayConfig, err := config.LoadGatewayConfig(signalContext, "")
	if err != nil {
		return err
	}
	applyGatewayFlagOverrides(&gatewayConfig, options)
	if err := gatewayConfig.Validate(); err != nil {
		return fmt.Errorf("gateway config override invalid: %w", err)
	}
	acl, err := buildGatewayControlPlaneACL(gatewayConfig.Security.ACLMode)
	if err != nil {
		return err
	}

	tokenFile := strings.TrimSpace(options.TokenFile)
	if tokenFile == "" {
		tokenFile = strings.TrimSpace(gatewayConfig.Security.TokenFile)
	}

	authManager, err := newAuthManager(tokenFile)
	if err != nil {
		return fmt.Errorf("initialize gateway auth manager: %w", err)
	}
	var metrics *gateway.GatewayMetrics
	if gatewayConfig.Observability.Enabled() {
		metrics = gateway.NewGatewayMetrics()
	}
	relay := gateway.NewStreamRelay(gateway.StreamRelayOptions{
		Logger:  logger,
		Metrics: metrics,
	})

	runtimePort, closeRuntimePort, err := buildGatewayRuntimePort(signalContext, options.Workdir)
	if err != nil {
		return fmt.Errorf("initialize gateway runtime: %w", err)
	}
	defer func() {
		if closeRuntimePort != nil {
			_ = closeRuntimePort()
		}
	}()

	ipcServer, err := newGatewayServer(gateway.ServerOptions{
		ListenAddress:  options.ListenAddress,
		Logger:         logger,
		MaxConnections: gatewayConfig.Limits.IPCMaxConnections,
		MaxFrameSize:   int64(gatewayConfig.Limits.MaxFrameBytes),
		ReadTimeout:    time.Duration(gatewayConfig.Timeouts.IPCReadSec) * time.Second,
		WriteTimeout:   time.Duration(gatewayConfig.Timeouts.IPCWriteSec) * time.Second,
		Relay:          relay,
		Authenticator:  authManager,
		ACL:            acl,
		Metrics:        metrics,
	})
	if err != nil {
		return err
	}
	networkServer, err := newGatewayNetwork(gateway.NetworkServerOptions{
		ListenAddress:        options.HTTPAddress,
		Logger:               logger,
		ReadTimeout:          time.Duration(gatewayConfig.Timeouts.HTTPReadSec) * time.Second,
		WriteTimeout:         time.Duration(gatewayConfig.Timeouts.HTTPWriteSec) * time.Second,
		ShutdownTimeout:      time.Duration(gatewayConfig.Timeouts.HTTPShutdownSec) * time.Second,
		MaxRequestBytes:      int64(gatewayConfig.Limits.HTTPMaxRequestBytes),
		MaxStreamConnections: gatewayConfig.Limits.HTTPMaxStreamConnections,
		Relay:                relay,
		Authenticator:        authManager,
		ACL:                  acl,
		Metrics:              metrics,
		AllowedOrigins:       gatewayConfig.Security.AllowOrigins,
	})
	if err != nil {
		_ = ipcServer.Close(context.Background())
		return err
	}
	defer func() {
		relay.Stop()
		_ = networkServer.Close(context.Background())
		_ = ipcServer.Close(context.Background())
	}()

	logger.Printf("gateway ipc listen address: %s", ipcServer.ListenAddress())
	logger.Printf("gateway network listen address: %s", networkServer.ListenAddress())

	go func() {
		serveErr := networkServer.Serve(signalContext, runtimePort)
		if serveErr != nil && signalContext.Err() == nil {
			logger.Printf(
				"warning: HTTP server failed to start on %s (port in use?), but IPC server is still running: %v",
				networkServer.ListenAddress(),
				serveErr,
			)
		}
	}()

	return ipcServer.Serve(signalContext, runtimePort)
}

// buildGatewayControlPlaneACL 基于配置构造控制面 ACL 策略，未知模式直接拒绝启动。
func buildGatewayControlPlaneACL(aclMode string) (*gateway.ControlPlaneACL, error) {
	normalizedACLMode := strings.ToLower(strings.TrimSpace(aclMode))
	if normalizedACLMode == "" {
		normalizedACLMode = string(gateway.ACLModeStrict)
	}
	switch normalizedACLMode {
	case string(gateway.ACLModeStrict):
		return gateway.NewStrictControlPlaneACL(), nil
	default:
		return nil, fmt.Errorf("unsupported gateway acl mode %q", aclMode)
	}
}

// applyGatewayFlagOverrides 将 CLI flags 覆盖到网关配置，优先级高于 config.yaml。
func applyGatewayFlagOverrides(gatewayConfig *config.GatewayConfig, options gatewayCommandOptions) {
	if gatewayConfig == nil {
		return
	}
	if options.ACLMode != "" {
		gatewayConfig.Security.ACLMode = options.ACLMode
	}
	if options.MaxFrameBytes > 0 {
		gatewayConfig.Limits.MaxFrameBytes = options.MaxFrameBytes
	}
	if options.IPCMaxConnections > 0 {
		gatewayConfig.Limits.IPCMaxConnections = options.IPCMaxConnections
	}
	if options.HTTPMaxRequestBytes > 0 {
		gatewayConfig.Limits.HTTPMaxRequestBytes = options.HTTPMaxRequestBytes
	}
	if options.HTTPMaxStreamConnections > 0 {
		gatewayConfig.Limits.HTTPMaxStreamConnections = options.HTTPMaxStreamConnections
	}
	if options.IPCReadSec > 0 {
		gatewayConfig.Timeouts.IPCReadSec = options.IPCReadSec
	}
	if options.IPCWriteSec > 0 {
		gatewayConfig.Timeouts.IPCWriteSec = options.IPCWriteSec
	}
	if options.HTTPReadSec > 0 {
		gatewayConfig.Timeouts.HTTPReadSec = options.HTTPReadSec
	}
	if options.HTTPWriteSec > 0 {
		gatewayConfig.Timeouts.HTTPWriteSec = options.HTTPWriteSec
	}
	if options.HTTPShutdownSec > 0 {
		gatewayConfig.Timeouts.HTTPShutdownSec = options.HTTPShutdownSec
	}
	if options.MetricsEnabledOverridden {
		enabled := options.MetricsEnabled
		gatewayConfig.Observability.MetricsEnabled = &enabled
	}
}

// defaultNewGatewayServer 创建默认网关服务实例，供命令层启动流程调用。
func defaultNewGatewayServer(options gateway.ServerOptions) (gatewayServer, error) {
	return gateway.NewServer(options)
}

// defaultNewGatewayNetworkServer 创建默认网关网络访问面服务实例，供命令层启动流程调用。
func defaultNewGatewayNetworkServer(options gateway.NetworkServerOptions) (gatewayNetworkServer, error) {
	return gateway.NewNetworkServer(options)
}

// newURLDispatchCommand 创建 URL Scheme 派发子命令骨架，仅做参数收敛与调用转发。
func newURLDispatchCommand() *cobra.Command {
	options := &urlDispatchCommandOptions{}

	cmd := &cobra.Command{
		Use:           "url-dispatch [url]",
		Short:         "Dispatch a neocode:// URL to gateway",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			urlValue := strings.TrimSpace(options.URL)
			if urlValue == "" && len(args) == 1 {
				urlValue = strings.TrimSpace(args[0])
			}
			if urlValue == "" {
				return errors.New("missing required --url or positional <url>")
			}
			normalizedURL, err := normalizeDispatchURL(urlValue)
			if err != nil {
				return err
			}

			dispatchErr := runURLDispatchCommand(cmd.Context(), urlDispatchCommandOptions{
				URL:           normalizedURL,
				ListenAddress: strings.TrimSpace(options.ListenAddress),
				TokenFile:     strings.TrimSpace(options.TokenFile),
			})
			if dispatchErr != nil {
				exitProcess(1)
				return nil
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&options.URL, "url", "", "neocode:// URL to dispatch")
	cmd.Flags().StringVar(&options.ListenAddress, "listen", "", "gateway listen address override")
	cmd.Flags().StringVar(&options.TokenFile, "token-file", "", "gateway auth token file path (default ~/.neocode/auth.json)")

	return cmd
}

// defaultURLDispatchCommandRunner 执行 URL 唤醒请求并将结果以结构化 JSON 输出。
func defaultURLDispatchCommandRunner(ctx context.Context, options urlDispatchCommandOptions) error {
	authToken, authErr := loadAuthToken(options.TokenFile)
	if authErr != nil {
		writeErr := writeDispatchError(os.Stderr, authErr)
		if writeErr != nil {
			_ = writeURLDispatchFallbackErrorOutput(os.Stderr)
		}
		exitProcess(1)
		return nil
	}

	result, err := dispatchURLThroughIPC(ctx, urlscheme.DispatchRequest{
		RawURL:        options.URL,
		ListenAddress: options.ListenAddress,
		AuthToken:     authToken,
	})
	if err != nil {
		writeErr := writeDispatchError(os.Stderr, err)
		if writeErr != nil {
			_ = writeURLDispatchFallbackErrorOutput(os.Stderr)
		}
		exitProcess(1)
		return nil
	}

	if err := writeDispatchSuccess(os.Stdout, result); err != nil {
		writeErr := writeDispatchError(os.Stderr, err)
		if writeErr != nil {
			_ = writeURLDispatchFallbackErrorOutput(os.Stderr)
		}
		exitProcess(1)
		return nil
	}
	return nil
}

// loadGatewayAuthToken 读取静默认证 token；若文件不存在则回退为空以兼容无鉴权模式。
func loadGatewayAuthToken(path string) (string, error) {
	token, err := gatewayauth.LoadTokenFromFile(path)
	if err == nil {
		return token, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "no such file") {
		return "", nil
	}
	return "", err
}

// normalizeDispatchURL 对 url-dispatch 输入做最小归一化，详细校验交由 dispatcher 完成。
func normalizeDispatchURL(rawURL string) (string, error) {
	normalized := strings.TrimSpace(rawURL)
	if normalized == "" {
		return "", errors.New("missing required --url or positional <url>")
	}
	return normalized, nil
}

// writeURLDispatchSuccessOutput 将 url-dispatch 成功结果输出为结构化 JSON。
func writeURLDispatchSuccessOutput(writer io.Writer, result urlscheme.DispatchResult) error {
	return encodeJSONLine(writer, urlDispatchSuccessOutput{
		Status:        "ok",
		ListenAddress: result.ListenAddress,
		Action:        string(result.Response.Action),
		RequestID:     result.Response.RequestID,
		Payload:       result.Response.Payload,
	})
}

// writeURLDispatchErrorOutput 将 url-dispatch 错误结果输出为结构化 JSON。
func writeURLDispatchErrorOutput(writer io.Writer, err error) error {
	code := "internal_error"
	message := err.Error()

	var dispatchErr *urlscheme.DispatchError
	if errors.As(err, &dispatchErr) {
		code = dispatchErr.Code
		message = dispatchErr.Message
	}

	return encodeJSONLine(writer, urlDispatchErrorOutput{
		Status:  "error",
		Code:    code,
		Message: message,
	})
}

// writeURLDispatchFallbackErrorOutput 在结构化错误输出失败时提供兜底 JSON，避免命令静默退出。
func writeURLDispatchFallbackErrorOutput(writer io.Writer) error {
	_, err := fmt.Fprintln(writer, fallbackDispatchErrorJSON)
	return err
}

// encodeJSONLine 将对象编码为单行 JSON，并写入目标输出流。
func encodeJSONLine(writer io.Writer, payload any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(payload)
}
