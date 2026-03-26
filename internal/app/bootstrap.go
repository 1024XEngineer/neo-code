package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"neocode/internal/config"
	"neocode/internal/provider"
	openaiprovider "neocode/internal/provider/openai"
	"neocode/internal/runtime"
	"neocode/internal/tools"
	bashtool "neocode/internal/tools/bash"
	fstool "neocode/internal/tools/filesystem"
	webfetchtool "neocode/internal/tools/webfetch"
	"neocode/internal/tui"
)

const providerTimeout = 90 * time.Second

// Bootstrap wires the full application dependency graph.
type Bootstrap struct {
	Config  config.Config
	Runtime *runtime.Service
	UI      *tui.App
}

// Build assembles the runtime, provider, tools, and TUI.
func Build(configPath string) (*Bootstrap, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	bindings := make([]runtime.ProviderBinding, 0, len(cfg.Providers))
	for _, providerCfg := range cfg.Providers {
		apiKey, ok := os.LookupEnv(providerCfg.APIKeyEnv)
		if !ok || apiKey == "" {
			if providerCfg.Name == cfg.SelectedProvider {
				return nil, fmt.Errorf(
					"当前选中的 provider %q 没有读到环境变量 %q。这个报错发生在真正请求模型之前，所以不是模型名过期导致的。请确认当前进程里真的有这个变量，或者把 api_key_env 写成纯变量名，例如 OPENAI_API_KEY。",
					providerCfg.Name,
					providerCfg.APIKeyEnv,
				)
			}
			continue
		}

		modelProvider, err := buildProvider(providerCfg, apiKey)
		if err != nil {
			return nil, err
		}

		model := providerCfg.Model
		if providerCfg.Name == cfg.SelectedProvider && cfg.CurrentModel != "" {
			model = cfg.CurrentModel
		}

		bindings = append(bindings, runtime.ProviderBinding{
			Name:   providerCfg.Name,
			Model:  model,
			Client: modelProvider,
		})
	}

	if len(bindings) == 0 {
		return nil, fmt.Errorf("没有可用的 provider，请检查配置中的 API Key 环境变量是否已设置")
	}

	registry := tools.NewRegistry()
	for _, tool := range []tools.Tool{
		fstool.NewReadFileTool(),
		fstool.NewWriteFileTool(),
		fstool.NewListDirTool(),
		fstool.NewSearchTool(),
		bashtool.NewExecTool(cfg.Shell),
		webfetchtool.NewFetchTool(),
	} {
		if err := registry.Register(tool); err != nil {
			return nil, err
		}
	}

	rt, err := runtime.New(
		bindings[0].Client,
		registry,
		bindings[0].Model,
		cfg.Workdir,
		runtime.WithProviders(bindings, cfg.SelectedProvider),
		runtime.WithSessionStorePath(cfg.SessionsPath),
	)
	if err != nil {
		return nil, err
	}

	return &Bootstrap{
		Config:  cfg,
		Runtime: rt,
		UI:      tui.New(rt),
	}, nil
}

// Run starts the full Bubble Tea application.
func Run(ctx context.Context, configPath string) error {
	bootstrap, err := Build(configPath)
	if err != nil {
		return err
	}

	return bootstrap.UI.Run(ctx)
}

func buildProvider(cfg config.ProviderConfig, apiKey string) (provider.Provider, error) {
	switch cfg.Type {
	case "openai", "openai-compatible":
		return openaiprovider.New(cfg.Name, cfg.BaseURL, apiKey, providerTimeout), nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q", cfg.Type)
	}
}
