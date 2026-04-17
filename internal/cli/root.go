package cli

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"neo-code/internal/app"
	"neo-code/internal/config"
	"neo-code/internal/updater"
	"neo-code/internal/version"
)

var launchRootProgram = defaultRootProgramLauncher
var newRootProgram = app.NewProgram
var runGlobalPreload = defaultGlobalPreload
var runSilentUpdateCheck = defaultSilentUpdateCheck
var readCurrentVersion = version.Current
var checkLatestRelease = updater.CheckLatest

const silentUpdateCheckTimeout = 3 * time.Second
const silentUpdateCheckDrainTimeout = 300 * time.Millisecond

var ansiEscapeSequencePattern = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

var (
	silentUpdateCheckMu   sync.Mutex
	silentUpdateCheckDone <-chan struct{}
)

// GlobalFlags 描述 CLI 根命令当前支持的全局参数。
type GlobalFlags struct {
	Workdir string
}

// Execute 负责执行 NeoCode 的 CLI 根命令。
func Execute(ctx context.Context) error {
	app.EnsureConsoleUTF8()
	_ = ConsumeUpdateNotice()
	setSilentUpdateCheckDone(nil)

	err := NewRootCommand().ExecuteContext(ctx)
	waitSilentUpdateCheckDone(silentUpdateCheckDrainTimeout)
	return err
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
			if err := runGlobalPreload(cmd.Context()); err != nil {
				return err
			}
			if !shouldSkipSilentUpdateCheck(cmd) {
				runSilentUpdateCheck(cmd.Context())
			}
			return nil
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
		newUpdateCommand(),
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

// defaultGlobalPreload runs lightweight startup preloads such as persisted env.
func defaultGlobalPreload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return config.LoadPersistedEnv("")
}

// defaultSilentUpdateCheck 在后台异步检查新版本并缓存退出后提示文案。
func defaultSilentUpdateCheck(ctx context.Context) {
	currentVersion := readCurrentVersion()
	if !version.IsSemverRelease(currentVersion) {
		setSilentUpdateCheckDone(nil)
		return
	}
	parentCtx := context.WithoutCancel(ctx)
	done := make(chan struct{})
	setSilentUpdateCheckDone(done)

	go func(parent context.Context, currentVersion string, done chan struct{}) {
		defer close(done)

		checkCtx, cancel := context.WithTimeout(parent, silentUpdateCheckTimeout)
		defer cancel()

		result, err := checkLatestRelease(checkCtx, updater.CheckOptions{
			CurrentVersion:    currentVersion,
			IncludePrerelease: false,
		})
		if err != nil || !result.HasUpdate {
			return
		}

		latestVersion := sanitizeVersionForTerminal(result.LatestVersion)
		if latestVersion == "" {
			return
		}
		setUpdateNotice(fmt.Sprintf("\u53d1\u73b0\u65b0\u7248\u672c: %s\uff0c\u8fd0\u884c neocode update \u5373\u53ef\u5347\u7ea7", latestVersion))
	}(parentCtx, currentVersion, done)
}

// shouldSkipGlobalPreload 判断当前命令是否应跳过全局预加载逻辑。
func shouldSkipGlobalPreload(cmd *cobra.Command) bool {
	return normalizedCommandName(cmd) == "url-dispatch"
}

// shouldSkipSilentUpdateCheck 判断当前命令是否应跳过静默更新检测。
func shouldSkipSilentUpdateCheck(cmd *cobra.Command) bool {
	switch normalizedCommandName(cmd) {
	case "url-dispatch", "update":
		return true
	default:
		return false
	}
}

// sanitizeVersionForTerminal 清洗远端版本字符串，避免 ANSI 控制序列或不可见字符污染终端输出。
func sanitizeVersionForTerminal(version string) string {
	cleaned := ansiEscapeSequencePattern.ReplaceAllString(version, "")
	var builder strings.Builder
	builder.Grow(len(cleaned))
	for _, ch := range cleaned {
		if ch >= 0x20 && ch <= 0x7e {
			builder.WriteRune(ch)
		}
	}
	return strings.TrimSpace(builder.String())
}

// normalizedCommandName 返回标准化后的命令名，统一处理空命令与大小写。
func normalizedCommandName(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(cmd.Name()))
}

// setSilentUpdateCheckDone 保存当前静默检测任务的完成信号通道。
func setSilentUpdateCheckDone(done <-chan struct{}) {
	silentUpdateCheckMu.Lock()
	silentUpdateCheckDone = done
	silentUpdateCheckMu.Unlock()
}

// waitSilentUpdateCheckDone 在命令退出阶段等待静默检测短暂收口，降低提示丢失概率。
func waitSilentUpdateCheckDone(timeout time.Duration) {
	if timeout <= 0 {
		return
	}

	silentUpdateCheckMu.Lock()
	done := silentUpdateCheckDone
	silentUpdateCheckMu.Unlock()
	if done == nil {
		return
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}
