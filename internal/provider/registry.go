package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	providertypes "neo-code/internal/provider/types"
)

type Builder func(ctx context.Context, cfg RuntimeConfig) (Provider, error)
type DiscoveryFunc func(ctx context.Context, cfg RuntimeConfig) ([]providertypes.ModelDescriptor, error)

type DriverDefinition struct {
	Name                    string
	Build                   Builder
	Discover                DiscoveryFunc
	ValidateCatalogIdentity func(identity ProviderIdentity) error
}

type Registry struct {
	drivers map[string]DriverDefinition
}

func NewRegistry() *Registry {
	return &Registry{drivers: map[string]DriverDefinition{}}
}

func (r *Registry) Register(driver DriverDefinition) error {
	if r == nil {
		return errors.New("provider: registry is nil")
	}

	r.ensureDrivers()

	driver.Name = strings.TrimSpace(driver.Name)
	driverType := normalizeDriverKey(driver.Name)
	if driverType == "" {
		return errors.New("provider: driver name is empty")
	}
	if driver.Build == nil {
		return fmt.Errorf("provider: driver %q build func is nil", driver.Name)
	}
	if _, exists := r.drivers[driverType]; exists {
		return fmt.Errorf("%w: %s", ErrDriverAlreadyRegistered, driver.Name)
	}
	r.drivers[driverType] = driver
	return nil
}

func (r *Registry) Build(ctx context.Context, cfg RuntimeConfig) (Provider, error) {
	driver, err := r.driver(cfg.Driver)
	if err != nil {
		return nil, err
	}
	return driver.Build(ctx, cfg)
}

func (r *Registry) DiscoverModels(ctx context.Context, cfg RuntimeConfig) ([]providertypes.ModelDescriptor, error) {
	driver, err := r.driver(cfg.Driver)
	if err != nil {
		return nil, err
	}
	if driver.Discover == nil {
		return nil, NewDiscoveryConfigError(fmt.Sprintf("driver %q does not support model discovery", driver.Name))
	}
	return driver.Discover(ctx, cfg)
}

func (r *Registry) SupportsDiscovery(driverType string) bool {
	driver, err := r.driver(driverType)
	if err != nil {
		return false
	}
	return driver.Discover != nil
}

func (r *Registry) Supports(driverType string) bool {
	_, err := r.driver(driverType)
	return err == nil
}

// ValidateCatalogIdentity 在读取 catalog 快照或执行默认模型回退前执行无需密钥的静态校验，避免无效配置被误判为可用。
func (r *Registry) ValidateCatalogIdentity(identity ProviderIdentity) error {
	if r == nil {
		return ErrDriverNotFound
	}

	driver, err := r.driver(identity.Driver)
	if err != nil {
		return err
	}
	if driver.ValidateCatalogIdentity == nil {
		return nil
	}
	return driver.ValidateCatalogIdentity(identity)
}

func (r *Registry) driver(driverType string) (DriverDefinition, error) {
	if r == nil {
		return DriverDefinition{}, ErrDriverNotFound
	}
	driver, ok := r.drivers[normalizeDriverKey(driverType)]
	if !ok {
		return DriverDefinition{}, fmt.Errorf("%w: %s", ErrDriverNotFound, strings.TrimSpace(driverType))
	}
	return driver, nil
}

func (r *Registry) ensureDrivers() {
	if r.drivers == nil {
		r.drivers = map[string]DriverDefinition{}
	}
}

// normalizeDriverKey 统一规范化 driver 名称，保证注册与查询稳定匹配。
func normalizeDriverKey(driverType string) string {
	return strings.ToLower(strings.TrimSpace(driverType))
}
