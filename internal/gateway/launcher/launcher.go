package launcher

import (
	"fmt"
	"os"
	"os/exec"
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
	resolveByLookup := func(binary string) (string, error) {
		resolved, err := lookPathFn(strings.TrimSpace(binary))
		if err != nil {
			return "", fmt.Errorf("resolve executable %q: %w", strings.TrimSpace(binary), err)
		}
		return strings.TrimSpace(resolved), nil
	}

	explicitBinary := strings.TrimSpace(options.ExplicitBinary)
	if explicitBinary != "" {
		resolved, err := resolveByLookup(explicitBinary)
		if err != nil {
			return LaunchSpec{}, err
		}
		return LaunchSpec{
			LaunchMode: LaunchModeExplicitPath,
			Executable: resolved,
		}, nil
	}

	if resolved, err := resolveByLookup("neocode-gateway"); err == nil {
		return LaunchSpec{
			LaunchMode: LaunchModePathBinary,
			Executable: resolved,
		}, nil
	}

	resolvedFallbackExecutable, err := resolveByLookup("neocode")
	if err != nil {
		return LaunchSpec{}, err
	}

	return LaunchSpec{
		LaunchMode: LaunchModeFallbackSubcommand,
		Executable: resolvedFallbackExecutable,
		Args:       []string{"gateway"},
	}, nil
}
