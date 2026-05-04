package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
)

var (
	runModelLsCommand  = defaultModelLsCommandRunner
	runModelSetCommand = defaultModelSetCommandRunner
)

// newModelCommand 创建 model 命令组，管理当前 provider 下的模型选择。
func newModelCommand() *cobra.Command {
	return newModelCommandWithResolver(newRuntimeSelectionServiceResolver())
}

// newModelCommandWithResolver 基于注入的选择服务解析器构建 model 命令组。
func newModelCommandWithResolver(resolver selectionServiceResolver) *cobra.Command {
	if resolver == nil {
		resolver = newRuntimeSelectionServiceResolver()
	}
	cmd := &cobra.Command{
		Use:          "model",
		Short:        "Manage model selection for the current provider",
		SilenceUsage: true,
	}

	cmd.AddCommand(
		newModelLsCommandWithResolver(resolver),
		newModelSetCommandWithResolver(resolver),
	)

	return cmd
}

// newModelLsCommand 创建 model ls 子命令，列出当前选中 provider 可用的模型。
func newModelLsCommand() *cobra.Command {
	return newModelLsCommandWithResolver(newRuntimeSelectionServiceResolver())
}

// newModelLsCommandWithResolver 创建支持依赖注入的 model ls 子命令。
func newModelLsCommandWithResolver(resolver selectionServiceResolver) *cobra.Command {
	if resolver == nil {
		resolver = newRuntimeSelectionServiceResolver()
	}
	return &cobra.Command{
		Use:   "ls",
		Short: "List available models for the current provider",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := resolver.Resolve(cmd)
			if err != nil {
				return err
			}
			return runModelLsCommand(cmd, svc)
		},
	}
}

// newModelSetCommand 创建 model set 子命令，切换当前使用的模型。
func newModelSetCommand() *cobra.Command {
	return newModelSetCommandWithResolver(newRuntimeSelectionServiceResolver())
}

// newModelSetCommandWithResolver 创建支持依赖注入的 model set 子命令。
func newModelSetCommandWithResolver(resolver selectionServiceResolver) *cobra.Command {
	if resolver == nil {
		resolver = newRuntimeSelectionServiceResolver()
	}
	return &cobra.Command{
		Use:   "set <model-id>",
		Short: "Switch to a specific model",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := resolver.Resolve(cmd)
			if err != nil {
				return err
			}
			return runModelSetCommand(cmd, svc, args[0])
		},
	}
}

// defaultModelLsCommandRunner 列出当前选中 provider 的所有可用模型。
func defaultModelLsCommandRunner(cmd *cobra.Command, svc SelectionService) error {
	if svc == nil {
		return fmt.Errorf("selection service is unavailable")
	}

	var workdir string
	if f := cmd.Flag("workdir"); f != nil {
		workdir = f.Value.String()
	}
	loader := config.NewLoader(workdir, config.StaticDefaults())
	cfg, err := loader.Load(cmd.Context())
	if err != nil {
		return err
	}

	selectedName := strings.TrimSpace(cfg.SelectedProvider)
	if selectedName == "" {
		return fmt.Errorf("尚未选择任何供应商，请先运行 neocode use <provider>")
	}

	providerCfg, err := cfg.ProviderByName(selectedName)
	if err != nil {
		return fmt.Errorf("当前选中的供应商 %q 不存在: %w", selectedName, err)
	}

	currentModel := strings.TrimSpace(cfg.CurrentModel)
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "供应商: %s\n", providerCfg.Name)
	fmt.Fprintf(out, "当前模型: %s\n", displayCurrentModel(currentModel))
	fmt.Fprintln(out, "可用模型:")

	snapshotErr := error(nil)
	models, err := svc.ListModelsSnapshot(cmd.Context())
	if err != nil {
		snapshotErr = err
		models = nil
	}
	if len(models) == 0 {
		if snapshotErr != nil {
			_, _ = fmt.Fprintf(
				cmd.ErrOrStderr(),
				"warning: model snapshot unavailable, fallback to live discovery: %v\n",
				snapshotErr,
			)
		}
		models, err = svc.ListModels(cmd.Context())
		if err != nil {
			return err
		}
	}
	if len(models) == 0 {
		fmt.Fprintln(out, "  (无可用模型，该供应商使用动态发现)")
		return nil
	}

	for _, model := range models {
		modelID := strings.TrimSpace(model.ID)
		if modelID == "" {
			continue
		}
		marker := "  "
		if strings.EqualFold(modelID, currentModel) {
			marker = "* "
		}
		line := fmt.Sprintf("%s%s", marker, modelID)
		if name := strings.TrimSpace(model.Name); name != "" && name != modelID {
			line += fmt.Sprintf(" (%s)", name)
		}
		fmt.Fprintln(out, line)
	}

	return nil
}

// defaultModelSetCommandRunner 切换当前模型，并通过选择服务完成模型归属校验与持久化。
func defaultModelSetCommandRunner(cmd *cobra.Command, svc SelectionService, modelID string) error {
	if svc == nil {
		return fmt.Errorf("selection service is unavailable")
	}

	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("模型 ID 不能为空")
	}

	selection, err := svc.SetCurrentModel(cmd.Context(), modelID)
	if err != nil {
		if errors.Is(err, configstate.ErrModelNotFound) {
			var workdir string
			if f := cmd.Flag("workdir"); f != nil {
				workdir = f.Value.String()
			}
			loader := config.NewLoader(workdir, config.StaticDefaults())
			cfg, loadErr := loader.Load(cmd.Context())
			if loadErr != nil {
				return fmt.Errorf("provider has no model %q: %w", modelID, err)
			}
			providerName := strings.TrimSpace(cfg.SelectedProvider)
			if providerName == "" {
				return fmt.Errorf("provider has no model %q: %w", modelID, err)
			}
			return fmt.Errorf("provider %q has no model %q: %w", providerName, modelID, err)
		}
		return err
	}

	activeModel := strings.TrimSpace(selection.ModelID)
	if activeModel == "" {
		activeModel = modelID
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✅ 已切换模型: %s\n", activeModel)
	return nil
}

// displayCurrentModel 格式化当前模型名称，未设置时显示占位文案。
func displayCurrentModel(model string) string {
	if model == "" {
		return "(未设置，将自动选择)"
	}
	return model
}
