package main

import (
	"fmt"

	"go-llm-demo/config"
	"go-llm-demo/internal/server/infra/provider"
	"go-llm-demo/internal/server/infra/repository"
	"go-llm-demo/internal/server/service"
)

func main() {
	if err := config.LoadAppConfig("config.yaml"); err != nil {
		fmt.Printf("加载配置失败：%v\n", err)
		return
	}

	cfg := config.GlobalAppConfig
	memoryRepo := repository.NewFileMemoryStore(cfg.Memory.StoragePath, cfg.Memory.MaxItems)
	sessionRepo := repository.NewSessionMemoryStore(cfg.Memory.MaxItems)
	memorySvc := service.NewMemoryService(
		memoryRepo,
		sessionRepo,
		cfg.Memory.TopK,
		cfg.Memory.MinMatchScore,
		cfg.Memory.MaxPromptChars,
		cfg.Memory.StoragePath,
		cfg.Memory.PersistTypes,
	)

	roleRepo := repository.NewFileRoleStore("./data/roles.json")
	roleSvc := service.NewRoleService(roleRepo, cfg.Persona.FilePath)

	chatProvider, err := provider.NewChatProvider(cfg.AI.Model)
	if err != nil {
		fmt.Printf("初始化 ChatProvider 失败：%v\n", err)
		return
	}

	chatGateway := service.NewChatService(memorySvc, roleSvc, chatProvider)
	fmt.Printf("Server initialized with services: %+v\n", chatGateway)
	fmt.Println("Note: This is a placeholder. Actual server implementation goes here.")
}
