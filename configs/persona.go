package configs

import (
	"fmt"
	"os"
	"strings"
)

const (
	DefaultPromptFilePath = "./configs/prompt.md"
)

func ResolvePersonaFilePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return DefaultPromptFilePath
	}

	return DefaultPromptFilePath
}

func LoadPersonaPrompt(path string) (string, string, error) {
	resolvedPath := ResolvePersonaFilePath(path)
	if resolvedPath == "" {
		return "", "", nil
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", resolvedPath, fmt.Errorf("read prompt file %q: %w", resolvedPath, err)
	}

	return strings.TrimSpace(string(data)), resolvedPath, nil
}
