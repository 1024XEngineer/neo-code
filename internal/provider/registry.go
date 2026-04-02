package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"neo-code/internal/config"
)

type Builder func(ctx context.Context, cfg config.ResolvedProviderConfig) (Provider, error)

type DriverDefinition struct {
	Name  string
	Build Builder
}

type Registry struct {
	mu                 sync.Mutex
	drivers            map[string]DriverDefinition
	discoveryProviders map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{
		drivers:            map[string]DriverDefinition{},
		discoveryProviders: map[string]Provider{},
	}
}

func (r *Registry) Register(driver DriverDefinition) error {
	if r == nil {
		return errors.New("provider: registry is nil")
	}

	r.ensureDrivers()

	driver.Name = strings.TrimSpace(driver.Name)
	driverType := config.NormalizeProviderDriver(driver.Name)
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

func (r *Registry) DiscoverModels(ctx context.Context, cfg config.ResolvedProviderConfig) ([]ModelDescriptor, error) {
	discoveryProvider, err := r.discoveryProvider(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return discoveryProvider.DiscoverModels(ctx)
}

func (r *Registry) Supports(driverType string) bool {
	_, err := r.driver(driverType)
	return err == nil
}

func (r *Registry) driver(driverType string) (DriverDefinition, error) {
	if r == nil {
		return DriverDefinition{}, ErrDriverNotFound
	}
	driver, ok := r.drivers[config.NormalizeProviderDriver(driverType)]
	if !ok {
		return DriverDefinition{}, fmt.Errorf("%w: %s", ErrDriverNotFound, strings.TrimSpace(driverType))
	}
	return driver, nil
}

func (r *Registry) ensureDrivers() {
	if r.drivers == nil {
		r.drivers = map[string]DriverDefinition{}
	}
	if r.discoveryProviders == nil {
		r.discoveryProviders = map[string]Provider{}
	}
}

func (r *Registry) discoveryProvider(ctx context.Context, cfg config.ResolvedProviderConfig) (Provider, error) {
	driver, err := r.driver(cfg.Driver)
	if err != nil {
		return nil, err
	}

	cacheKey, err := discoveryCacheKey(cfg)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.ensureDrivers()
	if cached, ok := r.discoveryProviders[cacheKey]; ok {
		r.mu.Unlock()
		return cached, nil
	}
	r.mu.Unlock()

	built, err := driver.Build(ctx, cfg)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.ensureDrivers()
	if cached, ok := r.discoveryProviders[cacheKey]; ok {
		r.mu.Unlock()
		return cached, nil
	}
	r.discoveryProviders[cacheKey] = built
	r.mu.Unlock()

	return built, nil
}

func discoveryCacheKey(cfg config.ResolvedProviderConfig) (string, error) {
	identity, err := cfg.Identity()
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(identity.Key() + "|" + strings.TrimSpace(cfg.APIKey)))
	return identity.Key() + "|" + hex.EncodeToString(sum[:]), nil
}
