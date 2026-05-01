package state

import (
	"context"
	"errors"
	"strings"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
)

// Service 管理当前 provider/model 选择状态，并通过 config.Manager 持久化变更。
type Service struct {
	manager    *config.Manager
	supporters DriverSupporter
	catalogs   ModelCatalog
}

// NewService 创建选择状态服务实例。
func NewService(manager *config.Manager, supporters DriverSupporter, catalogs ModelCatalog) *Service {
	return &Service{
		manager:    manager,
		supporters: supporters,
		catalogs:   catalogs,
	}
}

// ListProviderOptions 枚举所有已配置且当前运行时支持的 provider 及其缓存模型列表。
func (s *Service) ListProviderOptions(ctx context.Context) ([]ProviderOption, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg := s.manager.Get()
	options := make([]ProviderOption, 0, len(cfg.Providers))
	for _, providerCfg := range cfg.Providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !s.supporters.Supports(providerCfg.Driver) {
			continue
		}

		input, err := catalogInputFromProvider(providerCfg)
		if err != nil {
			return nil, err
		}
		models, err := s.catalogs.ListProviderModelsCached(ctx, input)
		if err != nil {
			return nil, err
		}
		options = append(options, providerOption(providerCfg, models))
	}

	return options, nil
}

// SelectProvider 切换当前 provider，并将 current_model 修正为该 provider 下的有效模型。
func (s *Service) SelectProvider(ctx context.Context, providerName string) (Selection, error) {
	if err := s.validate(); err != nil {
		return Selection{}, err
	}

	cfgSnapshot := s.manager.Get()
	providerCfg, err := cfgSnapshot.ProviderByName(providerName)
	if err != nil {
		return Selection{}, ErrProviderNotFound
	}
	if err := ensureSupportedProvider(s.supporters, providerCfg); err != nil {
		return Selection{}, err
	}

	input, err := catalogInputFromProvider(providerCfg)
	if err != nil {
		return Selection{}, err
	}
	var models []providertypes.ModelDescriptor
	if providerCfg.Source == config.ProviderSourceCustom {
		models, err = s.catalogs.ListProviderModels(ctx, input)
	} else {
		models, err = s.catalogs.ListProviderModelsSnapshot(ctx, input)
		if len(models) == 0 {
			models = providertypes.DescriptorsFromIDs([]string{strings.TrimSpace(providerCfg.Model)})
		}
	}
	if err != nil {
		return Selection{}, err
	}
	if len(models) == 0 {
		return Selection{}, ErrNoModelsAvailable
	}

	var selection Selection
	err = s.manager.Update(ctx, func(cfg *config.Config) error {
		selected, err := cfg.ProviderByName(providerName)
		if err != nil {
			return ErrProviderNotFound
		}
		if err := ensureSupportedProvider(s.supporters, selected); err != nil {
			return err
		}

		cfg.SelectedProvider = selected.Name
		nextModel, _ := resolveCurrentModel(cfg.CurrentModel, models, selected.Model)
		cfg.CurrentModel = nextModel
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return Selection{}, err
	}

	return selection, nil
}

// ListModels 获取当前选中 provider 的模型列表，必要时会同步触发远程发现。
func (s *Service) ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	selected, err := selectedProviderConfig(s.manager.Get())
	if err != nil {
		return nil, err
	}
	if err := ensureSupportedProvider(s.supporters, selected); err != nil {
		return nil, err
	}
	input, err := catalogInputFromProvider(selected)
	if err != nil {
		return nil, err
	}
	return s.catalogs.ListProviderModels(ctx, input)
}

// RefreshModels 强制刷新当前选中 provider 的模型目录，并在必要时修正 current_model。
func (s *Service) RefreshModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	selected, err := selectedProviderConfig(s.manager.Get())
	if err != nil {
		return nil, err
	}
	if err := ensureSupportedProvider(s.supporters, selected); err != nil {
		return nil, err
	}
	input, err := catalogInputFromProvider(selected)
	if err != nil {
		return nil, err
	}
	models, err := s.catalogs.RefreshProviderModels(ctx, input)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, ErrNoModelsAvailable
	}

	if err := s.manager.Update(ctx, func(cfg *config.Config) error {
		currentSelected, err := selectedProviderConfig(*cfg)
		if err != nil {
			return err
		}
		sameIdentity, err := sameProviderIdentity(currentSelected, selected)
		if err != nil {
			return err
		}
		if !sameIdentity {
			return errSelectionDrifted
		}
		cfg.CurrentModel, _ = resolveCurrentModel(cfg.CurrentModel, models, currentSelected.Model)
		return nil
	}); err != nil {
		return nil, err
	}

	return models, nil
}

// ListModelsSnapshot 获取当前选中 provider 的快照模型列表，不阻塞等待同步发现。
func (s *Service) ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	selected, err := selectedProviderConfig(s.manager.Get())
	if err != nil {
		return nil, err
	}
	if err := ensureSupportedProvider(s.supporters, selected); err != nil {
		return nil, err
	}
	input, err := catalogInputFromProvider(selected)
	if err != nil {
		return nil, err
	}
	return s.catalogs.ListProviderModelsSnapshot(ctx, input)
}

