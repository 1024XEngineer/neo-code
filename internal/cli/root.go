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
	"neo-code/internal/updater"
	"neo-code/internal/version"
)

var launchRootProgram = defaultRootProgramLauncher
var newRootProgram = app.NewProgram
var runGlobalPreload = defaultGlobalPreload
var runSilentUpdateCheck = defaultSilentUpdateCheck
var runReleaseProbe = defaultReleaseProbe
var readCurrentVersion = version.Current
var checkLatestRelease = updater.CheckLatest

const silentUpdateCheckTimeout = 3 * time.Second
const silentUpdateCheckDrainTimeout = 300 * time.Millisecond
const commandAnnotationSkipGlobalPreload = "neocode.skip_global_preload"
const commandAnnotationSkipSilentUpdateCheck = "neocode.skip_silent_update_check"

var ansiEscapeSequencePattern = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|][^\x07]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

var (
	silentUpdateCheckMu   sync.Mutex
	silentUpdateCheckDone <-chan struct{}
)

// GlobalFlags 描述根命令共享的全局启动参数。
type GlobalFlags struct {
	Workdir      string
	Session      string
	WakeInputB64 string
}

// Execute 执行 NeoCode 根命令入口，并在退出前等待静默更新检查收尾。
func Execute(ctx context.Context) error {
	app.EnsureConsoleUTF8()
	_ = ConsumeUpdateNotice()
	setSilentUpdateCheckDone(nil)

	err := NewRootCommand().ExecuteContext(ctx)
	waitSilentUpdateCheckDone(silentUpdateCheckDrainTimeout)
	return err
}

// NewRootCommand 构建 NeoCode 的根命令及全局参数绑定。
func NewRootCommand() *cobra.Command {
	settings := viper.New()
	flags := &GlobalFlags{}
	selectionResolver := newRuntimeSelectionServiceResolver()

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
			flags.Session = strings.TrimSpace(settings.GetString("session"))
			flags.WakeInputB64 = strings.TrimSpace(settings.GetString("wake-input-b64"))
			return launchRootProgram(cmd.Context(), app.BootstrapOptions{
				Workdir:      flags.Workdir,
				SessionID:    flags.Session,
				WakeInputB64: flags.WakeInputB64,
			})
		},
	}
	cmd.PersistentFlags().String("workdir", "", "workdir override for current run")
	cmd.PersistentFlags().String("session", "", "session id to hydrate on startup")
	cmd.PersistentFlags().String("wake-input-b64", "", "internal wake startup payload")
	_ = cmd.PersistentFlags().MarkHidden("wake-input-b64")
	_ = settings.BindPFlag("workdir", cmd.PersistentFlags().Lookup("workdir"))
	_ = settings.BindPFlag("session", cmd.PersistentFlags().Lookup("session"))
	_ = settings.BindPFlag("wake-input-b64", cmd.PersistentFlags().Lookup("wake-input-b64"))
	cmd.AddCommand(
		newGatewayCommand(),
		newFeishuAdapterCommand(),
		newWebCommand(),
		newDaemonCommand(),
		newShellCommand(),
		newDiagCommand(),
		newMigrateCommand(),
		newVersionCommand(),
		newUpdateCommand(),
		newProviderCommandWithResolver(selectionResolver),
		newModelCommandWithResolver(selectionResolver),
		newUseCommandWithResolver(selectionResolver),
	)

	return cmd
}

// defaultRootProgramLauncher 负责创建并运行 TUI Program，同时保证清理函数被正确执行。
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

// defaultGlobalPreload 执行全局预加载钩子；当前仅做上下文取消检查。
func defaultGlobalPreload(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// defaultSilentUpdateCheck 在后台静默检查是否有新版本，并写入一次性升级提示。
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

		result, err := runReleaseProbe(parent, currentVersion, false, silentUpdateCheckTimeout)
		if err != nil || !result.HasUpdate {
			return
		}

		installableVersion := sanitizeVersionForTerminal(result.InstallableVersion)
		if installableVersion == "" {
			installableVersion = sanitizeVersionForTerminal(result.LatestVersion)
		}
		if installableVersion == "" {
			return
		}
		setUpdateNotice(fmt.Sprintf("\u53d1\u73b0\u65b0\u7248\u672c: %s\uff0c\u8fd0\u884c neocode update \u5373\u53ef\u5347\u7ea7", installableVersion))
	}(parentCtx, currentVersion, done)
}

// shouldSkipGlobalPreload 判断当前子命令是否跳过全局预加载。
func shouldSkipGlobalPreload(cmd *cobra.Command) bool {
	return commandAnnotationEnabled(cmd, commandAnnotationSkipGlobalPreload)
}

// shouldSkipSilentUpdateCheck 判断当前子命令是否跳过静默更新检查。
func shouldSkipSilentUpdateCheck(cmd *cobra.Command) bool {
	switch normalizedCommandName(cmd) {
	case "update", "version":
		return true
	default:
		return commandAnnotationEnabled(cmd, commandAnnotationSkipSilentUpdateCheck)
	}
}

// sanitizeVersionForTerminal 清理版本号中的 ANSI 控制字符与不可打印字符，避免污染终端输出。
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

// normalizedCommandName 返回小写且去空白后的命令名，便于统一比较。
func normalizedCommandName(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(cmd.Name()))
}

// commandAnnotationEnabled 沿当前命令链查找布尔注解，供轻量命令跳过全局启动副作用。
func commandAnnotationEnabled(cmd *cobra.Command, key string) bool {
	for current := cmd; current != nil; current = current.Parent() {
		if strings.EqualFold(strings.TrimSpace(current.Annotations[key]), "true") {
			return true
		}
	}
	return false
}

// setSilentUpdateCheckDone 原子地更新静默检查完成信号通道。
func setSilentUpdateCheckDone(done <-chan struct{}) {
	silentUpdateCheckMu.Lock()
	silentUpdateCheckDone = done
	silentUpdateCheckMu.Unlock()
}

// waitSilentUpdateCheckDone 在给定超时时间内等待静默更新检查结束，超时后直接返回。
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
