package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"neo-code/internal/app"
)

var launchRootProgram = defaultRootProgramLauncher
var newRootProgram = app.NewProgram
var runGlobalPreload = defaultGlobalPreload

// GlobalFlags 描述 CLI 根命令当前支持的全局参数。
type GlobalFlags struct {
	Workdir string
}

// Execute 负责执行 NeoCode 的 CLI 根命令。
func Execute(ctx context.Context) error {
	app.EnsureConsoleUTF8()
	return NewRootCommand().ExecuteContext(ctx)
}

// NewRootCommand 创建 NeoCode 的 CLI 根命令。
func NewRootCommand() *cobra.Command {
	settings := viper.New()
	flags := &GlobalFlags{}

	cmd := &cobra.Command{
		Use:          "neocode",
		Short:        "NeoCode coding agent",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if shouldSkipGlobalPreload(cmd) {
				return nil
			}
			return runGlobalPreload(cmd.Context())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.Workdir = strings.TrimSpace(settings.GetString("workdir"))
			return launchRootProgram(cmd.Context(), app.BootstrapOptions{
				Workdir: flags.Workdir,
			})
		},
	}

	cmd.PersistentFlags().String("workdir", "", "工作目录（覆盖本次运行工作区）")
	_ = settings.BindPFlag("workdir", cmd.PersistentFlags().Lookup("workdir"))
	cmd.AddCommand(
		newGatewayCommand(),
		newURLDispatchCommand(),
	)

	return cmd
}

// defaultRootProgramLauncher 负责在默认根命令路径下启动 TUI。
func defaultRootProgramLauncher(ctx context.Context, opts app.BootstrapOptions) (err error) {
	program, cleanup, err := newRootProgram(ctx, opts)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer func() {
			cleanupErr := cleanup()
			if cleanupErr == nil {
				return
			}
			if err == nil {
				err = cleanupErr
				return
			}
			err = errors.Join(err, cleanupErr)
		}()
	}
	_, err = program.Run()
	return err
}

// defaultGlobalPreload 预留给根命令全局预加载逻辑，默认不执行重度配置加载。
func defaultGlobalPreload(_ context.Context) error {
	return nil
}

// shouldSkipGlobalPreload 判断当前命令是否应跳过全局预加载逻辑。
func shouldSkipGlobalPreload(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cmd.Name()), "url-dispatch")
}
