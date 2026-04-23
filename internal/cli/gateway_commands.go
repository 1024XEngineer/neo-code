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
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"neo-code/internal/config"
	"neo-code/internal/gateway"
	"neo-code/internal/gateway/adapters/urlscheme"
	gatewayauth "neo-code/internal/gateway/auth"
)

const (
	defaultGatewayLogLevel          = "info"
	fallbackDispatchErrorJSON       = `{"status":"error","code":"internal_error","message":"failed to encode or write error output"}`
	defaultGatewayIdleShutdownDelay = 30 * time.Second
)

var (
	runGatewayCommand       = defaultGatewayCommandRunner
	runURLDispatchCommand   = defaultURLDispatchCommandRunner
	newGatewayServer        = defaultNewGatewayServer
	newGatewayNetwork       = defaultNewGatewayNetworkServer
	dispatchURLThroughIPC   = urlscheme.Dispatch
	newAuthManager          = defaultNewAuthManager
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

// defaultNewAuthManager 创建默认网关认证器，并把具体持久化实现收敛在 CLI 装配层内部。
func defaultNewAuthManager(path string) (gateway.TokenAuthenticator, error) {
	return gatewayauth.NewManager(path)
}

// newGatewayCommand 创建并返回根命令下的 gateway 子命令，负责启动本地 Gateway 进程。
func newGatewayCommand() *cobra.Command {
	return newGatewayServerCommand("gateway", "Start local gateway server", mustReadInheritedWorkdir)
}

// NewGatewayStandaloneCommand 鍒涘缓 gateway-only 鐙珛鍏ュ彛鍛戒护锛岀‘淇濅粎鏆撮湶缃戝叧鏈嶅姟璇箟銆?func NewGatewayStandaloneCommand() *cobra.Command {
	standaloneWorkdir := ""
	command := newGatewayServerCommand("neocode-gateway", "Start NeoCode gateway-only server", func(*cobra.Command) string {
		return standaloneWorkdir
	})
	command.Flags().StringVar(&standaloneWorkdir, "workdir", "", "宸ヤ綔鐩綍锛堣鐩栨湰娆¤繍琛屽伐浣滃尯锛?)
	return command
}

// newGatewayServerCommand 鏋勫缓缃戝叧鍚姩鍛戒护锛屽苟澶嶇敤缁熶竴鍙傛暟褰掍竴鍖栦笌鎵ц璺緞銆?func newGatewayServerCommand(use, short string, readWorkdir func(*cobra.Command) string) *cobra.Command {
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

// normalizeGatewayLogLevel 瀵圭綉鍏虫棩蹇楃骇鍒仛褰掍竴鍖栧苟鏍￠獙鍚堟硶鍊笺€?func normalizeGatewayLogLevel(logLevel string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(logLevel))
	switch normalized {
	case "debug", "info", "warn", "error":
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid --log-level %q: must be debug|info|warn|error", logLevel)
	}
}

// mustReadInheritedWorkdir 鍦ㄥ瓙鍛戒护涓畨鍏ㄨ鍙栫户鎵跨殑 --workdir锛岃鍙栧け璐ユ椂鍥為€€涓虹┖鍊笺€?func mustReadInheritedWorkdir(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	workdir, err := cmd.Flags().GetString("workdir")
	if err != nil {
		return ""
	}
	return workdir
}

// defaultGatewayCommandRunner 浣跨敤缃戝叧鏈嶅姟楠ㄦ灦鍚姩鏈湴 IPC 鐩戝惉骞跺鐞嗕俊鍙烽€€鍑恒€?func defaultGatewayCommandRunner(ctx context.Context, options gatewayCommandOptions) error {
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
	defer func() {
		relay.Stop()
		_ = networkServer.Close(context.Background())
		_ = ipcServer.Close(context.Background())
	}()

	logger.Printf("gateway ipc listen address: %s", ipcServer.ListenAddress())
	logger.Printf("gateway network listen address: %s", networkServer.ListenAddress())
	idleCloser.observe(0)

	go func() {
		serveErr := networkServer.Serve(runtimeContext, runtimePort)
		if serveErr != nil && runtimeContext.Err() == nil {
			logger.Printf(
				"warning: HTTP server failed to start on %s (port in use?), but IPC server is still running: %v",
				networkServer.ListenAddress(),
				serveErr,
			)
		}
	}()

	return ipcServer.Serve(runtimeContext, runtimePort)
}

type gatewayIdleShutdownController struct {
	logger      *log.Logger
	idleTimeout time.Duration
	cancel      context.CancelFunc

	mu    sync.Mutex
	timer *time.Timer
}

// newGatewayIdleShutdownController 鍒涘缓缃戝叧绌洪棽鑷€€鎺у埗鍣細杩炴帴鏁板綊闆跺悗寤惰繜閫€鍑猴紝鏈夎繛鎺ユ仮澶嶅垯鍙栨秷閫€鍑恒€?func newGatewayIdleShutdownController(logger *log.Logger, cancel context.CancelFunc) *gatewayIdleShutdownController {
	return &gatewayIdleShutdownController{
		logger:      logger,
		idleTimeout: defaultGatewayIdleShutdownDelay,
		cancel:      cancel,
	}
}

