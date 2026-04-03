package config

// DefaultConfig returns the application defaults with builtin provider metadata attached.
func DefaultConfig() *Config {
	cfg := Default()
	providers := DefaultProviders()
	cfg.Providers = providers
	if len(providers) == 0 {
		return cfg
	}
	cfg.SelectedProvider = providers[0].Name
	cfg.CurrentModel = providers[0].Model
	return cfg
}
