package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"neo-code/internal/updater"
	"neo-code/internal/version"
)

type updateCommandOptions struct {
	IncludePrerelease bool
}

var runUpdateCommand = defaultUpdateCommandRunner
var doUpdate = updater.DoUpdate

var updateCommandTimeout = 5 * time.Minute

const updateTimeoutErrorTemplate = "\u66f4\u65b0\u8d85\u65f6\uff08%s\uff09\uff0c\u8bf7\u68c0\u67e5\u7f51\u7edc\u540e\u91cd\u8bd5"

// newUpdateCommand 创建 update 子命令并绑定升级参数。
func newUpdateCommand() *cobra.Command {
	options := &updateCommandOptions{}

	cmd := &cobra.Command{
		Use:          "update",
		Short:        "Update neocode to the latest release",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := runUpdateCommand(cmd.Context(), *options)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if !result.Updated {
				latest := displayVersionForTerminal(result.LatestVersion)
				_, _ = fmt.Fprintf(out, "Already up-to-date (latest: %s).\n", latest)
				return nil
			}

			current := displayVersionForTerminal(result.CurrentVersion)
			latest := displayVersionForTerminal(result.LatestVersion)
			_, _ = fmt.Fprintf(out, "Updated successfully: %s -> %s\n", current, latest)
			return nil
		},
	}

	cmd.Flags().BoolVar(&options.IncludePrerelease, "prerelease", false, "include prerelease versions")
	return cmd
}

// defaultUpdateCommandRunner 执行手动升级流程并返回升级结果。
func defaultUpdateCommandRunner(ctx context.Context, options updateCommandOptions) (updater.UpdateResult, error) {
	updateCtx, cancel := context.WithTimeout(ctx, updateCommandTimeout)
	defer cancel()

	result, err := doUpdate(updateCtx, updater.UpdateOptions{
		CurrentVersion:    version.Current(),
		IncludePrerelease: options.IncludePrerelease,
	})
	if err != nil {
		if errors.Is(updateCtx.Err(), context.DeadlineExceeded) {
			return updater.UpdateResult{}, fmt.Errorf(updateTimeoutErrorTemplate, updateCommandTimeout)
		}
		return updater.UpdateResult{}, err
	}
	return result, nil
}

// displayVersionForTerminal 清洗版本字符串并为不可用值提供统一回退文案。
func displayVersionForTerminal(raw string) string {
	version := sanitizeVersionForTerminal(raw)
	if version == "" {
		return "unknown"
	}
	return version
}
