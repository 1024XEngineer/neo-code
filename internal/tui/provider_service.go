package tui

import (
	"context"

	"neo-code/internal/provider"
)

type ProviderController interface {
	ListProviders(ctx context.Context) ([]provider.ProviderCatalogItem, error)
	SelectProvider(ctx context.Context, providerID string) (provider.ProviderSelection, error)
	ListModels(ctx context.Context) ([]provider.ModelDescriptor, error)
	SetCurrentModel(ctx context.Context, modelID string) (provider.ProviderSelection, error)
	SetAPIKeyEnvOverride(ctx context.Context, envName string) error
}
