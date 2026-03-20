package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"go-llm-demo/internal/server/infra/provider"
	"go-llm-demo/internal/server/infra/repository"
	"go-llm-demo/internal/server/service"
)

func main() {
	loadDotEnv(".env")

	memoryRepo := repository.NewFileMemoryStore(
		os.Getenv("MEMORY_FILE_PATH"),
		1000,
	)

	memorySvc := service.NewMemoryService(memoryRepo, 5, 0.75)

	roleRepo := repository.NewFileRoleStore(os.Getenv("ROLE_FILE_PATH"))
	roleSvc := service.NewRoleService(roleRepo, os.Getenv("PERSONA_FILE_PATH"))

	chatProvider, err := provider.NewChatProviderFromEnv(os.Getenv("AI_MODEL"))
	if err != nil {
		fmt.Printf("初始化 ChatProvider 失败：%v\n", err)
		return
	}

	chatGateway := service.NewChatService(memorySvc, roleSvc, chatProvider)

	fmt.Printf("Server initialized with services: %+v\n", chatGateway)
	fmt.Println("Note: This is a placeholder. Actual server implementation goes here.")
}

func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := string(data)
	scanner := bufio.NewScanner(strings.NewReader(lines))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && os.Getenv(key) == "" {
			os.Setenv(key, strings.Trim(value, `"'`))
		}
	}

	return scanner.Err()
}
