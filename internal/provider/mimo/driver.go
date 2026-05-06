package mimo

import (
	"context"

	"neo-code/internal/provider"
	providertypes "neo-code/internal/provider/types"
)

const DriverName = provider.DriverMiMo

func Driver() provider.DriverDefinition {
	return provider.DriverDefinition{
		Name: DriverName,
		Build: func(ctx context.Context, cfg provider.RuntimeConfig) (provider.Provider, error) {
			return New(cfg)
		},
		Discover: func(ctx context.Context, cfg provider.RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
			p, err := New(cfg)
			if err != nil {
				return nil, err
			}
			return p.DiscoverModels(ctx)
		},
		ValidateCatalogIdentity: func(identity provider.ProviderIdentity) error {
			return nil
		},
	}
}
