package config

import (
	"context"
	"errors"
	"sync"
)

type Manager struct {
	mu     sync.RWMutex
	loader *Loader
	config *Config
}

func NewManager(loader *Loader) *Manager {
	if loader == nil {
		panic("config: loader is nil")
	}

	return &Manager{
		loader: loader,
		config: func() *Config {
			cfg := loader.DefaultConfig()
			return &cfg
		}(),
	}
}

func (m *Manager) Load(ctx context.Context) (Config, error) {
	cfg, err := m.loader.Load(ctx)
	if err != nil {
		return Config{}, err
	}

	snapshot := cfg.Clone()

	m.mu.Lock()
	m.config = &snapshot
	m.mu.Unlock()

	return snapshot, nil
}

func (m *Manager) Reload(ctx context.Context) (Config, error) {
	return m.Load(ctx)
}

func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.config.Clone()
}

func (m *Manager) Save(ctx context.Context) error {
	m.mu.RLock()
	snapshot := m.config.Clone()
	m.mu.RUnlock()

	return m.loader.Save(ctx, &snapshot)
}

func (m *Manager) Update(ctx context.Context, mutate func(*Config) error) error {
	if mutate == nil {
		return errors.New("config: update mutate func is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	next := m.config.Clone()
	if err := mutate(&next); err != nil {
		return err
	}

	next.ApplyDefaultsFrom(m.loader.DefaultConfig())
	if err := next.Validate(); err != nil {
		return err
	}
	if err := m.loader.Save(ctx, &next); err != nil {
		return err
	}

	m.config = &next
	return nil
}

func (m *Manager) SelectedProvider() (ProviderConfig, error) {
	cfg := m.Get()
	return cfg.SelectedProviderConfig()
}

func (m *Manager) ResolvedSelectedProvider() (ResolvedProviderConfig, error) {
	cfg := m.Get()
	provider, err := cfg.EffectiveSelectedProviderConfig()
	if err != nil {
		return ResolvedProviderConfig{}, err
	}

	return provider.Resolve()
}

func (m *Manager) BaseDir() string {
	return m.loader.BaseDir()
}

func (m *Manager) ConfigPath() string {
	return m.loader.ConfigPath()
}
