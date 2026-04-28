package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"neo-code/internal/config"
	"neo-code/internal/gateway"
	gatewayauth "neo-code/internal/gateway/auth"
)

const (
	defaultGatewayLogLevel          = "info"
	defaultGatewayIdleShutdownDelay = 30 * time.Second
)

var (
	runGatewayCommand       = defaultGatewayCommandRunner
	newGatewayServer        = defaultNewGatewayServer
	newGatewayNetwork       = defaultNewGatewayNetworkServer
	resolveExecutablePath   = os.Executable
	newAuthManager          = defaultNewAuthManager
	exitProcess             = os.Exit
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

// defaultNewAuthManager 创建默认网关认证器，并把具体持久化实现收敛在 CLI 装配层内部。
func defaultNewAuthManager(path string) (gateway.TokenAuthenticator, error) {
	return gatewayauth.NewManager(path)
}

// newGatewayCommand 创建并返回根命令下的 gateway 子命令，负责启动本地 Gateway 进程。
func newGatewayCommand() *cobra.Command {
	return newGatewayServerCommand("gateway", "Start local gateway server", mustReadInheritedWorkdir)
}

// NewGatewayStandaloneCommand 创建 gateway-only 独立入口命令，确保仅暴露网关服务语义。
func NewGatewayStandaloneCommand() *cobra.Command {
	standaloneWorkdir := ""
	command := newGatewayServerCommand("neocode-gateway", "Start NeoCode gateway-only server", func(*cobra.Command) string {
		return standaloneWorkdir
	})
	command.Flags().StringVar(&standaloneWorkdir, "workdir", "", "workdir override for this gateway process")
	return command
}

// newGatewayServerCommand 构建网关启动命令，并复用统一参数归一化与执行路径。
func newGatewayServerCommand(use, short string, readWorkdir func(*cobra.Command) string) *cobra.Command {
	options := &gatewayCommandOptions{}

	cmd := &cobra.Command{
		Use:          strings.TrimSpace(use),
		Short:        strings.TrimSpace(short),
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			normalizedLogLevel, err := normalizeGatewayLogLevel(options.LogLevel)
			if err != nil {
				return err
			}
			normalizedWorkdir := ""
			if readWorkdir != nil {
				normalizedWorkdir = strings.TrimSpace(readWorkdir(cmd))
			}

			return runGatewayCommand(cmd.Context(), gatewayCommandOptions{
				ListenAddress: strings.TrimSpace(options.ListenAddress),
				HTTPAddress:   strings.TrimSpace(options.HTTPAddress),
				LogLevel:      normalizedLogLevel,
				TokenFile:     strings.TrimSpace(options.TokenFile),
				ACLMode:       strings.TrimSpace(options.ACLMode),
				Workdir:       normalizedWorkdir,

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

// defaultGatewayCommandRunner 使用网关服务骨架启动本地 IPC 监听并处理中断退出。
func defaultGatewayCommandRunner(ctx context.Context, options gatewayCommandOptions) error {
	logger := log.New(os.Stderr, "neocode-gateway: ", log.LstdFlags)
	logger.Printf("starting gateway (log-level=%s)", options.LogLevel)

	signalContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtimeContext, cancelRuntime := context.WithCancel(signalContext)
	defer cancelRuntime()

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

	idleCloser := newGatewayIdleShutdownController(logger, cancelRuntime)
	defer idleCloser.close()

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
		ConnectionCountChanged: func(active int) {
			idleCloser.observe(active)
		},
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
	type transportAdapterEntry struct {
		name    string
		adapter gateway.TransportAdapter
	}
	transportAdapters := []transportAdapterEntry{
		{name: "ipc", adapter: ipcServer},
		{name: "network", adapter: networkServer},
	}
	defer func() {
		relay.Stop()
		for index := len(transportAdapters) - 1; index >= 0; index-- {
			_ = transportAdapters[index].adapter.Close(context.Background())
		}
	}()

	for _, entry := range transportAdapters {
		logger.Printf("gateway %s listen address: %s", entry.name, entry.adapter.ListenAddress())
	}
	idleCloser.observe(0)

	for index, entry := range transportAdapters {
		if index == 0 {
			continue
		}
		go func(networkAdapter gateway.TransportAdapter) {
			serveErr := networkAdapter.Serve(runtimeContext, runtimePort)
			if serveErr != nil && runtimeContext.Err() == nil {
				logger.Printf(
					"warning: HTTP server failed to start on %s (port in use?), but IPC server is still running: %v",
					networkAdapter.ListenAddress(),
					serveErr,
				)
			}
		}(entry.adapter)
	}

	return transportAdapters[0].adapter.Serve(runtimeContext, runtimePort)
}

type gatewayIdleShutdownController struct {
	logger      *log.Logger
	idleTimeout time.Duration
	cancel      context.CancelFunc

	mu    sync.Mutex
	timer *time.Timer
}

// newGatewayIdleShutdownController 创建网关空闲退出控制器：连接归零后延迟退出，连接恢复则取消退出。
func newGatewayIdleShutdownController(logger *log.Logger, cancel context.CancelFunc) *gatewayIdleShutdownController {
	return &gatewayIdleShutdownController{
		logger:      logger,
		idleTimeout: defaultGatewayIdleShutdownDelay,
		cancel:      cancel,
	}
}

// observe 接收 IPC 活跃连接数快照并维护空闲退出计时器。
func (c *gatewayIdleShutdownController) observe(active int) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if active > 0 {
		if c.timer != nil {
			c.timer.Stop()
			c.timer = nil
			if c.logger != nil {
				c.logger.Printf("active ipc connections=%d, cancel idle shutdown timer", active)
			}
		}
		return
	}

	if c.timer != nil {
		return
	}

	timeout := c.idleTimeout
	if timeout <= 0 {
		timeout = defaultGatewayIdleShutdownDelay
	}
	if c.logger != nil {
		c.logger.Printf("ipc connections dropped to zero, gateway will exit in %s if still idle", timeout)
	}
	c.timer = time.AfterFunc(timeout, func() {
		if c.logger != nil {
			c.logger.Printf("idle timeout reached, shutting down gateway")
		}
		if c.cancel != nil {
			c.cancel()
		}
	})
}

// close 释放空闲退出控制器持有的计时器资源。
func (c *gatewayIdleShutdownController) close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
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
func defaultNewGatewayServer(options gateway.ServerOptions) (gateway.TransportAdapter, error) {
	return gateway.NewServer(options)
}

// defaultNewGatewayNetworkServer 创建默认网关网络访问服务实例，供命令层启动流程调用。
func defaultNewGatewayNetworkServer(options gateway.NetworkServerOptions) (gateway.TransportAdapter, error) {
	return gateway.NewNetworkServer(options)
}

// encodeJSONLine 将对象编码为单行 JSON，并写入目标输出流。
func encodeJSONLine(writer io.Writer, payload any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(payload)
}
