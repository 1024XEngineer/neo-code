package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// EnvGatewayBinary 定义显式网关可执行路径的环境变量名。
	EnvGatewayBinary = "NEOCODE_GATEWAY_BIN"
	// LaunchModeExplicitPath 表示命中显式路径配置。
	LaunchModeExplicitPath = "explicit_path"
	// LaunchModePathBinary 表示命中 PATH 中的 neocode-gateway。
	LaunchModePathBinary = "path_neocode_gateway"
	// LaunchModeFallbackSubcommand 表示回退到 PATH 中 neocode 的 gateway 子命令。
	LaunchModeFallbackSubcommand = "fallback_neocode_gateway_subcommand"
)

// LaunchSpec 描述网关拉起决策结果。
type LaunchSpec struct {
	LaunchMode string
	Executable string
	Args       []string
}

// ResolveOptions 描述网关拉起解析所需输入。
type ResolveOptions struct {
	ExplicitBinary string
}

// ResolveGatewayLaunchSpec 解析网关可执行发现顺序：
// 显式路径(NEOCODE_GATEWAY_BIN) > PATH(neocode-gateway) > PATH(neocode) + gateway 子命令。
func ResolveGatewayLaunchSpec(options ResolveOptions) (LaunchSpec, error) {
	return resolveGatewayLaunchSpecWithDeps(options, exec.LookPath)
}

// StartDetachedGateway 以非阻塞方式拉起网关进程并释放父进程句柄。
func StartDetachedGateway(spec LaunchSpec) error {
	executable := strings.TrimSpace(spec.Executable)
	if executable == "" {
		return fmt.Errorf("empty gateway executable")
	}
	command := exec.Command(executable, spec.Args...)
	command.Stdin = nil
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func resolveGatewayLaunchSpecWithDeps(
	options ResolveOptions,
	lookPathFn func(string) (string, error),
) (LaunchSpec, error) {
	explicitBinary := strings.TrimSpace(options.ExplicitBinary)
	if explicitBinary != "" {
		if err := validateExplicitGatewayBinary(explicitBinary); err != nil {
			return LaunchSpec{}, err
		}
		spec, err := resolveLaunchSpecCandidate(
			lookPathFn,
			explicitBinary,
			LaunchModeExplicitPath,
			nil,
			"explicit gateway binary",
		)
		if err != nil {
			return LaunchSpec{}, err
		}
		return spec, nil
	}

	resolvedPathBinary, err := resolveExecutablePath(lookPathFn, "neocode-gateway")
	if err == nil {
		return resolveLaunchSpecFromResolvedPath(
			resolvedPathBinary,
			LaunchModePathBinary,
			nil,
			"PATH neocode-gateway",
		)
	}

	return resolveLaunchSpecCandidate(
		lookPathFn,
		"neocode",
		LaunchModeFallbackSubcommand,
		[]string{"gateway"},
		"PATH neocode",
	)
}

// resolveLaunchSpecCandidate 统一处理可执行查找、绝对路径校验与 LaunchSpec 构造。
func resolveLaunchSpecCandidate(
	lookPathFn func(string) (string, error),
	binary string,
	launchMode string,
	args []string,
	source string,
) (LaunchSpec, error) {
	resolvedPath, err := resolveExecutablePath(lookPathFn, binary)
	if err != nil {
		return LaunchSpec{}, err
	}
	return resolveLaunchSpecFromResolvedPath(resolvedPath, launchMode, args, source)
}

// resolveLaunchSpecFromResolvedPath 基于已解析的路径构造启动规格，并保留绝对路径校验。
func resolveLaunchSpecFromResolvedPath(
	resolvedPath string,
	launchMode string,
	args []string,
	source string,
) (LaunchSpec, error) {
	if err := validateResolvedExecutablePath(resolvedPath, source); err != nil {
		return LaunchSpec{}, err
	}
	return LaunchSpec{
		LaunchMode: launchMode,
		Executable: resolvedPath,
		Args:       append([]string(nil), args...),
	}, nil
}

// resolveExecutablePath 统一处理可执行路径查找与空白归一化。
func resolveExecutablePath(lookPathFn func(string) (string, error), binary string) (string, error) {
	trimmedBinary := strings.TrimSpace(binary)
	resolvedPath, err := lookPathFn(trimmedBinary)
	if err != nil {
		return "", fmt.Errorf("resolve executable %q: %w", trimmedBinary, err)
	}
	return strings.TrimSpace(resolvedPath), nil
}

// validateExplicitGatewayBinary 校验显式配置的网关二进制路径，禁止使用相对路径降低 PATH 劫持风险。
func validateExplicitGatewayBinary(explicitBinary string) error {
	if !isAbsolutePath(explicitBinary) {
		return fmt.Errorf("explicit gateway binary must be an absolute path: %q", explicitBinary)
	}
	return nil
}

// validateResolvedExecutablePath 校验解析后的可执行路径必须为绝对路径，避免执行不受控相对路径目标。
func validateResolvedExecutablePath(resolvedPath string, source string) error {
	if !isAbsolutePath(resolvedPath) {
		return fmt.Errorf("resolved executable from %s is not an absolute path: %q", source, resolvedPath)
	}
	return nil
}

// isAbsolutePath 按当前平台原生语义校验绝对路径，避免放宽到依赖环境状态的跨平台路径解释。
func isAbsolutePath(p string) bool {
	return filepath.IsAbs(p)
}
