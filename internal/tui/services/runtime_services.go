package services

import (
	"context"

	"go-llm-demo/configs"
	"go-llm-demo/internal/server/domain"
	serverprovider "go-llm-demo/internal/server/infra/provider"
	serverrepo "go-llm-demo/internal/server/infra/repository"
	servertools "go-llm-demo/internal/server/infra/tools"
	serverservice "go-llm-demo/internal/server/service"
)

type ToolCall = domain.ToolCall
type ToolResult = servertools.ToolResult
type ChatEvent = domain.ChatEvent
type ChatEventType = domain.ChatEventType
type ChatToolCall = domain.ChatToolCall
type ChatToolCallFunction = domain.ChatToolCallFunction
type ToolSchema = domain.ToolSchema

const (
	TypeUserPreference = domain.TypeUserPreference
	TypeProjectRule    = domain.TypeProjectRule
	TypeCodeFact       = domain.TypeCodeFact
	TypeFixRecipe      = domain.TypeFixRecipe
	TypeSessionMemory  = domain.TypeSessionMemory

	ChatEventDelta    = domain.ChatEventDelta
	ChatEventToolCall = domain.ChatEventToolCall
	ChatEventDone     = domain.ChatEventDone
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

func ApproveSecurityAsk(toolType, target string) {
	servertools.ApproveSecurityAsk(toolType, target)
}

func InitializeSecurity(configDir string) error {
	securityRepo := serverrepo.NewSecurityConfigRepository()
	securitySvc := serverservice.NewSecurityService(securityRepo)
	if err := securitySvc.Initialize(configDir); err != nil {
		return err
	}
	servertools.SetSecurityChecker(securitySvc)
	return nil
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
