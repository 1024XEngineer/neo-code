package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	configstate "neo-code/internal/config/state"
	providertypes "neo-code/internal/provider/types"
)

type mockSelectionService struct {
	listModelsFn           func(ctx context.Context) ([]providertypes.ModelDescriptor, error)
	listModelsSnapshotFn   func(ctx context.Context) ([]providertypes.ModelDescriptor, error)
	setCurrentModelFn      func(ctx context.Context, modelID string) (configstate.Selection, error)
	selectProviderFn       func(ctx context.Context, providerName string) (configstate.Selection, error)
	createCustomProviderFn func(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error)
}

func (m *mockSelectionService) ListModels(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	if m != nil && m.listModelsFn != nil {
		return m.listModelsFn(ctx)
	}
	return nil, nil
}

func (m *mockSelectionService) ListModelsSnapshot(ctx context.Context) ([]providertypes.ModelDescriptor, error) {
	if m != nil && m.listModelsSnapshotFn != nil {
		return m.listModelsSnapshotFn(ctx)
	}
	return nil, nil
}

func (m *mockSelectionService) SetCurrentModel(ctx context.Context, modelID string) (configstate.Selection, error) {
	if m != nil && m.setCurrentModelFn != nil {
		return m.setCurrentModelFn(ctx, modelID)
	}
	return configstate.Selection{}, nil
}

func (m *mockSelectionService) SelectProvider(ctx context.Context, providerName string) (configstate.Selection, error) {
	if m != nil && m.selectProviderFn != nil {
		return m.selectProviderFn(ctx, providerName)
	}
	return configstate.Selection{}, nil
}

func (m *mockSelectionService) CreateCustomProvider(
	ctx context.Context,
	input configstate.CreateCustomProviderInput,
) (configstate.Selection, error) {
	if m != nil && m.createCustomProviderFn != nil {
		return m.createCustomProviderFn(ctx, input)
	}
	return configstate.Selection{}, nil
}

func staticSelectionResolver(svc SelectionService) selectionServiceResolver {
	return selectionServiceResolverFunc(func(*cobra.Command) (SelectionService, error) {
		if svc == nil {
			return nil, errors.New("selection service unavailable")
		}
		return svc, nil
	})
}
