package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"neo-code/internal/config"
)

type Builder func(ctx context.Context, cfg config.ResolvedProviderConfig) (Provider, error)
type DiscoveryFunc func(ctx context.Context, cfg config.ResolvedProviderConfig) ([]config.ModelDescriptor, error)

type DriverDefinition struct {
	Name     string
	Build    Builder
	Discover DiscoveryFunc
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
	driverType := config.NormalizeKey(driver.Name)
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

func (r *Registry) Build(ctx context.Context, cfg config.ResolvedProviderConfig) (Provider, error) {
	driver, err := r.driver(cfg.Driver)
	if err != nil {
		return nil, err
	}
	return driver.Build(ctx, cfg)
}

func (r *Registry) DiscoverModels(ctx context.Context, cfg config.ResolvedProviderConfig) ([]config.ModelDescriptor, error) {
	driver, err := r.driver(cfg.Driver)
	if err != nil {
		return nil, err
	}
	if driver.Discover == nil {
		return nil, nil
	}
	return driver.Discover(ctx, cfg)
}

func (r *Registry) Supports(driverType string) bool {
	_, err := r.driver(driverType)
	return err == nil
}

func (r *Registry) driver(driverType string) (DriverDefinition, error) {
	if r == nil {
		return DriverDefinition{}, ErrDriverNotFound
	}
	driver, ok := r.drivers[config.NormalizeKey(driverType)]
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
