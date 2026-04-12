package services

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	configstate "neo-code/internal/config/state"
	providertypes "neo-code/internal/provider/types"
)

// ProviderSelector 定义 provider 选择命令所需最小能力。
type ProviderSelector interface {
	SelectProvider(ctx context.Context, providerID string) (configstate.Selection, error)
}

// ModelSelector 定义 model 选择命令所需最小能力。
type ModelSelector interface {
	SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error)
}

// ModelCatalogReader 定义当前 provider 模型目录刷新所需最小能力。
type ModelCatalogReader interface {
	ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error)
}

// SelectProviderCmd 执行 provider 切换并将结果映射为 UI 消息。
func SelectProviderCmd(
	providerSvc ProviderSelector,
	providerID string,
	toMsg func(configstate.Selection, error) tea.Msg,
) tea.Cmd {
	return func() tea.Msg {
		selection, err := providerSvc.SelectProvider(context.Background(), providerID)
		return toMsg(selection, err)
	}
}

// SelectModelCmd 执行 model 切换并将结果映射为 UI 消息。
func SelectModelCmd(
	providerSvc ModelSelector,
	modelID string,
	toMsg func(configstate.Selection, error) tea.Msg,
) tea.Cmd {
	return func() tea.Msg {
		selection, err := providerSvc.SetCurrentModel(context.Background(), modelID)
		return toMsg(selection, err)
	}
}

// RefreshModelCatalogCmd 拉取当前 provider 的模型列表，并带回触发时的 providerID 快照供 UI 去重。
func RefreshModelCatalogCmd(
	providerSvc ModelCatalogReader,
	providerID string,
	toMsg func(string, []providertypes.ModelDescriptor, error) tea.Msg,
) tea.Cmd {
	providerID = strings.TrimSpace(providerID)
	if providerSvc == nil || providerID == "" {
		return nil
	}

	return func() tea.Msg {
		models, err := providerSvc.ListModels(context.Background())
		return toMsg(providerID, models, err)
	}
}
