//go:build windows

package config

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const windowsUserEnvironmentKey = `Environment`

// PersistUserEnvVar persists key/value into Windows user environment variables.
func PersistUserEnvVar(key string, value string) error {
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

	envKey, _, err := registry.CreateKey(registry.CURRENT_USER, windowsUserEnvironmentKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("config: open windows user env: %w", err)
	}
	defer envKey.Close()

	if err := envKey.SetStringValue(normalizedKey, value); err != nil {
		return fmt.Errorf("config: set windows user env %q: %w", normalizedKey, err)
	}
	return nil
}

// DeleteUserEnvVar 删除 Windows 用户级环境变量，不存在时视为成功。
func DeleteUserEnvVar(key string) error {
	normalizedKey := strings.TrimSpace(key)
	if normalizedKey == "" {
		return errors.New("config: env key is empty")
	}
	if strings.ContainsAny(normalizedKey, " \t\r\n=") {
		return fmt.Errorf("config: env key %q is invalid", normalizedKey)
	}

	envKey, err := registry.OpenKey(registry.CURRENT_USER, windowsUserEnvironmentKey, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("config: open windows user env: %w", err)
	}
	defer envKey.Close()

	if err := envKey.DeleteValue(normalizedKey); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("config: delete windows user env %q: %w", normalizedKey, err)
	}
	return nil
}

// LookupUserEnvVar 查询 Windows 用户级环境变量，不存在时返回 exists=false。
func LookupUserEnvVar(key string) (string, bool, error) {
	normalizedKey := strings.TrimSpace(key)
	if normalizedKey == "" {
		return "", false, errors.New("config: env key is empty")
	}
	if strings.ContainsAny(normalizedKey, " \t\r\n=") {
		return "", false, fmt.Errorf("config: env key %q is invalid", normalizedKey)
	}

	envKey, err := registry.OpenKey(registry.CURRENT_USER, windowsUserEnvironmentKey, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("config: open windows user env: %w", err)
	}
	defer envKey.Close()

	value, _, err := envKey.GetStringValue(normalizedKey)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("config: read windows user env %q: %w", normalizedKey, err)
	}
	return value, true, nil
}
