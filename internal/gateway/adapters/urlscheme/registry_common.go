package urlscheme

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// normalizeURLSchemeExecutablePath 校验并归一化用于协议注册的可执行文件路径，避免写入无效系统配置。
func normalizeURLSchemeExecutablePath(executablePath string) (string, error) {
	normalizedPath := strings.TrimSpace(executablePath)
	if normalizedPath == "" {
		return "", errors.New("executable path is empty")
	}
	if strings.ContainsAny(normalizedPath, "\r\n") {
		return "", errors.New("executable path contains newline")
	}
	if !filepath.IsAbs(normalizedPath) {
		return "", errors.New("executable path must be absolute")
	}
	return normalizedPath, nil
}

// mapURLSchemeCommandError 将外部命令执行错误统一映射为稳定错误码，保证 CLI 可给出可读反馈。
func mapURLSchemeCommandError(command string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return newDispatchError(ErrorCodeNotSupported, fmt.Sprintf("%s is not available on this system", command))
	}
	return newDispatchError(ErrorCodeInternal, fmt.Sprintf("%s failed: %v", command, err))
}
