package config

const (
	defaultConfigDirName    = ".neocode"
	defaultConfigFileName   = "config.yaml"
	defaultSessionsFileName = "sessions.json"
)

// Config is the fully validated runtime configuration.
type Config struct {
	Providers        []ProviderConfig `yaml:"providers"`
	SelectedProvider string           `yaml:"selected_provider"`
	CurrentModel     string           `yaml:"current_model"`
	Workdir          string           `yaml:"workdir"`
	Shell            string           `yaml:"shell"`
	SessionsPath     string           `yaml:"sessions_path"`
}

// ProviderConfig describes a model provider entry.
type ProviderConfig struct {
	Name      string `yaml:"name"`
	Type      string `yaml:"type"`
	BaseURL   string `yaml:"base_url"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
}

// ProviderByName returns the provider with the matching name.
func (c Config) ProviderByName(name string) (ProviderConfig, bool) {
	for _, provider := range c.Providers {
		if provider.Name == name {
			return provider, true
		}
	}

	return ProviderConfig{}, false
}
