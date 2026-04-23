package bash

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"neo-code/internal/security"
	"neo-code/internal/tools"
)

// SecurityExecutor is the bash execution boundary that enforces workspace and
// runtime safety checks before running a shell command.
type SecurityExecutor interface {
	Execute(ctx context.Context, call tools.ToolCallInput, command string, requestedWorkdir string) (tools.ToolResult, error)
}

type commandRunner interface {
	CombinedOutput(ctx context.Context, binary string, args []string, workdir string, env []string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) CombinedOutput(
	ctx context.Context,
	binary string,
	args []string,
	workdir string,
	env []string,
) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = workdir
	if len(env) > 0 {
		cmd.Env = env
	}
	return cmd.CombinedOutput()
}

type defaultSecurityExecutor struct {
	root    string
	shell   string
	timeout time.Duration
	runner  commandRunner
}

var hardenedGitReadOnlySubcommands = map[string]struct{}{
	"status":    {},
	"rev-parse": {},
	"describe":  {},
	"diff":      {},
	"log":       {},
	"show":      {},
}

// NewDefaultSecurityExecutor returns the default secure bash executor.
func NewDefaultSecurityExecutor(root string, shell string, timeout time.Duration) SecurityExecutor {
	return &defaultSecurityExecutor{
		root:    root,
		shell:   shell,
		timeout: timeout,
		runner:  execCommandRunner{},
	}
}

func (e *defaultSecurityExecutor) Execute(
	ctx context.Context,
	call tools.ToolCallInput,
	command string,
	requestedWorkdir string,
) (tools.ToolResult, error) {
	if strings.TrimSpace(command) == "" {
		err := errors.New("bash: command is empty")
		return tools.NewErrorResult("bash", tools.NormalizeErrorReason("bash", err), "", nil), err
	}
	intent := tools.AnalyzeBashCommand(command)

	base := strings.TrimSpace(call.Workdir)
	if base == "" {
		base = e.root
	}
	_, workdir, err := tools.ResolveWorkspaceTarget(
		call,
		security.TargetTypeDirectory,
		base,
		requestedWorkdir,
		resolveWorkdir,
	)
	if err != nil {
		return tools.NewErrorResult("bash", tools.NormalizeErrorReason("bash", err), "", nil), err
	}

	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	commandEnv, envHardened, envErr := buildCommandEnv(intent)
	if envErr != nil {
		result := tools.NewErrorResult(
			"bash",
			tools.NormalizeErrorReason("bash", envErr),
			"",
			bashResultMetadata(workdir, intent, commandExitCode(envErr), false, false),
		)
		return tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes), envErr
	}

	binary, args := shellCommand(e.shell, command)
	output, runErr := e.runner.CombinedOutput(runCtx, binary, args, workdir, commandEnv)
	content := string(output)
	metadata := bashResultMetadata(workdir, intent, commandExitCode(runErr), runErr == nil, envHardened)
	if runErr != nil {
		result := tools.NewErrorResult(
			"bash",
			tools.NormalizeErrorReason("bash", runErr),
			content,
			metadata,
		)
		result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
		return result, runErr
	}

	result := tools.ToolResult{
		Name:     "bash",
		Content:  content,
		Metadata: metadata,
	}
	result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
	return result, nil
}

// bashResultMetadata 构造 bash 工具统一元数据，确保模型可见执行语义与成功标记。
func bashResultMetadata(
	workdir string,
	intent tools.BashSemanticIntent,
	exitCode int,
	ok bool,
	envHardened bool,
) map[string]any {
	metadata := map[string]any{
		"workdir":        workdir,
		"ok":             ok,
		"exit_code":      exitCode,
		"classification": strings.TrimSpace(intent.Classification),
		"env_hardened":   envHardened,
	}
	if normalized := strings.TrimSpace(intent.NormalizedIntent); normalized != "" {
		metadata["normalized_intent"] = normalized
	}
	if fingerprint := strings.TrimSpace(intent.PermissionFingerprint); fingerprint != "" {
		metadata["permission_fingerprint"] = fingerprint
	}
	return metadata
}

// buildCommandEnv 根据语义构造子进程环境，确保 git 只读命令在隔离环境中执行。
func buildCommandEnv(intent tools.BashSemanticIntent) ([]string, bool, error) {
	if !shouldHardenGitReadOnlyExecution(intent) {
		return os.Environ(), false, nil
	}
	if !isHardenedGitReadOnlySubcommand(intent.Subcommand) {
		return nil, false, fmt.Errorf("bash: git read-only subcommand %q is not allowed for auto execution", intent.Subcommand)
	}
	if intent.ParseError || intent.Composite || strings.TrimSpace(intent.Subcommand) == "" {
		return nil, false, errors.New("bash: cannot safely classify git read-only command")
	}
	env, err := sanitizeGitReadOnlyEnv(os.Environ())
	if err != nil {
		return nil, false, err
	}
	return env, true, nil
}

