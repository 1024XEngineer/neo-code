package bootstrap

import (
	"context"
	"fmt"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	"neo-code/internal/memo"
	providertypes "neo-code/internal/provider/types"
	tuiservices "neo-code/internal/tui/services"
)

// ProviderService 定义 TUI 需要注入的 provider 交互能力。
type ProviderService interface {
	ListProviderOptions(ctx context.Context) ([]configstate.ProviderOption, error)
	SelectProvider(ctx context.Context, providerID string) (configstate.Selection, error)
	ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error)
	ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error)
	SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error)
	CreateCustomProvider(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error)
}

// Options 定义 bootstrap 装配输入。
type Options struct {
	Config          *config.Config
	ConfigManager   *config.Manager
	Runtime         tuiservices.Runtime
	ProviderService ProviderService
	MemoSvc         *memo.Service
	Mode            Mode
	Factory         ServiceFactory
}

// Container 表示完成装配后供 TUI Core 使用的依赖集合。
type Container struct {
	Config          config.Config
	ConfigManager   *config.Manager
	Runtime         tuiservices.Runtime
	ProviderService ProviderService
	MemoSvc         *memo.Service
	Mode            Mode
}

// Build 执行 TUI bootstrap 装配，并返回可注入到 App/Core 的容器。
func Build(options Options) (Container, error) {
	if options.ConfigManager == nil {
		return Container{}, fmt.Errorf("tui bootstrap: config manager is nil")
	}
	if options.Runtime == nil {
		return Container{}, fmt.Errorf("tui bootstrap: runtime is nil")
	}
	if options.ProviderService == nil {
		return Container{}, fmt.Errorf("tui bootstrap: provider service is nil")
	}

	mode := NormalizeMode(options.Mode)
	cfg := resolveConfigSnapshot(options.Config, options.ConfigManager)

	factory := options.Factory
	if factory == nil {
		factory = passthroughFactory{}
	}

	runtimeSvc, err := factory.BuildRuntime(mode, options.Runtime)
	if err != nil {
		return Container{}, fmt.Errorf("tui bootstrap: build runtime: %w", err)
	}
	if runtimeSvc == nil {
		return Container{}, fmt.Errorf("tui bootstrap: runtime factory returned nil")
	}

	providerSvc, err := factory.BuildProvider(mode, options.ProviderService)
	if err != nil {
		return Container{}, fmt.Errorf("tui bootstrap: build provider service: %w", err)
	}
	if providerSvc == nil {
		return Container{}, fmt.Errorf("tui bootstrap: provider factory returned nil")
	}

	return Container{
		Config:          cfg,
		ConfigManager:   options.ConfigManager,
		Runtime:         runtimeSvc,
		ProviderService: providerSvc,
		MemoSvc:         options.MemoSvc,
		Mode:            mode,
	}, nil
}

// resolveConfigSnapshot 返回用于本次 TUI 初始化的配置快照。
func resolveConfigSnapshot(cfg *config.Config, manager *config.Manager) config.Config {
	if cfg == nil {
		return manager.Get()
	}
	return cfg.Clone()
}