// SetCurrentModel 切换当前模型，目标模型必须出现在当前 provider 的可用模型列表中。
func (s *Service) SetCurrentModel(ctx context.Context, modelID string) (Selection, error) {
	if err := s.validate(); err != nil {
		return Selection{}, err
	}

	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return Selection{}, ErrModelNotFound
	}

	selected, err := selectedProviderConfig(s.manager.Get())
	if err != nil {
		return Selection{}, err
	}
	if err := ensureSupportedProvider(s.supporters, selected); err != nil {
		return Selection{}, err
	}

	input, err := catalogInputFromProvider(selected)
	if err != nil {
		return Selection{}, err
	}
	models, err := s.catalogs.ListProviderModels(ctx, input)
	if err != nil {
		return Selection{}, err
	}
	if len(models) == 0 {
		return Selection{}, ErrNoModelsAvailable
	}
	if !containsModelDescriptorID(models, modelID) {
		return Selection{}, ErrModelNotFound
	}

	var selection Selection
	err = s.manager.Update(ctx, func(cfg *config.Config) error {
		if _, err := selectedProviderConfig(*cfg); err != nil {
			return err
		}
		cfg.CurrentModel = modelID
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return Selection{}, err
	}

	return selection, nil
}

// EnsureSelection 确保当前 provider 和 model 仍然有效，必要时自动修正。
func (s *Service) EnsureSelection(ctx context.Context) (Selection, error) {
	const maxEnsureSelectionAttempts = 2

	for attempt := 0; attempt < maxEnsureSelectionAttempts; attempt++ {
		selection, err := s.ensureSelectionOnce(ctx)
		if err == nil {
			return selection, nil
		}
		if errors.Is(err, errSelectionDrifted) && attempt+1 < maxEnsureSelectionAttempts {
			continue
		}
		return Selection{}, err
	}

	return Selection{}, errSelectionDrifted
}

// ensureSelectionOnce 基于一次稳定快照校正当前 provider/model 选择，并在写入前校验 provider 未漂移。
func (s *Service) ensureSelectionOnce(ctx context.Context) (Selection, error) {
	if err := s.validate(); err != nil {
		return Selection{}, err
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}

	cfgSnapshot := s.manager.Get()
	selected, err := selectedProviderConfig(cfgSnapshot)
	if err != nil {
		selected, err = s.bootstrapInitialSelection(ctx, cfgSnapshot)
		if err != nil {
			return Selection{}, err
		}
		cfgSnapshot = s.manager.Get()
	}
	if err := ensureSupportedProvider(s.supporters, selected); err != nil {
		return Selection{}, err
	}

	input, err := catalogInputFromProvider(selected)
	if err != nil {
		return Selection{}, err
	}
	models, err := s.catalogs.ListProviderModelsSnapshot(ctx, input)
	if err != nil {
		return Selection{}, err
	}
	if len(models) == 0 {
		if selected.Source == config.ProviderSourceCustom {
			if strings.TrimSpace(cfgSnapshot.CurrentModel) == "" {
				discovered, discoverErr := s.catalogs.ListProviderModels(ctx, input)
				if discoverErr == nil {
					models = discovered
				}
			}
			if len(models) == 0 {
				latestSnapshot := s.manager.Get()
				sameSelection, sameErr := sameSelectionSnapshot(latestSnapshot, cfgSnapshot, selected)
				if sameErr != nil {
					return Selection{}, sameErr
				}
				if !sameSelection {
					return Selection{}, errSelectionDrifted
				}
				return selectionFromConfig(latestSnapshot), nil
			}
		} else {
			models = providertypes.DescriptorsFromIDs([]string{strings.TrimSpace(selected.Model)})
		}
	}
	if len(models) == 0 {
		return Selection{}, ErrNoModelsAvailable
	}
	_, modelChanged := resolveCurrentModel(cfgSnapshot.CurrentModel, models, selected.Model)
	if !modelChanged && strings.TrimSpace(cfgSnapshot.SelectedProvider) != "" {
		latestSnapshot := s.manager.Get()
		sameSelection, sameErr := sameSelectionSnapshot(latestSnapshot, cfgSnapshot, selected)
		if sameErr != nil {
			return Selection{}, sameErr
		}
		if !sameSelection {
			return Selection{}, errSelectionDrifted
		}
		return selectionFromConfig(latestSnapshot), nil
	}

	var selection Selection
	err = s.manager.Update(ctx, func(cfg *config.Config) error {
		currentSelected, err := selectedProviderConfig(*cfg)
		if err != nil {
			currentSelected = selected
			cfg.SelectedProvider = selected.Name
		} else {
			sameIdentity, identityErr := sameProviderIdentity(currentSelected, selected)
			if identityErr != nil {
				return identityErr
			}
			if !sameIdentity {
				return errSelectionDrifted
			}
		}

		cfg.CurrentModel, _ = resolveCurrentModel(cfg.CurrentModel, models, currentSelected.Model)
		selection = selectionFromConfig(*cfg)
		return nil
	})
	if err != nil {
		return Selection{}, err
	}

	return selection, nil
}

// bootstrapInitialSelection 为缺失当前选择状态的配置建立一个干净的初始选择。
func (s *Service) bootstrapInitialSelection(ctx context.Context, cfg config.Config) (config.ProviderConfig, error) {
	for _, providerCfg := range cfg.Providers {
		if err := ensureSupportedProvider(s.supporters, providerCfg); err != nil {
			continue
		}
		err := s.manager.Update(ctx, func(current *config.Config) error {
			current.SelectedProvider = providerCfg.Name
			current.CurrentModel = strings.TrimSpace(providerCfg.Model)
			return nil
		})
		if err != nil {
			return config.ProviderConfig{}, err
		}
		return providerCfg, nil
	}
	return config.ProviderConfig{}, ErrProviderNotFound
}

// validate 校验服务依赖是否完整。
func (s *Service) validate() error {
	if s == nil {
		return errors.New("selection: service is nil")
	}
	if s.manager == nil {
		return errors.New("selection: config manager is nil")
	}
	if s.supporters == nil {
		return errors.New("selection: driver supporter is nil")
	}
	if s.catalogs == nil {
		return errors.New("selection: catalog service is nil")
	}
	return nil
}
