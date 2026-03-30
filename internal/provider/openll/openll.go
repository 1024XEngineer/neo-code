package openll

import (
	"neo-code/internal/config"
)

const (
	Name             = "openll"
	DriverName       = "openai"
	DefaultBaseURL   = "https://www.openll.top/v1"
	DefaultModel     = "gpt-5.4"
	DefaultAPIKeyEnv = "AI_API_KEY"
)

var builtinModels = []string{
	DefaultModel,
	"gpt-5.3-codex",
	"gpt-5.3-turbo",
}

func BuiltinConfig() config.ProviderConfig {
	return config.ProviderConfig{
		Name:      Name,
		Driver:    DriverName,
		BaseURL:   DefaultBaseURL,
		Model:     DefaultModel,
		Models:    append([]string(nil), builtinModels...),
		APIKeyEnv: DefaultAPIKeyEnv,
	}
}
