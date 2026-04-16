package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const envFileName = ".env"

// EnvFilePath returns the persisted env file path under the config base dir.
func EnvFilePath(baseDir string) string {
	trimmed := strings.TrimSpace(baseDir)
	if trimmed == "" {
		trimmed = defaultBaseDir()
	}
	return filepath.Join(trimmed, envFileName)
}

// PersistEnvVar upserts an env key/value pair into the persisted .env file.
func PersistEnvVar(baseDir string, key string, value string) error {
	normalizedKey := strings.TrimSpace(key)
	if normalizedKey == "" {
		return errors.New("config: env key is empty")
	}
	if strings.ContainsAny(normalizedKey, " \t\r\n=") {
		return fmt.Errorf("config: env key %q is invalid", normalizedKey)
	}
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("config: env value contains newline")
	}

	envPath := EnvFilePath(baseDir)
	if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
		return fmt.Errorf("config: create env dir: %w", err)
	}

	var lines []string
	data, readErr := os.ReadFile(envPath)
	switch {
	case readErr == nil:
		lines = strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	case os.IsNotExist(readErr):
		lines = nil
	default:
		return fmt.Errorf("config: read env file: %w", readErr)
	}

	updated := false
	for i := range lines {
		currentKey, _, ok := parseEnvAssignment(lines[i])
		if !ok {
			continue
		}
		if currentKey == normalizedKey {
			lines[i] = formatEnvAssignment(normalizedKey, value)
			updated = true
			break
		}
	}
	if !updated {
		lines = append(lines, formatEnvAssignment(normalizedKey, value))
	}

	content := strings.Join(lines, "\n")
	content = strings.TrimRight(content, "\n") + "\n"
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("config: write env file: %w", err)
	}
	return nil
}

// LoadPersistedEnv loads key/value pairs from persisted .env into process env.
// Existing process env keys are kept as-is.
func LoadPersistedEnv(baseDir string) error {
	envPath := EnvFilePath(baseDir)
	data, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config: read env file: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for _, line := range lines {
		key, value, ok := parseEnvAssignment(line)
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("config: set env %q: %w", key, err)
		}
	}
	return nil
}

// RemovePersistedEnvVar 从持久化 .env 文件中删除指定键；文件不存在时视为成功。
func RemovePersistedEnvVar(baseDir string, key string) error {
	normalizedKey := strings.TrimSpace(key)
	if normalizedKey == "" {
		return errors.New("config: env key is empty")
	}
	if strings.ContainsAny(normalizedKey, " \t\r\n=") {
		return fmt.Errorf("config: env key %q is invalid", normalizedKey)
	}

	envPath := EnvFilePath(baseDir)
	data, err := os.ReadFile(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config: read env file: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		currentKey, _, ok := parseEnvAssignment(line)
		if ok && currentKey == normalizedKey {
			continue
		}
		filtered = append(filtered, line)
	}

	content := strings.Join(filtered, "\n")
	content = strings.TrimRight(content, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("config: write env file: %w", err)
	}
	return nil
}

func parseEnvAssignment(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}

	eq := strings.IndexByte(trimmed, '=')
	if eq <= 0 {
		return "", "", false
	}

	key := strings.TrimSpace(trimmed[:eq])
	if key == "" {
		return "", "", false
	}
	rawValue := strings.TrimSpace(trimmed[eq+1:])
	return key, parseEnvValue(rawValue), true
}

func parseEnvValue(raw string) string {
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1]
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		if unquoted, err := strconv.Unquote(raw); err == nil {
			return unquoted
		}
		return raw[1 : len(raw)-1]
	}
	return raw
}

func formatEnvAssignment(key string, value string) string {
	return key + "=" + encodeEnvValue(value)
}

func encodeEnvValue(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\r\n#\"'") {
		return strconv.Quote(value)
	}
	return value
}
