package configs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultPersonaFilePath = "./configs/persona.txt"
	legacyPersonaFilePath  = "./persona.txt"
)

func ResolvePersonaFilePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}

	candidates := []string{trimmed}
	if trimmed == legacyPersonaFilePath || trimmed == "persona.txt" {
		candidates = append(candidates, DefaultPersonaFilePath, "configs/persona.txt")
	}
	candidates = append(candidates, resolveRelativePersonaCandidates(trimmed)...)

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return trimmed
}

func resolveRelativePersonaCandidates(path string) []string {
	if filepath.IsAbs(path) {
		return nil
	}

	normalized := filepath.Clean(strings.TrimPrefix(path, "./"))
	if normalized == "." || normalized == "" {
		return nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil
	}

	var candidates []string
	seen := map[string]struct{}{}
	for dir := wd; dir != ""; {
		candidate := filepath.Clean(filepath.Join(dir, normalized))
		if _, ok := seen[candidate]; !ok {
			candidates = append(candidates, candidate)
			seen[candidate] = struct{}{}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return candidates
}

func LoadPersonaPrompt(path string) (string, string, error) {
	resolvedPath := ResolvePersonaFilePath(path)
	if resolvedPath == "" {
		return "", "", nil
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", resolvedPath, fmt.Errorf("read persona file %q: %w", resolvedPath, err)
	}

	return strings.TrimSpace(string(data)), resolvedPath, nil
}
