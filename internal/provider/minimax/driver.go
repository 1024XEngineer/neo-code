package minimax

import (
	"context"

	"neo-code/internal/provider"
)

const DriverName = provider.DriverMiniMax

func Driver() provider.DriverDefinition {
	return provider.DriverDefinition{
		Name: DriverName,
		Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
			return New(cfg)
		},
		ValidateCatalogIdentity: func(identity provider.ProviderIdentity) error {
			return nil
		},
	}
}
