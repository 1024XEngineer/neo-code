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

	"github.com/spf13/cobra"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/adapters/urlscheme"
)

const (
	defaultGatewayLogLevel = "info"
)

var (
	runGatewayCommand     = defaultGatewayCommandRunner
	runURLDispatchCommand = defaultURLDispatchCommandRunner
	newGatewayServer      = defaultNewGatewayServer
	dispatchURLThroughIPC = urlscheme.Dispatch
)

type gatewayCommandOptions struct {
	ListenAddress string
	LogLevel      string
}

type urlDispatchCommandOptions struct {
	URL           string
	ListenAddress string
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
				LogLevel:      normalizedLogLevel,
			})
		},
	}

	cmd.Flags().StringVar(&options.ListenAddress, "listen", "", "gateway listen address (optional override)")
	cmd.Flags().StringVar(&options.LogLevel, "log-level", defaultGatewayLogLevel, "gateway log level: debug|info|warn|error")

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

// defaultGatewayCommandRunner 使用网关服务骨架启动本地 IPC 监听并处理信号退出。
func defaultGatewayCommandRunner(ctx context.Context, options gatewayCommandOptions) error {
	logger := log.New(os.Stderr, "neocode-gateway: ", log.LstdFlags)
	logger.Printf("starting gateway (log-level=%s)", options.LogLevel)

	signalContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	server, err := newGatewayServer(gateway.ServerOptions{
		ListenAddress: options.ListenAddress,
		Logger:        logger,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = server.Close(context.Background())
	}()

	logger.Printf("gateway listen address: %s", server.ListenAddress())
	return server.Serve(signalContext, nil)
}

// defaultNewGatewayServer 创建默认网关服务实例，供命令层启动流程调用。
func defaultNewGatewayServer(options gateway.ServerOptions) (gatewayServer, error) {
	return gateway.NewServer(options)
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

			return runURLDispatchCommand(cmd.Context(), urlDispatchCommandOptions{
				URL:           normalizedURL,
				ListenAddress: strings.TrimSpace(options.ListenAddress),
			})
		},
	}

	cmd.Flags().StringVar(&options.URL, "url", "", "neocode:// URL to dispatch")
	cmd.Flags().StringVar(&options.ListenAddress, "listen", "", "gateway listen address override")

	return cmd
}

// defaultURLDispatchCommandRunner 执行 URL 唤醒请求并将结果以结构化 JSON 输出。
func defaultURLDispatchCommandRunner(ctx context.Context, options urlDispatchCommandOptions) error {
	result, err := dispatchURLThroughIPC(ctx, urlscheme.DispatchRequest{
		RawURL:        options.URL,
		ListenAddress: options.ListenAddress,
	})
	if err != nil {
		writeErr := writeURLDispatchErrorOutput(os.Stderr, err)
		if writeErr != nil {
			return errors.Join(err, writeErr)
		}
		return err
	}

	if err := writeURLDispatchSuccessOutput(os.Stdout, result); err != nil {
		return err
	}
	return nil
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

// encodeJSONLine 将对象编码为单行 JSON，并写入目标输出流。
func encodeJSONLine(writer io.Writer, payload any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(payload)
}
