package infra

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var runtimeGOOSForOpenResource = runtime.GOOS
var execCommandForOpenResource = exec.Command
var absPathForOpenResource = filepath.Abs

// OpenExternalResource 在本机默认应用中打开 URL 或本地文件。
func OpenExternalResource(target string) error {
	normalizedTarget, err := normalizeOpenResourceTarget(target)
	if err != nil {
		return err
	}

	commandName, commandArgs, err := openResourceCommand(runtimeGOOSForOpenResource, normalizedTarget)
	if err != nil {
		return err
	}
	command := execCommandForOpenResource(commandName, commandArgs...)
	if runErr := command.Run(); runErr != nil {
		return fmt.Errorf("open resource %q: %w", normalizedTarget, runErr)
	}
	return nil
}

// normalizeOpenResourceTarget 将输入归一化为可打开的 URL 或存在的绝对文件路径。
func normalizeOpenResourceTarget(target string) (string, error) {
	trimmedTarget := strings.TrimSpace(target)
	if trimmedTarget == "" {
		return "", fmt.Errorf("open resource: target is empty")
	}

	if parsed, parseErr := url.Parse(trimmedTarget); parseErr == nil && parsed != nil {
		scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
		if scheme == "http" || scheme == "https" {
			return trimmedTarget, nil
		}
		if scheme == "file" {
			filePath, fileErr := fileURLToLocalPath(parsed)
			if fileErr != nil {
				return "", fileErr
			}
			return normalizeOpenResourceLocalPath(filePath)
		}
	}

	return normalizeOpenResourceLocalPath(trimmedTarget)
}

// openResourceCommand 根据平台构造打开目标资源的命令。
func openResourceCommand(goos string, target string) (string, []string, error) {
	switch strings.ToLower(strings.TrimSpace(goos)) {
	case "windows":
		return "cmd", []string{"/c", "start", "", target}, nil
	case "darwin":
		return "open", []string{target}, nil
	default:
		return "xdg-open", []string{target}, nil
	}
}

// normalizeOpenResourceLocalPath 将本地路径归一化为存在且非目录的绝对路径。
func normalizeOpenResourceLocalPath(path string) (string, error) {
	absolutePath := strings.TrimSpace(path)
	if absolutePath == "" {
		return "", fmt.Errorf("open resource: local path is empty")
	}
	if !filepath.IsAbs(absolutePath) {
		resolvedPath, resolveErr := absPathForOpenResource(absolutePath)
		if resolveErr != nil {
			return "", fmt.Errorf("open resource: resolve absolute path: %w", resolveErr)
		}
		absolutePath = resolvedPath
	}
	fileInfo, statErr := os.Stat(absolutePath)
	if statErr != nil {
		return "", fmt.Errorf("open resource: stat %q: %w", absolutePath, statErr)
	}
	if fileInfo.IsDir() {
		return "", fmt.Errorf("open resource: %q is a directory", absolutePath)
	}
	return absolutePath, nil
}

// fileURLToLocalPath 将 file:// URL 解析为本地文件路径，并校验 host 合法性。
func fileURLToLocalPath(parsed *url.URL) (string, error) {
	if parsed == nil {
		return "", fmt.Errorf("open resource: invalid file url")
	}

	host := strings.TrimSpace(parsed.Host)
	if host != "" && !strings.EqualFold(host, "localhost") &&
		!(strings.EqualFold(runtimeGOOSForOpenResource, "windows") && hasWindowsDriveHost(host)) {
		return "", fmt.Errorf("open resource: unsupported file url host %q", host)
	}

	decodedPath, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", fmt.Errorf("open resource: decode file url path: %w", err)
	}
	decodedPath = strings.TrimSpace(decodedPath)
	if decodedPath == "" {
		return "", fmt.Errorf("open resource: file url path is empty")
	}

	if strings.EqualFold(runtimeGOOSForOpenResource, "windows") {
		if hasWindowsDriveHost(host) {
			decodedPath = host + decodedPath
		}
		if hasWindowsFileURLDrivePrefix(decodedPath) {
			decodedPath = decodedPath[1:]
		}
		decodedPath = filepath.FromSlash(decodedPath)
	}
	return decodedPath, nil
}

// hasWindowsFileURLDrivePrefix 判断 file URL 路径是否为 /C:/... 形态。
func hasWindowsFileURLDrivePrefix(path string) bool {
	if len(path) < 3 || path[0] != '/' || path[2] != ':' {
		return false
	}
	drive := path[1]
	return (drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')
}

// hasWindowsDriveHost 判断 host 是否为 Windows 盘符形式（如 C:）。
func hasWindowsDriveHost(host string) bool {
	if len(host) != 2 || host[1] != ':' {
		return false
	}
	drive := host[0]
	return (drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')
}
