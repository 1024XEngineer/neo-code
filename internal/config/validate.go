package config

import (
	"fmt"
	"os"
	"strings"
)

// Validate checks that the configuration is internally consistent.
func Validate(cfg Config) error {
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("config.providers must contain at least one provider")
	}

	seen := make(map[string]struct{}, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		if strings.TrimSpace(provider.Name) == "" {
			return fmt.Errorf("provider name cannot be empty")
		}
		if _, exists := seen[provider.Name]; exists {
			return fmt.Errorf("duplicate provider name %q", provider.Name)
		}
		seen[provider.Name] = struct{}{}

		if strings.TrimSpace(provider.Type) == "" {
			return fmt.Errorf("provider %q type cannot be empty", provider.Name)
		}
		if strings.TrimSpace(provider.BaseURL) == "" {
			return fmt.Errorf("provider %q base_url cannot be empty", provider.Name)
		}
		if strings.TrimSpace(provider.Model) == "" {
			return fmt.Errorf("provider %q model cannot be empty", provider.Name)
		}
		if strings.TrimSpace(provider.APIKeyEnv) == "" {
			return fmt.Errorf("provider %q api_key_env cannot be empty", provider.Name)
		}
	}

	if _, ok := cfg.ProviderByName(cfg.SelectedProvider); !ok {
		return fmt.Errorf("selected provider %q does not exist", cfg.SelectedProvider)
	}

	if strings.TrimSpace(cfg.CurrentModel) == "" {
		return fmt.Errorf("current_model cannot be empty")
	}

	if strings.TrimSpace(cfg.Workdir) == "" {
		return fmt.Errorf("workdir cannot be empty")
	}

	info, err := os.Stat(cfg.Workdir)
	if err != nil {
		return fmt.Errorf("invalid workdir %q: %w", cfg.Workdir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workdir %q is not a directory", cfg.Workdir)
	}

	if strings.TrimSpace(cfg.Shell) == "" {
		return fmt.Errorf("shell cannot be empty")
	}

	if strings.TrimSpace(cfg.SessionsPath) == "" {
		return fmt.Errorf("sessions_path cannot be empty")
	}

	if info, err := os.Stat(cfg.SessionsPath); err == nil && info.IsDir() {
		return fmt.Errorf("sessions_path %q must be a file path, not a directory", cfg.SessionsPath)
	}

	return nil
}
