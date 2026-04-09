package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"neo-code/internal/app"
)

var launchRootProgram = defaultRootProgramLauncher
var newRootProgram = app.NewProgram

type workspaceRelaunchModel interface {
	PendingWorkspaceWorkdir() string
}

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
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.Workdir = strings.TrimSpace(settings.GetString("workdir"))
			return launchRootProgram(cmd.Context(), app.BootstrapOptions{
				Workdir: flags.Workdir,
			})
		},
	}

	cmd.PersistentFlags().String("workdir", "", "工作目录（覆盖本次运行工作区）")
	_ = settings.BindPFlag("workdir", cmd.PersistentFlags().Lookup("workdir"))

	return cmd
}

// defaultRootProgramLauncher 负责在默认根命令路径下启动 TUI。
func defaultRootProgramLauncher(ctx context.Context, opts app.BootstrapOptions) error {
	current := opts
	for {
		program, err := newRootProgram(ctx, current)
		if err != nil {
			return err
		}
		model, err := program.Run()
		if err != nil {
			return err
		}

		relaunchModel, ok := model.(workspaceRelaunchModel)
		if !ok {
			return nil
		}

		nextWorkdir := strings.TrimSpace(relaunchModel.PendingWorkspaceWorkdir())
		if nextWorkdir == "" {
			return nil
		}
		current.Workdir = nextWorkdir
	}
}