// observe 鎺ユ敹 IPC 娲昏穬杩炴帴鏁板揩鐓у苟缁存姢绌洪棽閫€鍑鸿鏃跺櫒銆?func (c *gatewayIdleShutdownController) observe(active int) {
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

// close 閲婃斁绌洪棽閫€鍑烘帶鍒跺櫒鎸佹湁鐨勮鏃跺櫒璧勬簮銆?func (c *gatewayIdleShutdownController) close() {
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

// buildGatewayControlPlaneACL 鍩轰簬閰嶇疆鏋勯€犳帶鍒堕潰 ACL 绛栫暐锛屾湭鐭ユā寮忕洿鎺ユ嫆缁濆惎鍔ㄣ€?func buildGatewayControlPlaneACL(aclMode string) (*gateway.ControlPlaneACL, error) {
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

// applyGatewayFlagOverrides 灏?CLI flags 瑕嗙洊鍒扮綉鍏抽厤缃紝浼樺厛绾ч珮浜?config.yaml銆?func applyGatewayFlagOverrides(gatewayConfig *config.GatewayConfig, options gatewayCommandOptions) {
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

// defaultNewGatewayServer 鍒涘缓榛樿缃戝叧鏈嶅姟瀹炰緥锛屼緵鍛戒护灞傚惎鍔ㄦ祦绋嬭皟鐢ㄣ€?func defaultNewGatewayServer(options gateway.ServerOptions) (gatewayServer, error) {
	return gateway.NewServer(options)
}

// defaultNewGatewayNetworkServer 鍒涘缓榛樿缃戝叧缃戠粶璁块棶闈㈡湇鍔″疄渚嬶紝渚涘懡浠ゅ眰鍚姩娴佺▼璋冪敤銆?func defaultNewGatewayNetworkServer(options gateway.NetworkServerOptions) (gatewayNetworkServer, error) {
	return gateway.NewNetworkServer(options)
}

// newURLDispatchCommand 鍒涘缓 URL Scheme 娲惧彂瀛愬懡浠ら鏋讹紝浠呭仛鍙傛暟鏀舵暃涓庤皟鐢ㄨ浆鍙戙€?func newURLDispatchCommand() *cobra.Command {
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

// defaultURLDispatchCommandRunner 鎵ц URL 鍞ら啋璇锋眰骞跺皢缁撴灉浠ョ粨鏋勫寲 JSON 杈撳嚭銆?func defaultURLDispatchCommandRunner(ctx context.Context, options urlDispatchCommandOptions) error {
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

// loadGatewayAuthToken 璇诲彇闈欓粯璁よ瘉 token锛涜嫢鏂囦欢涓嶅瓨鍦ㄥ垯鍥為€€涓虹┖浠ュ吋瀹规棤閴存潈妯″紡銆?func loadGatewayAuthToken(path string) (string, error) {
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

// normalizeDispatchURL 瀵?url-dispatch 杈撳叆鍋氭渶灏忓綊涓€鍖栵紝璇︾粏鏍￠獙浜ょ敱 dispatcher 瀹屾垚銆?func normalizeDispatchURL(rawURL string) (string, error) {
	normalized := strings.TrimSpace(rawURL)
	if normalized == "" {
		return "", errors.New("missing required --url or positional <url>")
	}
	return normalized, nil
}

// writeURLDispatchSuccessOutput 灏?url-dispatch 鎴愬姛缁撴灉杈撳嚭涓虹粨鏋勫寲 JSON銆?func writeURLDispatchSuccessOutput(writer io.Writer, result urlscheme.DispatchResult) error {
	return encodeJSONLine(writer, urlDispatchSuccessOutput{
		Status:        "ok",
		ListenAddress: result.ListenAddress,
		Action:        string(result.Response.Action),
		RequestID:     result.Response.RequestID,
		Payload:       result.Response.Payload,
	})
}

// writeURLDispatchErrorOutput 灏?url-dispatch 閿欒缁撴灉杈撳嚭涓虹粨鏋勫寲 JSON銆?func writeURLDispatchErrorOutput(writer io.Writer, err error) error {
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

// writeURLDispatchFallbackErrorOutput 鍦ㄧ粨鏋勫寲閿欒杈撳嚭澶辫触鏃舵彁渚涘厹搴?JSON锛岄伩鍏嶅懡浠ら潤榛橀€€鍑恒€?func writeURLDispatchFallbackErrorOutput(writer io.Writer) error {
	_, err := fmt.Fprintln(writer, fallbackDispatchErrorJSON)
	return err
}

// encodeJSONLine 灏嗗璞＄紪鐮佷负鍗曡 JSON锛屽苟鍐欏叆鐩爣杈撳嚭娴併€?func encodeJSONLine(writer io.Writer, payload any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(payload)
}

