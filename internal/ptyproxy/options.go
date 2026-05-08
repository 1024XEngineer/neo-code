package ptyproxy

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// DiagSocketEnv 是 shell 子进程内用于定位普通诊断 socket 的环境变量名。
	DiagSocketEnv = "NEOCODE_DIAG_SOCKET"
	// IDMDiagSocketEnv 是 shell 子进程内用于定位 IDM 诊断 socket 的环境变量名。
	IDMDiagSocketEnv = "NEOCODE_IDM_SOCKET"
	// DiagAltScreenGuardDisableEnv 用于快速禁用全屏抑制逻辑，便于紧急回滚。
	DiagAltScreenGuardDisableEnv = "NEOCODE_DIAG_ALTSCREEN_GUARD_DISABLED"
	// IDMSessionPlanModeDisableEnv 用于关闭 IDM @ai 的 plan 模式注入，便于紧急回滚。
	IDMSessionPlanModeDisableEnv = "NEOCODE_IDM_PLAN_MODE_DISABLED"
	// DiagFastResponseDisableEnv 用于关闭诊断快速首响，便于对比和回滚。
	DiagFastResponseDisableEnv = "NEOCODE_DIAG_FAST_RESPONSE_DISABLED"
	// DiagCacheDisableEnv 用于关闭诊断缓存和 in-flight 合并，便于对比和回滚。
	DiagCacheDisableEnv = "NEOCODE_DIAG_CACHE_DISABLED"
	// DefaultRingBufferCapacity 定义诊断日志缓存窗口的默认字节上限（64KB）。
	DefaultRingBufferCapacity = 64 * 1024
)

// ManualShellOptions 定义 Manual 模式代理 shell 的启动参数。
type ManualShellOptions struct {
	Workdir              string
	Shell                string
	SocketPath           string
	GatewayListenAddress string
	GatewayTokenFile     string
	Stdin                io.Reader
	Stdout               io.Writer
	Stderr               io.Writer
}

// NormalizeShellOptions 补齐默认 I/O 与工作目录，避免调用方遗漏基础参数。
func NormalizeShellOptions(options ManualShellOptions) (ManualShellOptions, error) {
	normalized := options
	if normalized.Stdin == nil {
		normalized.Stdin = os.Stdin
	}
	if normalized.Stdout == nil {
		normalized.Stdout = os.Stdout
	}
	if normalized.Stderr == nil {
		normalized.Stderr = os.Stderr
	}

	workdir := strings.TrimSpace(normalized.Workdir)
	if workdir == "" {
		currentDir, err := os.Getwd()
		if err != nil {
			return ManualShellOptions{}, fmt.Errorf("ptyproxy: resolve current workdir: %w", err)
		}
		workdir = currentDir
	}
	absoluteWorkdir, err := filepath.Abs(filepath.Clean(workdir))
	if err != nil {
		return ManualShellOptions{}, fmt.Errorf("ptyproxy: resolve workdir: %w", err)
	}
	normalized.Workdir = absoluteWorkdir
	normalized.Shell = strings.TrimSpace(normalized.Shell)
	normalized.SocketPath = strings.TrimSpace(normalized.SocketPath)
	normalized.GatewayListenAddress = strings.TrimSpace(normalized.GatewayListenAddress)
	normalized.GatewayTokenFile = strings.TrimSpace(normalized.GatewayTokenFile)
	return normalized, nil
}

// MergeEnvVar 以覆盖方式注入环境变量，确保同名旧值不会污染子进程。
func MergeEnvVar(environment []string, key string, value string) []string {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return append([]string(nil), environment...)
	}
	normalizedValue := strings.TrimSpace(value)
	prefix := strings.ToUpper(trimmedKey) + "="
	merged := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if strings.HasPrefix(strings.ToUpper(item), prefix) {
			continue
		}
		merged = append(merged, item)
	}
	merged = append(merged, trimmedKey+"="+normalizedValue)
	return merged
}

// IsAltScreenGuardEnabledFromEnv 根据环境变量决定是否开启全屏抑制逻辑。
func IsAltScreenGuardEnabledFromEnv() bool {
	value := strings.TrimSpace(os.Getenv(DiagAltScreenGuardDisableEnv))
	if value == "" {
		return true
	}
	disabled, err := strconv.ParseBool(value)
	if err == nil {
		return !disabled
	}
	// 兼容运维兜底：只要显式设置了无法解析的非空值，也视为禁用。
	return false
}

// IsIDMPlanModeEnabledFromEnv 根据环境变量决定 IDM @ai 是否显式进入 plan 模式。
func IsIDMPlanModeEnabledFromEnv() bool {
	return !isTruthyEnv(IDMSessionPlanModeDisableEnv)
}

// IsDiagFastResponseEnabledFromEnv 根据环境变量决定是否输出诊断快速首响。
func IsDiagFastResponseEnabledFromEnv() bool {
	return !isTruthyEnv(DiagFastResponseDisableEnv)
}

// IsDiagCacheEnabledFromEnv 根据环境变量决定是否开启诊断缓存与 in-flight 合并。
func IsDiagCacheEnabledFromEnv() bool {
	return !isTruthyEnv(DiagCacheDisableEnv)
}

// isTruthyEnv 以宽松布尔语义识别回滚开关是否开启。
func isTruthyEnv(name string) bool {
	value := strings.TrimSpace(os.Getenv(strings.TrimSpace(name)))
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return true
	}
	return parsed
}
