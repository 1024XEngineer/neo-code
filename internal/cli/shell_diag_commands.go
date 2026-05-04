package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"neo-code/internal/ptyproxy"
)

var (
	runShellCommand        = defaultShellCommandRunner
	runShellInitCommand    = defaultShellInitCommandRunner
	runDiagCommand         = defaultDiagCommandRunner
	runDiagAutoCommand     = defaultDiagAutoCommandRunner
	runDiagDiagnoseCommand = defaultDiagCommandRunner
	runManualShellProxy    = ptyproxy.RunManualShell
	sendDiagnoseSignalFn   = ptyproxy.SendDiagnoseSignal
	sendAutoModeSignalFn   = ptyproxy.SendAutoModeSignal
	queryAutoModeFn        = ptyproxy.QueryAutoMode
	buildShellInitScript   = ptyproxy.BuildShellInitScript
	readDiagEnvValue       = os.Getenv
	resolveLatestDiagPath  = ptyproxy.ResolveLatestRunDiagSocketPath
)

type shellCommandOptions struct {
	Workdir              string
	Shell                string
	SocketPath           string
	GatewayListenAddress string
	GatewayTokenFile     string
	Init                 bool
}

type diagCommandOptions struct {
	SocketPath string
}

type diagAutoCommandOptions struct {
	SocketPath string
	Enabled    bool
	QueryOnly  bool
}

// newShellCommand 创建终端代理入口：支持启动代理或输出 shell integration 初始化脚本。
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
				SocketPath:           strings.TrimSpace(options.SocketPath),
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
	command.Flags().StringVar(&options.SocketPath, "socket", "", "diagnose unix socket path override")
	command.Flags().StringVar(&options.GatewayListenAddress, "gateway-listen", "", "gateway listen address override")
	command.Flags().StringVar(&options.GatewayTokenFile, "gateway-token-file", "", "gateway token file override")
	command.Flags().BoolVar(&options.Init, "init", false, "print shell integration script")
	return command
}

// newDiagCommand 创建诊断命令组：默认触发手动诊断，支持 auto on/off 开关控制。
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
		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath, err := resolveDiagSocketPath(options.SocketPath)
			if err != nil {
				return err
			}
			return runDiagCommand(cmd.Context(), diagCommandOptions{SocketPath: socketPath})
		},
	}
	command.Flags().StringVar(&options.SocketPath, "socket", "", "diagnose unix socket path override")
	command.AddCommand(
		newDiagAutoCommand(),
		newDiagDiagnoseCommand(),
	)
	return command
}

// newDiagAutoCommand 创建 auto on/off 子命令。
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
			socketPath, err := resolveDiagSocketPath(options.SocketPath)
			if err != nil {
				return err
			}
			return runDiagAutoCommand(cmd.Context(), diagAutoCommandOptions{
				SocketPath: socketPath,
				Enabled:    options.Enabled,
				QueryOnly:  options.QueryOnly,
			}, cmd.OutOrStdout())
		},
	}
	command.Flags().StringVar(&options.SocketPath, "socket", "", "diagnose unix socket path override")
	return command
}

// newDiagDiagnoseCommand 创建 diagnose 子命令，便于显式表达一次手动诊断请求。
func newDiagDiagnoseCommand() *cobra.Command {
	options := &diagCommandOptions{}
	command := &cobra.Command{
		Use:          "diagnose",
		Short:        "Trigger one manual diagnosis in current neocode shell",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			socketPath, err := resolveDiagSocketPath(options.SocketPath)
			if err != nil {
				return err
			}
			return runDiagDiagnoseCommand(cmd.Context(), diagCommandOptions{SocketPath: socketPath})
		},
	}
	command.Flags().StringVar(&options.SocketPath, "socket", "", "diagnose unix socket path override")
	return command
}

// defaultShellCommandRunner 组装 PTY 代理参数并启动 shell。
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
		SocketPath:           strings.TrimSpace(options.SocketPath),
		GatewayListenAddress: strings.TrimSpace(options.GatewayListenAddress),
		GatewayTokenFile:     strings.TrimSpace(options.GatewayTokenFile),
		Stdin:                stdin,
		Stdout:               stdout,
		Stderr:               stderr,
	})
}

// defaultShellInitCommandRunner 输出 shell integration 初始化脚本。
func defaultShellInitCommandRunner(_ context.Context, options shellCommandOptions, stdout io.Writer) error {
	if stdout == nil {
		return nil
	}
	_, err := io.WriteString(stdout, buildShellInitScript(strings.TrimSpace(options.Shell))+"\n")
	return err
}

// defaultDiagCommandRunner 向 shell 代理发送一次手动诊断信令。
func defaultDiagCommandRunner(ctx context.Context, options diagCommandOptions) error {
	return sendDiagnoseSignalFn(ctx, strings.TrimSpace(options.SocketPath))
}

// defaultDiagAutoCommandRunner 向 shell 代理发送 auto 模式切换信令。
func defaultDiagAutoCommandRunner(ctx context.Context, options diagAutoCommandOptions, stdout io.Writer) error {
	if options.QueryOnly {
		enabled, err := queryAutoModeFn(ctx, strings.TrimSpace(options.SocketPath))
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

	if err := sendAutoModeSignalFn(ctx, strings.TrimSpace(options.SocketPath), options.Enabled); err != nil {
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

// resolveDiagSocketPath 按“--socket > NEOCODE_DIAG_SOCKET > 最新运行目录 socket”解析目标路径。
func resolveDiagSocketPath(socketFlag string) (string, error) {
	if socketPath := strings.TrimSpace(socketFlag); socketPath != "" {
		return socketPath, nil
	}
	if envValue := strings.TrimSpace(readDiagEnvValue(ptyproxy.DiagSocketEnv)); envValue != "" {
		return envValue, nil
	}
	if discoveredPath, err := resolveLatestDiagPath(); err == nil && strings.TrimSpace(discoveredPath) != "" {
		return strings.TrimSpace(discoveredPath), nil
	}
	return "", errors.New(
		fmt.Sprintf(
			"diagnose socket is empty: use --socket or set %s in your shell environment",
			ptyproxy.DiagSocketEnv,
		),
	)
}
