package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	neturl "net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"neo-code/internal/gateway/adapters/urlscheme"
)

var (
	runDaemonServeCommand     = defaultDaemonServeCommandRunner
	runDaemonInstallCommand   = defaultDaemonInstallCommandRunner
	runDaemonUninstallCommand = defaultDaemonUninstallCommandRunner
	runDaemonStatusCommand    = defaultDaemonStatusCommandRunner
	runDaemonEncodeCommand    = defaultDaemonEncodeCommandRunner

	serveHTTPDaemon     = urlscheme.ServeHTTPDaemon
	installHTTPDaemon   = urlscheme.InstallHTTPDaemon
	uninstallHTTPDaemon = urlscheme.UninstallHTTPDaemon
	getHTTPDaemonStatus = urlscheme.GetHTTPDaemonStatus
)

type daemonServeCommandOptions struct {
	ListenAddress        string
	GatewayListenAddress string
}

type daemonInstallCommandOptions struct {
	ListenAddress string
	Executable    string
}

type daemonStatusCommandOptions struct {
	ListenAddress string
}

type daemonEncodeCommandOptions struct {
	Action        string
	Prompt        string
	Path          string
	Workdir       string
	SessionID     string
	ListenAddress string
}

// newDaemonCommand 创建 daemon 命令组，承载 HTTP 唤醒服务与自启动管理能力。
func newDaemonCommand() *cobra.Command {
	command := &cobra.Command{
		Use:          "daemon",
		Short:        "Manage NeoCode HTTP daemon",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		Annotations: map[string]string{
			commandAnnotationSkipGlobalPreload:     "true",
			commandAnnotationSkipSilentUpdateCheck: "true",
		},
	}
	command.AddCommand(
		newDaemonServeCommand(),
		newDaemonInstallCommand(),
		newDaemonUninstallCommand(),
		newDaemonStatusCommand(),
		newDaemonEncodeCommand(),
	)
	return command
}

// newDaemonServeCommand 创建 daemon serve 子命令。
func newDaemonServeCommand() *cobra.Command {
	options := &daemonServeCommandOptions{}
	command := &cobra.Command{
		Use:           "serve",
		Short:         "Start HTTP daemon to accept /run and /review wake links",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonServeCommand(cmd.Context(), daemonServeCommandOptions{
				ListenAddress:        strings.TrimSpace(options.ListenAddress),
				GatewayListenAddress: strings.TrimSpace(options.GatewayListenAddress),
			})
		},
	}
	command.Flags().StringVar(
		&options.ListenAddress,
		"listen",
		urlscheme.DefaultHTTPDaemonListenAddress,
		"http daemon listen address",
	)
	command.Flags().StringVar(
		&options.GatewayListenAddress,
		"gateway-listen",
		"",
		"gateway ipc listen address override",
	)
	return command
}

// newDaemonInstallCommand 创建 daemon install 子命令。
func newDaemonInstallCommand() *cobra.Command {
	options := &daemonInstallCommandOptions{}
	command := &cobra.Command{
		Use:           "install",
		Short:         "Install HTTP daemon autostart and hosts alias",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonInstallCommand(cmd.Context(), daemonInstallCommandOptions{
				ListenAddress: strings.TrimSpace(options.ListenAddress),
				Executable:    strings.TrimSpace(options.Executable),
			})
		},
	}
	command.Flags().StringVar(
		&options.ListenAddress,
		"listen",
		urlscheme.DefaultHTTPDaemonListenAddress,
		"http daemon listen address",
	)
	command.Flags().StringVar(
		&options.Executable,
		"executable",
		"",
		"absolute neocode executable path override",
	)
	return command
}

// newDaemonUninstallCommand 创建 daemon uninstall 子命令。
func newDaemonUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "uninstall",
		Short:         "Uninstall HTTP daemon autostart",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonUninstallCommand(cmd.Context())
		},
	}
}

// newDaemonStatusCommand 创建 daemon status 子命令。
func newDaemonStatusCommand() *cobra.Command {
	options := &daemonStatusCommandOptions{}
	command := &cobra.Command{
		Use:           "status",
		Short:         "Show HTTP daemon status",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStatusCommand(cmd.Context(), daemonStatusCommandOptions{
				ListenAddress: strings.TrimSpace(options.ListenAddress),
			})
		},
	}
	command.Flags().StringVar(
		&options.ListenAddress,
		"listen",
		urlscheme.DefaultHTTPDaemonListenAddress,
		"http daemon listen address",
	)
	return command
}

