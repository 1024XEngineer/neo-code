package services

import (
	"context"

	"go-llm-demo/configs"
	"go-llm-demo/internal/server/domain"
	serverprovider "go-llm-demo/internal/server/infra/provider"
	servertools "go-llm-demo/internal/server/infra/tools"
)

type ToolCall = domain.ToolCall
type ToolResult = servertools.ToolResult

const (
	TypeUserPreference = domain.TypeUserPreference
	TypeProjectRule    = domain.TypeProjectRule
	TypeCodeFact       = domain.TypeCodeFact
	TypeFixRecipe      = domain.TypeFixRecipe
	TypeSessionMemory  = domain.TypeSessionMemory
)

var (
	ErrInvalidAPIKey        = serverprovider.ErrInvalidAPIKey
	ErrAPIKeyValidationSoft = serverprovider.ErrAPIKeyValidationSoft
)

func ResolveWorkspaceRoot(workspaceFlag string) (string, error) {
	return servertools.ResolveWorkspaceRoot(workspaceFlag)
}

func SetWorkspaceRoot(root string) error {
	return servertools.SetWorkspaceRoot(root)
}

func GetWorkspaceRoot() string {
	return servertools.GetWorkspaceRoot()
}

func NormalizeToolParams(params map[string]interface{}) map[string]interface{} {
	return servertools.NormalizeParams(params)
}

func ExecuteToolCall(call ToolCall) *ToolResult {
	return servertools.GlobalRegistry.Execute(call)
}

func ValidateChatAPIKey(ctx context.Context, cfg *configs.AppConfiguration) error {
	return serverprovider.ValidateChatAPIKey(ctx, cfg)
}

func NormalizeProviderName(name string) (string, bool) {
	return serverprovider.NormalizeProviderName(name)
}

func SupportedProviders() []string {
	return serverprovider.SupportedProviders()
}

func DefaultModelForProvider(name string) string {
	return serverprovider.DefaultModelForProvider(name)
}
