package ptyproxy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	diagSocketRunRelativeDir = ".neocode/run"
	diagSocketFilePrefix     = "neocode-diag-"
	diagSocketFileSuffix     = ".sock"
)

// ResolveDefaultDiagSocketPath 解析当前进程默认诊断 socket 路径。
func ResolveDefaultDiagSocketPath() (string, error) {
	return resolveDiagSocketPathForPID(os.Getpid())
}

// ResolveLatestRunDiagSocketPath 返回 ~/.neocode/run 下最近修改的诊断 socket 路径。
func ResolveLatestRunDiagSocketPath() (string, error) {
	runDir, err := resolveDiagSocketRunDir()
	if err != nil {
		return "", err
	}
	return findLatestSocketByPattern(runDir, diagSocketFilePrefix+"*"+diagSocketFileSuffix)
}

// ResolveLegacyTmpDiagSocketPath 返回临时目录下最近修改的遗留诊断 socket 路径。
func ResolveLegacyTmpDiagSocketPath() (string, error) {
	return findLatestSocketByPattern(os.TempDir(), diagSocketFilePrefix+"*"+diagSocketFileSuffix)
}

// ResolveLegacyTmpDiagSocketPathForPID 返回临时目录下与指定 PID 匹配的遗留诊断 socket 路径。
func ResolveLegacyTmpDiagSocketPathForPID(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("ptyproxy: invalid diag socket pid %d", pid)
	}
	pattern := diagSocketFilePrefix + strconv.Itoa(pid) + diagSocketFileSuffix
	return findLatestSocketByPattern(os.TempDir(), pattern)
}

// resolveDiagSocketPathForPID 按约定生成带 PID 的诊断 socket 路径。
func resolveDiagSocketPathForPID(pid int) (string, error) {
	runDir, err := resolveDiagSocketRunDir()
	if err != nil {
		return "", err
	}
	if pid <= 0 {
		pid = os.Getpid()
	}
	return filepath.Join(runDir, diagSocketFilePrefix+strconv.Itoa(pid)+diagSocketFileSuffix), nil
}

// parseDiagSocketPIDFromPath 从诊断 socket 文件名中解析 PID。
func parseDiagSocketPIDFromPath(socketPath string) (int, error) {
	base := strings.TrimSpace(filepath.Base(strings.TrimSpace(socketPath)))
	if base == "." || base == "" {
		return 0, fmt.Errorf("ptyproxy: diag socket path is empty")
	}
	if !strings.HasPrefix(base, diagSocketFilePrefix) || !strings.HasSuffix(base, diagSocketFileSuffix) {
		return 0, fmt.Errorf("ptyproxy: diag socket filename is invalid: %s", base)
	}
	rawPID := strings.TrimPrefix(base, diagSocketFilePrefix)
	rawPID = strings.TrimSuffix(rawPID, diagSocketFileSuffix)
	pid, err := strconv.Atoi(rawPID)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("ptyproxy: diag socket pid is invalid: %s", rawPID)
	}
	return pid, nil
}

// resolveDiagSocketRunDir 解析诊断 socket 的统一运行目录。
func resolveDiagSocketRunDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ptyproxy: resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, diagSocketRunRelativeDir), nil
}

// findLatestSocketByPattern 从目录中挑选最新的匹配 socket 文件。
func findLatestSocketByPattern(root string, pattern string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(root, pattern))
	if err != nil {
		return "", fmt.Errorf("ptyproxy: glob diag socket path: %w", err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("ptyproxy: no diag socket found in %s", strings.TrimSpace(root))
	}

	type candidate struct {
		path    string
		modTime time.Time
	}
	candidates := make([]candidate, 0, len(matches))
	for _, matchedPath := range matches {
		info, statErr := os.Stat(matchedPath)
		if statErr != nil {
			continue
		}
		if info.Mode()&os.ModeSocket == 0 {
			continue
		}
		candidates = append(candidates, candidate{
			path:    matchedPath,
			modTime: info.ModTime(),
		})
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("ptyproxy: no socket candidate found in %s", strings.TrimSpace(root))
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return filepath.Clean(candidates[0].path), nil
}
