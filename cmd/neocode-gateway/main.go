package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"neo-code/internal/gateway"
)

const (
	defaultLogLevel = "info"
)

var errHelpRequested = errors.New("help requested")

// main 负责启动 Gateway 独立进程，并在收到系统信号时优雅退出。
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "neocode-gateway: %v\n", err)
		os.Exit(1)
	}
}

// run 解析启动参数并驱动网关服务生命周期。
func run() error {
	listenAddress, logLevel, err := parseFlags()
	if err != nil {
		if errors.Is(err, errHelpRequested) {
			return nil
		}
		return err
	}

	logger := log.New(os.Stderr, "neocode-gateway: ", log.LstdFlags)
	logger.Printf("starting gateway (log-level=%s)", logLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server, err := gateway.NewServer(gateway.ServerOptions{
		ListenAddress: listenAddress,
		Logger:        logger,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = server.Close(context.Background())
	}()

	logger.Printf("gateway listen address: %s", server.ListenAddress())
	return server.Serve(ctx, nil)
}

// parseFlags 解析命令行参数并执行基础校验。
func parseFlags() (listenAddress string, logLevel string, err error) {
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	var listen string
	var level string
	fs.StringVar(&listen, "listen", "", "gateway listen address (optional override)")
	fs.StringVar(&level, "log-level", defaultLogLevel, "gateway log level: debug|info|warn|error")

	if parseErr := fs.Parse(os.Args[1:]); parseErr != nil {
		if errors.Is(parseErr, flag.ErrHelp) {
			return "", "", errHelpRequested
		}
		return "", "", parseErr
	}

	normalizedLevel := strings.ToLower(strings.TrimSpace(level))
	switch normalizedLevel {
	case "debug", "info", "warn", "error":
	default:
		return "", "", fmt.Errorf("invalid --log-level %q: must be debug|info|warn|error", level)
	}

	return strings.TrimSpace(listen), normalizedLevel, nil
}
