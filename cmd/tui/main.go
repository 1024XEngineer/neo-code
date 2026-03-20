package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"go-llm-demo/internal/tui/core"
	"go-llm-demo/internal/tui/infra"
)

func main() {
	loadDotEnv(".env")

	persona := loadPersonaPrompt(os.Getenv("PERSONA_FILE_PATH"))

	client, err := infra.NewLocalChatClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
		os.Exit(1)
	}

	model := core.NewModel(client, persona)

	p := tea.NewProgram(model,
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "运行失败: %v\n", err)
		os.Exit(1)
	}
}

func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || os.Getenv(key) != "" {
			continue
		}

		value = strings.Trim(value, `"'`)
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return nil
}

func loadPersonaPrompt(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return ""
	}

	return strings.TrimSpace(string(data))
}

type ctxKey string

const ctxModelKey ctxKey = "model"

func withModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, ctxModelKey, model)
}

func getModel(ctx context.Context) string {
	if v := ctx.Value(ctxModelKey); v != nil {
		return v.(string)
	}
	return ""
}
