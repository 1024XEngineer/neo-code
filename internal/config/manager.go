package config

import (
	"bufio"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
)

type Manager struct {
	mu     sync.RWMutex
	loader *Loader
	config *Config
}

func NewManager(loader *Loader) *Manager {
	if loader == nil {
		loader = NewLoader("")
	}

	return &Manager{
		loader: loader,
		config: Default(),
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

	next.ApplyDefaults()
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
	provider, err := m.SelectedProvider()
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

func (m *Manager) EnvPath() string {
	return m.loader.EnvPath()
}

func (m *Manager) ReloadEnvironment() {
	m.loader.LoadEnvironment()
}

func (m *Manager) OverloadManagedEnvironment() error {
	return m.loader.OverloadManagedEnvironment()
}

func (m *Manager) UpsertEnv(key string, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("config: env key is empty")
	}

	lines := []string{}
	if data, err := os.ReadFile(m.EnvPath()); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
	}

	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+"=") {
			lines[i] = key + "=" + value
			replaced = true
		}
	}
	if !replaced {
		lines = append(lines, key+"="+value)
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if err := os.MkdirAll(m.BaseDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.EnvPath(), []byte(content), 0o644)
}