// shouldHardenGitReadOnlyExecution 判断是否应对当前命令启用 Git 只读环境隔离。
func shouldHardenGitReadOnlyExecution(intent tools.BashSemanticIntent) bool {
	return intent.IsGit && strings.TrimSpace(intent.Classification) == tools.BashIntentClassificationReadOnly
}

// isHardenedGitReadOnlySubcommand 校验自动放行的 Git 只读子命令白名单。
func isHardenedGitReadOnlySubcommand(subcommand string) bool {
	normalized := strings.ToLower(strings.TrimSpace(subcommand))
	_, ok := hardenedGitReadOnlySubcommands[normalized]
	return ok
}

// sanitizeGitReadOnlyEnv 清洗并重建环境变量，降低 Git 配置触发外部程序的风险。
func sanitizeGitReadOnlyEnv(baseEnv []string) ([]string, error) {
	filtered := make(map[string]string, len(baseEnv)+16)
	for _, entry := range baseEnv {
		if shouldIgnoreInheritedEnvEntry(entry) {
			continue
		}
		key, value, ok := splitEnvEntry(entry)
		if !ok {
			return nil, fmt.Errorf("bash: invalid environment entry %q", entry)
		}
		if shouldDropGitInheritedEnv(key) {
			continue
		}
		filtered[key] = value
	}

	for key, value := range gitReadOnlyEnvOverrides() {
		filtered[key] = value
	}

	keys := make([]string, 0, len(filtered))
	for key := range filtered {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+filtered[key])
	}
	return env, nil
}

// shouldIgnoreInheritedEnvEntry 过滤 Windows 等平台注入的伪环境项，避免误判为非法 KEY=VALUE。
func shouldIgnoreInheritedEnvEntry(entry string) bool {
	return strings.HasPrefix(entry, "=")
}

// splitEnvEntry 拆解 KEY=VALUE 形态的环境项，拒绝异常格式避免隐式继承污染值。
func splitEnvEntry(entry string) (string, string, bool) {
	idx := strings.Index(entry, "=")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(entry[:idx])
	if key == "" {
		return "", "", false
	}
	return key, entry[idx+1:], true
}

// shouldDropGitInheritedEnv 判断环境项是否属于高风险 Git 继承变量。
func shouldDropGitInheritedEnv(key string) bool {
	upperKey := strings.ToUpper(strings.TrimSpace(key))
	if upperKey == "" {
		return false
	}
	if strings.HasPrefix(upperKey, "GIT_CONFIG_") {
		return true
	}
	switch upperKey {
	case "GIT_PAGER", "PAGER", "GIT_EDITOR", "GIT_ASKPASS", "GIT_EXTERNAL_DIFF", "GIT_DIFF_OPTS":
		return true
	default:
		return false
	}
}

// gitReadOnlyEnvOverrides 返回 Git 只读命令强制注入的环境变量覆盖集。
func gitReadOnlyEnvOverrides() map[string]string {
	nullDevice := gitNullDevicePath()
	return map[string]string{
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   nullDevice,
		"GIT_CONFIG_COUNT":    "4",
		"GIT_CONFIG_KEY_0":    "core.pager",
		"GIT_CONFIG_VALUE_0":  "cat",
		"GIT_CONFIG_KEY_1":    "diff.external",
		"GIT_CONFIG_VALUE_1":  "",
		"GIT_CONFIG_KEY_2":    "core.askpass",
		"GIT_CONFIG_VALUE_2":  "",
		"GIT_CONFIG_KEY_3":    "pager.log",
		"GIT_CONFIG_VALUE_3":  "false",
		"GIT_PAGER":           "cat",
		"PAGER":               "cat",
		"GIT_EDITOR":          "cat",
		"GIT_ASKPASS":         "",
		"GIT_EXTERNAL_DIFF":   "",
		"GIT_DIFF_OPTS":       "",
	}
}

// gitNullDevicePath 返回当前平台下用于屏蔽文件输入的空设备路径。
func gitNullDevicePath() string {
	if runtime.GOOS == "windows" {
		return "NUL"
	}
	return "/dev/null"
}

// commandExitCode 提取命令退出码；无法确定时返回 -1。
func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr interface{ ExitCode() int }
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func shellCommand(shell string, command string) (string, []string) {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "powershell", "pwsh":
		return "powershell", []string{"-NoProfile", "-Command", command}
	case "bash":
		return "bash", []string{"-lc", command}
	case "sh":
		return "sh", []string{"-lc", command}
	}

	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-Command", command}
	}
	return "sh", []string{"-lc", command}
}

func resolveWorkdir(root string, requested string) (string, error) {
	if strings.ContainsRune(root, '\x00') || strings.ContainsRune(requested, '\x00') {
		return "", errors.New("bash: invalid path contains NUL")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := requested
	if strings.TrimSpace(target) == "" {
		target = base
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("bash: workdir escapes workspace root")
	}
	return target, nil
}