// newDaemonEncodeCommand 创建 daemon encode 子命令组。
func newDaemonEncodeCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "encode",
		Short:         "Generate clickable HTTP wake URL",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
	}
	command.AddCommand(
		newDaemonEncodeRunCommand(),
		newDaemonEncodeReviewCommand(),
	)
	return command
}

// newDaemonEncodeRunCommand 创建 daemon encode run 子命令。
func newDaemonEncodeRunCommand() *cobra.Command {
	options := &daemonEncodeCommandOptions{Action: "run"}
	command := &cobra.Command{
		Use:           "run",
		Short:         "Encode wake run URL",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonEncodeCommand(cmd.Context(), daemonEncodeCommandOptions{
				Action:        "run",
				Prompt:        strings.TrimSpace(options.Prompt),
				Workdir:       strings.TrimSpace(options.Workdir),
				SessionID:     strings.TrimSpace(options.SessionID),
				ListenAddress: strings.TrimSpace(options.ListenAddress),
			})
		},
	}
	command.Flags().StringVar(&options.Prompt, "prompt", "", "run prompt text")
	command.Flags().StringVar(&options.Workdir, "workdir", "", "workspace absolute path")
	command.Flags().StringVar(&options.SessionID, "session-id", "", "existing session id for resume-only wake")
	command.Flags().StringVar(
		&options.ListenAddress,
		"listen",
		urlscheme.DefaultHTTPDaemonListenAddress,
		"http daemon listen address",
	)
	return command
}

// newDaemonEncodeReviewCommand 创建 daemon encode review 子命令。
func newDaemonEncodeReviewCommand() *cobra.Command {
	options := &daemonEncodeCommandOptions{Action: "review"}
	command := &cobra.Command{
		Use:           "review",
		Short:         "Encode wake review URL",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonEncodeCommand(cmd.Context(), daemonEncodeCommandOptions{
				Action:        "review",
				Path:          strings.TrimSpace(options.Path),
				Workdir:       strings.TrimSpace(options.Workdir),
				SessionID:     strings.TrimSpace(options.SessionID),
				ListenAddress: strings.TrimSpace(options.ListenAddress),
			})
		},
	}
	command.Flags().StringVar(&options.Path, "path", "", "review file path")
	command.Flags().StringVar(&options.Workdir, "workdir", "", "workspace absolute path")
	command.Flags().StringVar(&options.SessionID, "session-id", "", "existing session id for resume-only wake")
	command.Flags().StringVar(
		&options.ListenAddress,
		"listen",
		urlscheme.DefaultHTTPDaemonListenAddress,
		"http daemon listen address",
	)
	return command
}

// defaultDaemonServeCommandRunner 启动 HTTP daemon 主循环。
func defaultDaemonServeCommandRunner(ctx context.Context, options daemonServeCommandOptions) error {
	if serveHTTPDaemon == nil {
		return errors.New("http daemon server is unavailable")
	}
	return serveHTTPDaemon(ctx, urlscheme.HTTPDaemonServeOptions{
		ListenAddress:        options.ListenAddress,
		GatewayListenAddress: options.GatewayListenAddress,
	})
}

// defaultDaemonInstallCommandRunner 安装 daemon 自启动并输出结构化结果。
func defaultDaemonInstallCommandRunner(ctx context.Context, options daemonInstallCommandOptions) error {
	if installHTTPDaemon == nil {
		return errors.New("http daemon installer is unavailable")
	}

	executablePath := strings.TrimSpace(options.Executable)
	if executablePath == "" {
		if resolveExecutablePath == nil {
			return errors.New("resolve current executable is unavailable")
		}
		resolvedPath, resolveErr := resolveExecutablePath()
		if resolveErr != nil {
			return fmt.Errorf("resolve current executable: %w", resolveErr)
		}
		executablePath = strings.TrimSpace(resolvedPath)
	}
	if executablePath == "" {
		return errors.New("resolve current executable: empty path")
	}

	result, err := installHTTPDaemon(urlscheme.HTTPDaemonInstallOptions{
		ExecutablePath: executablePath,
		ListenAddress:  options.ListenAddress,
	})
	if err != nil {
		return err
	}
	return encodeJSONLine(os.Stdout, map[string]any{
		"status":         "ok",
		"listen_address": result.ListenAddress,
		"autostart_mode": result.AutostartMode,
		"hosts_warning":  strings.TrimSpace(result.HostsWarning),
	})
}

