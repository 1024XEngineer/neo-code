package services

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"neo-code/internal/config"
)

// ProviderSelector 定义 provider 选择命令所需最小能力。
type ProviderSelector interface {
	SelectProvider(ctx context.Context, providerID string) (config.ProviderSelection, error)
}

// ModelSelector 定义 model 选择命令所需最小能力。
type ModelSelector interface {
	SetCurrentModel(ctx context.Context, modelID string) (config.ProviderSelection, error)
}

// ModelCatalogReader 定义 model catalog 刷新所需最小能力。
type ModelCatalogReader interface {
	ListModels(ctx context.Context) ([]config.ModelDescriptor, error)
}

// SelectProviderCmd 执行 provider 切换并将结果映射为 UI 消息。
func SelectProviderCmd(
	providerSvc ProviderSelector,
	providerID string,
	toMsg func(config.ProviderSelection, error) tea.Msg,
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
	toMsg func(config.ProviderSelection, error) tea.Msg,
) tea.Cmd {
	return func() tea.Msg {
		selection, err := providerSvc.SetCurrentModel(context.Background(), modelID)
		return toMsg(selection, err)
	}
}

// RefreshModelCatalogCmd 拉取指定 provider 的模型列表并返回 UI 消息。
func RefreshModelCatalogCmd(
	providerSvc ModelCatalogReader,
	providerID string,
	toMsg func(string, []config.ModelDescriptor, error) tea.Msg,
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
