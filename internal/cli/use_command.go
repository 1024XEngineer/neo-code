package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	configstate "neo-code/internal/config/state"
)

var runUseCommand = defaultUseCommandRunner

type useCommandOptions struct {
	Model string
}

// newUseCommand 创建 use 命令，用于全局切换选中的 provider，并可选指定模型。
func newUseCommand() *cobra.Command {
	return newUseCommandWithResolver(newRuntimeSelectionServiceResolver())
}

// newUseCommandWithResolver 创建支持依赖注入的 use 命令。
func newUseCommandWithResolver(resolver selectionServiceResolver) *cobra.Command {
	if resolver == nil {
		resolver = newRuntimeSelectionServiceResolver()
	}
	var opts useCommandOptions
	cmd := &cobra.Command{
		Use:   "use <provider>",
		Short: "Switch to a specific provider (and optionally a model)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := resolver.Resolve(cmd)
			if err != nil {
				return err
			}
			return runUseCommand(cmd, svc, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Model, "model", "", "model to select for the provider")
	return cmd
}

// defaultUseCommandRunner 执行 provider 切换逻辑，并在指定 --model 时同步设置 current_model。
func defaultUseCommandRunner(cmd *cobra.Command, svc SelectionService, name string, opts useCommandOptions) error {
	if svc == nil {
		return fmt.Errorf("selection service is unavailable")
	}

	providerName := strings.TrimSpace(name)
	model := strings.TrimSpace(opts.Model)

	var (
		selection configstate.Selection
		err       error
	)
	if model != "" {
		selection, err = svc.SelectProviderWithModel(cmd.Context(), providerName, model)
		if err != nil {
			if errors.Is(err, configstate.ErrModelNotFound) {
				return fmt.Errorf("provider %q has no model %q: %w", providerName, model, err)
			}
			return err
		}
	} else {
		selection, err = svc.SelectProvider(cmd.Context(), providerName)
		if err != nil {
			return err
		}
	}

	activeProvider := strings.TrimSpace(selection.ProviderID)
	if activeProvider == "" {
		activeProvider = providerName
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "✅ 已全局切换到供应商: %s\n", activeProvider)
	if model != "" {
		activeModel := strings.TrimSpace(selection.ModelID)
		if activeModel == "" {
			activeModel = model
		}
		fmt.Fprintf(out, "✅ 已设置模型: %s\n", activeModel)
	}
	return nil
}