// defaultDaemonUninstallCommandRunner 卸载 daemon 自启动并输出结构化结果。
func defaultDaemonUninstallCommandRunner(context.Context) error {
	if uninstallHTTPDaemon == nil {
		return errors.New("http daemon uninstaller is unavailable")
	}
	if err := uninstallHTTPDaemon(); err != nil {
		return err
	}
	return encodeJSONLine(os.Stdout, map[string]any{
		"status": "ok",
	})
}

// defaultDaemonStatusCommandRunner 查询 daemon 状态并输出结构化结果。
func defaultDaemonStatusCommandRunner(ctx context.Context, options daemonStatusCommandOptions) error {
	if getHTTPDaemonStatus == nil {
		return errors.New("http daemon status provider is unavailable")
	}
	status, err := getHTTPDaemonStatus(ctx, urlscheme.HTTPDaemonStatusOptions{
		ListenAddress: options.ListenAddress,
	})
	if err != nil {
		return err
	}
	return encodeJSONLine(os.Stdout, map[string]any{
		"status":                 "ok",
		"listen_address":         status.ListenAddress,
		"running":                status.Running,
		"autostart_configured":   status.AutostartConfigured,
		"autostart_mode":         status.AutostartMode,
		"hosts_alias_configured": status.HostsAliasConfigured,
	})
}

// defaultDaemonEncodeCommandRunner 生成 URL 编码后的 daemon 唤醒链接，并输出单行 URL。
func defaultDaemonEncodeCommandRunner(_ context.Context, options daemonEncodeCommandOptions) error {
	urlText, err := buildDaemonEncodedWakeURL(options)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(os.Stdout, urlText)
	return nil
}

// buildDaemonEncodedWakeURL 按 action 组装标准化的可点击 HTTP 唤醒链接。
func buildDaemonEncodedWakeURL(options daemonEncodeCommandOptions) (string, error) {
	action := strings.ToLower(strings.TrimSpace(options.Action))
	if action != "run" && action != "review" {
		return "", fmt.Errorf("unsupported encode action: %s", options.Action)
	}

	sessionID := strings.TrimSpace(options.SessionID)
	workdir := strings.TrimSpace(options.Workdir)
	query := neturl.Values{}
	switch action {
	case "run":
		prompt := strings.TrimSpace(options.Prompt)
		if sessionID == "" && prompt == "" {
			return "", errors.New("missing required flag: --prompt")
		}
		if prompt != "" {
			query.Set("prompt", prompt)
		}
	case "review":
		path := strings.TrimSpace(options.Path)
		if sessionID == "" && path == "" {
			return "", errors.New("missing required flag: --path")
		}
		if sessionID == "" && workdir == "" {
			return "", errors.New("missing required flag: --workdir (or provide --session-id)")
		}
		if path != "" {
			query.Set("path", path)
		}
	}

	if workdir != "" {
		query.Set("workdir", workdir)
	}
	if sessionID != "" {
		query.Set("session_id", sessionID)
	}

	hostPort := daemonEncodeHostPort(options.ListenAddress)
	return (&neturl.URL{
		Scheme:   "http",
		Host:     hostPort,
		Path:     "/" + action,
		RawQuery: query.Encode(),
	}).String(), nil
}

// daemonEncodeHostPort 将监听地址归一为对外文档可点击的 neocode 域名地址。
func daemonEncodeHostPort(listenAddress string) string {
	normalized := strings.TrimSpace(listenAddress)
	if normalized == "" {
		normalized = urlscheme.DefaultHTTPDaemonListenAddress
	}
	if _, port, err := net.SplitHostPort(normalized); err == nil && strings.TrimSpace(port) != "" {
		return net.JoinHostPort(urlscheme.DaemonHostsAlias, strings.TrimSpace(port))
	}
	if strings.IndexByte(normalized, ':') < 0 && normalized != "" {
		return net.JoinHostPort(urlscheme.DaemonHostsAlias, normalized)
	}
	return net.JoinHostPort(urlscheme.DaemonHostsAlias, "18921")
}
