package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newAdapterCommand 创建适配器命令组，统一承载外部协作平台桥接能力。
func newAdapterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "adapter",
		Short:        "Manage collaboration adapters",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
	}
	cmd.AddCommand(
		newFeishuAdapterCommand(),
	)
	return cmd
}

// newLegacyFeishuAdapterCommand 创建旧入口占位命令，提示用户迁移到新命令组。
func newLegacyFeishuAdapterCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "feishu-adapter",
		Short:        "Deprecated: use `adapter feishu`",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("command `feishu-adapter` has moved to `adapter feishu`")
		},
	}
}
