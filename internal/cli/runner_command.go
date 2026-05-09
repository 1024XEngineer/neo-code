package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"neo-code/internal/runner"
)

var runRunnerCommandFn = defaultRunRunner
var newRunnerServiceFn = func(cfg runner.Config) (runnerService, error) {
	return runner.New(cfg)
}

type runnerService interface {
	Run(context.Context) error
	Stop()
}

type runnerCommandOptions struct {
	GatewayAddress string
	TokenFile      string
	RunnerID       string
	RunnerName     string
	Workdir        string
}

func newRunnerCommand() *cobra.Command {
	options := &runnerCommandOptions{}
	cmd := &cobra.Command{
		Use:          "runner",
		Short:        "Start local runner daemon for remote task execution",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		Annotations: map[string]string{
			commandAnnotationSkipGlobalPreload:     "true",
			commandAnnotationSkipSilentUpdateCheck: "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunnerCommandFn(cmd.Context(), *options)
		},
	}

	cmd.Flags().StringVar(&options.GatewayAddress, "gateway-address", "", "gateway WebSocket address (e.g. 127.0.0.1:8080)")
	cmd.Flags().StringVar(&options.TokenFile, "token-file", "", "gateway token file path")
	cmd.Flags().StringVar(&options.RunnerID, "runner-id", "", "runner identifier (default: hostname)")
	cmd.Flags().StringVar(&options.RunnerName, "runner-name", "", "human-readable runner name")
	cmd.Flags().StringVar(&options.Workdir, "workdir", "", "runner working directory (default: current dir)")

	return cmd
}

func defaultRunRunner(ctx context.Context, options runnerCommandOptions) error {
	gatewayAddress := strings.TrimSpace(options.GatewayAddress)
	if gatewayAddress == "" {
		gatewayAddress = "127.0.0.1:8080"
	}

	workdir := strings.TrimSpace(options.Workdir)
	if workdir == "" {
		if wd, err := os.Getwd(); err == nil {
			workdir = wd
		}
	}

	runnerID := strings.TrimSpace(options.RunnerID)
	if runnerID == "" {
		if hostname, err := os.Hostname(); err == nil {
			runnerID = hostname
		} else {
			runnerID = "local-runner"
		}
	}

	token := ""
	if options.TokenFile != "" {
		data, err := os.ReadFile(options.TokenFile)
		if err != nil {
			return fmt.Errorf("read token file: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}

	r, err := newRunnerServiceFn(runner.Config{
		RunnerID:            runnerID,
		RunnerName:          strings.TrimSpace(options.RunnerName),
		GatewayAddress:      gatewayAddress,
		Token:               token,
		Workdir:             workdir,
		HeartbeatInterval:   10 * time.Second,
		ReconnectBackoffMin: 500 * time.Millisecond,
		ReconnectBackoffMax: 10 * time.Second,
		RequestTimeout:      30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("create runner: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nshutting down runner...")
		r.Stop()
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "runner %s connecting to %s...\n", runnerID, gatewayAddress)
	if err := r.Run(runCtx); err != nil && err != context.Canceled {
		return fmt.Errorf("runner: %w", err)
	}
	return nil
}
