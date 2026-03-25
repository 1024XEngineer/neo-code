package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"go-llm-demo/configs"
	"go-llm-demo/internal/server/domain"
	"go-llm-demo/internal/server/infra/provider"
	"go-llm-demo/internal/server/infra/repository"
	"go-llm-demo/internal/server/infra/tools"
	"go-llm-demo/internal/server/service"
)

func main() {
	workspaceRoot, err := tools.ResolveWorkspaceRoot("")
	if err != nil {
		fmt.Printf("failed to resolve workspace: %v\n", err)
		return
	}
	if err := tools.SetWorkspaceRoot(workspaceRoot); err != nil {
		fmt.Printf("failed to set workspace: %v\n", err)
		return
	}
	if err := initializeSecurity(filepath.Join(workspaceRoot, "configs", "security")); err != nil {
		fmt.Printf("failed to initialize security: %v\n", err)
		return
	}

	if err := configs.LoadAppConfig("config.yaml"); err != nil {
		fmt.Printf("failed to load config: %v\n", err)
		return
	}

	cfg := configs.GlobalAppConfig
	memoryRepo := repository.NewFileMemoryStore(cfg.Memory.StoragePath, cfg.Memory.MaxItems)
	sessionRepo := repository.NewSessionMemoryStore(cfg.Memory.MaxItems)
	workingRepo := repository.NewWorkingMemoryStore()

	memoryExtractor, err := buildMemoryExtractor(cfg)
	if err != nil {
		fmt.Printf("failed to initialize memory extractor: %v\n", err)
		return
	}
	memorySvc := service.NewMemoryServiceWithExtractor(
		memoryRepo,
		sessionRepo,
		memoryExtractor,
		cfg.Memory.TopK,
		cfg.Memory.MinMatchScore,
		cfg.Memory.MaxPromptChars,
		cfg.Memory.StoragePath,
		cfg.Memory.PersistTypes,
	)
	workingSvc := service.NewWorkingMemoryService(workingRepo, cfg.History.ShortTermTurns, tools.GetWorkspaceRoot())
	projectMemorySvc := service.NewProjectMemoryService(tools.GetWorkspaceRoot(), cfg.Memory.ProjectFiles, cfg.Memory.ProjectPromptChars)

	roleRepo := repository.NewFileRoleStore("./data/roles.json")
	roleSvc := service.NewRoleService(roleRepo, cfg.Persona.FilePath)

	todoRepo := repository.NewInMemoryTodoRepository()
	todoSvc := service.NewTodoService(todoRepo)

	chatProvider, err := provider.NewChatProvider(cfg.AI.Model)
	if err != nil {
		fmt.Printf("failed to initialize chat provider: %v\n", err)
		return
	}

	chatGateway := service.NewChatService(memorySvc, workingSvc, projectMemorySvc, todoSvc, roleSvc, chatProvider)
	fmt.Printf("server dependencies initialized: %+v\n", chatGateway)
	fmt.Println("note: cmd/server is still a wiring check entrypoint, not a production server.")
}

func buildMemoryExtractor(cfg *configs.AppConfiguration) (domain.MemoryExtractor, error) {
	if cfg == nil {
		return service.BuildMemoryExtractor(service.MemoryExtractorModeRule, nil, service.LLMMemoryExtractorOptions{})
	}

	mode := strings.TrimSpace(cfg.Memory.Extractor)
	if mode == "" || strings.EqualFold(mode, service.MemoryExtractorModeRule) {
		return service.BuildMemoryExtractor(service.MemoryExtractorModeRule, nil, service.LLMMemoryExtractorOptions{})
	}

	model := strings.TrimSpace(cfg.Memory.ExtractorModel)
	if model == "" {
		model = strings.TrimSpace(cfg.AI.Model)
	}

	extractorProvider, err := provider.NewChatProvider(model)
	if err != nil {
		if strings.EqualFold(mode, service.MemoryExtractorModeAuto) {
			return service.BuildMemoryExtractor(service.MemoryExtractorModeAuto, nil, service.LLMMemoryExtractorOptions{})
		}
		return nil, err
	}

	return service.BuildMemoryExtractor(mode, extractorProvider, service.LLMMemoryExtractorOptions{
		Timeout: time.Duration(cfg.Memory.ExtractorTimeoutSecond) * time.Second,
	})
}

func initializeSecurity(configDir string) error {
	securityRepo := repository.NewSecurityConfigRepository()
	securitySvc := service.NewSecurityService(securityRepo)
	if err := securitySvc.Initialize(configDir); err != nil {
		return err
	}
	tools.SetSecurityChecker(securitySvc)
	return nil
}
