package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/adapters"
)

// main 负责启动独立 Gateway 进程并监听本地 IPC 请求。
func main() {
	transportMode := flag.String("transport", "auto", "IPC transport mode: auto|npipe|uds")
	endpoint := flag.String("endpoint", "", "IPC endpoint path (socket path or named pipe)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bootstrap, err := gateway.NewBootstrap(gateway.BootstrapConfig{
		Transport: *transportMode,
		Endpoint:  *endpoint,
	}, adapters.NewCoreMock())
	if err != nil {
		fmt.Fprintf(os.Stderr, "neocode-gateway: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = bootstrap.Close()
	}()

	fmt.Fprintf(os.Stdout, "neocode-gateway listening on %s\n", bootstrap.Endpoint())
	if err := bootstrap.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "neocode-gateway: %v\n", err)
		os.Exit(1)
	}
}
