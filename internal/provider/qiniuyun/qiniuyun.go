package qiniuyun

import (
	"neo-code/internal/config"
	"neo-code/internal/provider/openai"
)

const (
	Name             = "qiniuyun"
	DriverName       = openai.DriverName
	DefaultBaseURL   = "https://api.qnaigc.com/v1"
	DefaultModel     = "deepseek/deepseek-v3.2-251201"
	DefaultAPIKeyEnv = "QINIUYUN_API_KEY"
)

var builtinModels = []string{
	"z-ai/glm-5",
	"minimax/minimax-m2.5",
	"moonshotai/kimi-k2.5",
	DefaultModel,
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
