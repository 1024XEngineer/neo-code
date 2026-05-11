package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"neo-code/internal/version"
)

type versionCommandOptions struct {
	IncludePrerelease bool
}

type versionCommandResult struct {
	CurrentVersion     string
	LatestVersion      string
	InstallableVersion string
	HasUpdate          bool
	Comparable         bool
	ComparableLatest   bool
	IncludePrerelease  bool
	CheckErr           error
}

var runVersionCommand = defaultVersionCommandRunner

var versionProbeTimeout = 3 * time.Second

// newVersionCommand 创建 version 子命令并绑定探测参数。
func newVersionCommand() *cobra.Command {
	options := &versionCommandOptions{}

	cmd := &cobra.Command{
		Use:          "version",
		Short:        "Show current version and check for updates",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVersionShortcut(cmd.Context(), cmd.OutOrStdout(), *options)
		},
	}

	cmd.Flags().BoolVar(&options.IncludePrerelease, "prerelease", false, "include prerelease versions")
	return cmd
}

// runVersionShortcut 复用 version 子命令核心逻辑，供顶层 --version/-v 与子命令统一输出。
func runVersionShortcut(ctx context.Context, out io.Writer, options ...versionCommandOptions) error {
	resolved := versionCommandOptions{}
	if len(options) > 0 {
		resolved = options[0]
	}
	result, err := runVersionCommand(ctx, resolved)
	if err != nil {
		return err
	}
	printVersionCommandResult(out, result)
	return nil
}

// defaultVersionCommandRunner 执行版本探测并构造可展示结果，探测失败不返回执行错误。
func defaultVersionCommandRunner(ctx context.Context, options versionCommandOptions) (versionCommandResult, error) {
	currentVersion := readCurrentVersion()
	result := versionCommandResult{
		CurrentVersion:    currentVersion,
		Comparable:        version.IsSemverRelease(currentVersion),
		IncludePrerelease: options.IncludePrerelease,
	}

	probe, err := runReleaseProbe(ctx, currentVersion, options.IncludePrerelease, versionProbeTimeout)
	if err != nil {
		result.CheckErr = err
		return result, nil
	}

	result.LatestVersion = strings.TrimSpace(probe.LatestVersion)
	result.InstallableVersion = strings.TrimSpace(probe.InstallableVersion)
	result.ComparableLatest = probe.ComparableLatest
	if result.Comparable {
		result.HasUpdate = probe.HasUpdate
	}
	return result, nil
}

// printVersionCommandResult 渲染 version 命令的输出文案，保证故障场景退出码仍为 0。
func printVersionCommandResult(out io.Writer, result versionCommandResult) {
	current := displayVersionForTerminal(result.CurrentVersion)
	_, _ = fmt.Fprintf(out, "Current version: %s\n", current)

	label := "Latest stable version"
	if result.IncludePrerelease {
		label = "Latest version (including prerelease)"
	}

	if result.CheckErr != nil {
		_, _ = fmt.Fprintf(out, "%s: check failed (%v)\n", label, result.CheckErr)
		return
	}

	latest := displayVersionForTerminal(result.LatestVersion)
	_, _ = fmt.Fprintf(out, "%s: %s\n", label, latest)
	installable := displayVersionForTerminal(result.InstallableVersion)

	if !result.Comparable {
		_, _ = fmt.Fprintln(out, "Comparison skipped: current build is non-semver.")
		return
	}
	if latest == "unknown" {
		_, _ = fmt.Fprintln(out, "Update status: unknown (latest version unavailable).")
		return
	}
	if !result.ComparableLatest {
		if result.HasUpdate {
			if installable != "unknown" {
				_, _ = fmt.Fprintf(out, "Update available for this platform: run neocode update (target: %s)\n", installable)
			} else {
				_, _ = fmt.Fprintln(out, "Update available for this platform: run neocode update")
			}
			_, _ = fmt.Fprintln(out, "Remote notice: a newer release exists but is currently not installable on this platform.")
			return
		}
		_, _ = fmt.Fprintln(out, "You are on the latest installable version for this platform.")
		_, _ = fmt.Fprintln(out, "Remote notice: a newer release exists but is currently not installable on this platform.")
		return
	}
	if result.HasUpdate {
		_, _ = fmt.Fprintln(out, "Update available: run neocode update")
		return
	}
	_, _ = fmt.Fprintln(out, "You are on the latest version.")
}
