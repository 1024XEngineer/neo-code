package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
)

type providerAddOptions struct {
	Driver                string
	URL                   string
	APIKeyEnv             string
	DiscoveryEndpointPath string
}

var (
	runProviderAddCommand = defaultProviderAddCommandRunner
	runProviderLsCommand  = defaultProviderLsCommandRunner
	runProviderRmCommand  = defaultProviderRmCommandRunner
)

// newProviderCommand 创建 provider 命令组，管理自定义供应商配置。
func newProviderCommand() *cobra.Command {
	return newProviderCommandWithResolver(newRuntimeSelectionServiceResolver())
}

// newProviderCommandWithResolver 基于注入的选择服务解析器构建 provider 命令组。
func newProviderCommandWithResolver(resolver selectionServiceResolver) *cobra.Command {
	if resolver == nil {
		resolver = newRuntimeSelectionServiceResolver()
	}

	cmd := &cobra.Command{
		Use:          "provider",
		Short:        "Manage custom providers",
		SilenceUsage: true,
	}

	cmd.AddCommand(
		newProviderAddCommandWithResolver(resolver),
		newProviderLsCommand(),
		newProviderRmCommand(),
	)

	return cmd
}

// newProviderAddCommand 创建 provider add 子命令。
func newProviderAddCommand() *cobra.Command {
	return newProviderAddCommandWithResolver(newRuntimeSelectionServiceResolver())
}

// newProviderAddCommandWithResolver 创建支持依赖注入的 provider add 子命令。
func newProviderAddCommandWithResolver(resolver selectionServiceResolver) *cobra.Command {
	if resolver == nil {
		resolver = newRuntimeSelectionServiceResolver()
	}

	var opts providerAddOptions
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a custom provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := resolver.Resolve(cmd)
			if err != nil {
				return err
			}
			return runProviderAddCommand(cmd, svc, args[0], opts)
		},
	}

	cmd.Flags().StringVar(&opts.Driver, "driver", "", "Provider driver (e.g., openaicompat)")
	cmd.Flags().StringVar(&opts.URL, "url", "", "Provider API base URL")
	cmd.Flags().StringVar(&opts.APIKeyEnv, "api-key-env", "", "Environment variable for API key")
	cmd.Flags().StringVar(&opts.DiscoveryEndpointPath, "discovery-endpoint", "", "Discovery endpoint path (optional)")

	_ = cmd.MarkFlagRequired("driver")
	_ = cmd.MarkFlagRequired("url")
	_ = cmd.MarkFlagRequired("api-key-env")

	return cmd
}

// defaultProviderAddCommandRunner 通过选择服务创建自定义 provider，确保冲突检测与回滚链路生效。
func defaultProviderAddCommandRunner(
	cmd *cobra.Command,
	svc SelectionService,
	name string,
	opts providerAddOptions,
) error {
	if svc == nil {
		return fmt.Errorf("selection service is unavailable")
	}

	apiKeyEnv := strings.TrimSpace(opts.APIKeyEnv)
	if apiKeyEnv == "" {
		return fmt.Errorf("api-key-env 不能为空")
	}
	apiKey := strings.TrimSpace(os.Getenv(apiKeyEnv))
	if apiKey == "" {
		return fmt.Errorf("请先设置 $%s 环境变量", apiKeyEnv)
	}

	driver := strings.TrimSpace(opts.Driver)
	discoveryPath := strings.TrimSpace(opts.DiscoveryEndpointPath)
	if discoveryPath == "" && strings.EqualFold(driver, "openaicompat") {
		discoveryPath = "/v1/models"
	}

	input := configstate.CreateCustomProviderInput{
		Name:                  strings.TrimSpace(name),
		Driver:                driver,
		BaseURL:               strings.TrimSpace(opts.URL),
		APIKeyEnv:             apiKeyEnv,
		APIKey:                apiKey,
		ModelSource:           config.ModelSourceDiscover,
		DiscoveryEndpointPath: discoveryPath,
	}

	selection, err := svc.CreateCustomProvider(cmd.Context(), input)
	if err != nil {
		return err
	}

	providerName := strings.TrimSpace(selection.ProviderID)
	if providerName == "" {
		providerName = strings.TrimSpace(name)
	}
	modelName := strings.TrimSpace(selection.ModelID)
	if modelName == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "✅ 供应商 %s 添加成功\n", providerName)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✅ 供应商 %s 添加成功，当前模型: %s\n", providerName, modelName)
	return nil
}

func newProviderLsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List all providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProviderLsCommand(cmd)
		},
	}
}

func defaultProviderLsCommandRunner(cmd *cobra.Command) error {
	var workdir string
	if f := cmd.Flag("workdir"); f != nil {
		workdir = f.Value.String()
	}
	loader := config.NewLoader(workdir, config.StaticDefaults())
	cfg, err := loader.Load(cmd.Context())
	if err != nil {
		return err
	}

	for _, p := range cfg.Providers {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s (Driver: %s, Source: %s)\n", p.Name, p.Driver, p.Source)
	}
	return nil
}

func newProviderRmCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a custom provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProviderRmCommand(cmd, args[0])
		},
	}
}

func defaultProviderRmCommandRunner(cmd *cobra.Command, name string) error {
	var workdir string
	if f := cmd.Flag("workdir"); f != nil {
		workdir = f.Value.String()
	}
	loader := config.NewLoader(workdir, config.StaticDefaults())
	baseDir := loader.BaseDir()

	if err := config.DeleteCustomProvider(baseDir, name); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "✅ 供应商 %s 已删除\n", name)
	return nil
}
