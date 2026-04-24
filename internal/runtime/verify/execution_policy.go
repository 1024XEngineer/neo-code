package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"neo-code/internal/config"
)

var (
	// ErrVerificationExecutionDenied 表示 verifier 命令被执行策略拒绝。
	ErrVerificationExecutionDenied = errors.New("verification execution denied")
	// ErrVerificationExecutionError 表示 verifier 命令执行过程中发生系统错误。
	ErrVerificationExecutionError = errors.New("verification execution error")
)

var readonlyGitSubcommands = map[string]struct{}{
	"diff":      {},
	"status":    {},
	"show":      {},
	"log":       {},
	"rev-parse": {},
	"ls-files":  {},
}

const (
	defaultVerificationTimeoutSec = 120
	defaultVerificationOutputCap  = 128 * 1024
	shellMetacharacters           = ";|&`$<>()\n\r"
)

// CommandExecutionRequest 描述一次 verifier 命令执行请求。
type CommandExecutionRequest struct {
	Command       string
	Workdir       string
	TimeoutSec    int
	OutputCapByte int
	Policy        config.VerificationExecutionPolicyConfig
}

// CommandExecutionResult 描述 verifier 命令执行结果。
type CommandExecutionResult struct {
	ExitCode    int
	Stdout      string
	Stderr      string
	TimedOut    bool
	Truncated   bool
	DurationMS  int64
	CommandName string
}

// CommandExecutor 约束 verifier 命令执行能力，便于测试替换。
type CommandExecutor interface {
	Execute(ctx context.Context, req CommandExecutionRequest) (CommandExecutionResult, error)
}

// PolicyCommandExecutor 在 runtime 进程内执行 non-interactive verifier 命令。
type PolicyCommandExecutor struct{}

// Execute 在白名单策略下执行 verifier 命令并返回结构化结果。
func (PolicyCommandExecutor) Execute(ctx context.Context, req CommandExecutionRequest) (CommandExecutionResult, error) {
	normalizedCommand := strings.TrimSpace(req.Command)
	commandName := commandHead(normalizedCommand)
	if normalizedCommand == "" || commandName == "" {
		return CommandExecutionResult{}, fmt.Errorf("%w: empty command", ErrVerificationExecutionDenied)
	}

	allowed, reason := isCommandAllowed(commandName, normalizedCommand, req.Policy)
	if !allowed {
		return CommandExecutionResult{}, fmt.Errorf("%w: %s", ErrVerificationExecutionDenied, reason)
	}

	timeoutSec := firstPositive(req.TimeoutSec, req.Policy.DefaultTimeout, defaultVerificationTimeoutSec)
	outputCap := firstPositive(req.OutputCapByte, req.Policy.DefaultOutputCap, defaultVerificationOutputCap)

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := shellCommand(runCtx, normalizedCommand)
	if workdir := strings.TrimSpace(req.Workdir); workdir != "" {
		cmd.Dir = workdir
	}
	cmd.Env = append(os.Environ(),
		"CI=1",
		"GIT_TERMINAL_PROMPT=0",
	)

	stdout := newCappedBuffer(outputCap)
	stderr := newCappedBuffer(outputCap)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)
	result := CommandExecutionResult{
		ExitCode:    0,
		Stdout:      stdout.String(),
		Stderr:      stderr.String(),
		Truncated:   stdout.Truncated() || stderr.Truncated(),
		DurationMS:  duration.Milliseconds(),
		CommandName: commandName,
	}
	if runErr == nil {
		return result, nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		return result, fmt.Errorf("%w: command timeout", ErrVerificationExecutionError)
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, fmt.Errorf("%w: %v", ErrVerificationExecutionError, runErr)
}

// isCommandAllowed 判断命令是否符合 verification non-interactive 白名单策略。
func isCommandAllowed(commandName string, raw string, policy config.VerificationExecutionPolicyConfig) (bool, string) {
	if hasShellMetacharacter(raw) {
		return false, "shell metacharacter is not allowed in verification command"
	}

	if _, blocked := normalizedCommandSet(policy.DeniedCommands)[commandName]; blocked {
		return false, fmt.Sprintf("command %q is denied by verification policy", commandName)
	}

	allowed := normalizedCommandSet(policy.AllowedCommands)
	if len(allowed) > 0 {
		if _, ok := allowed[commandName]; !ok {
			return false, fmt.Sprintf("command %q is not in allowed_commands", commandName)
		}
	}

	if commandName == "git" {
		sub := gitSubcommand(raw)
		if sub == "" {
			return false, "git subcommand is required"
		}
		if _, ok := readonlyGitSubcommands[sub]; !ok {
			return false, fmt.Sprintf("git subcommand %q is not read-only", sub)
		}
		if denied, reason := hasDangerousGitArguments(raw); denied {
			return false, reason
		}
	}
	return true, ""
}

// hasShellMetacharacter 判断命令文本是否包含会触发 shell 解释的高风险元字符。
func hasShellMetacharacter(command string) bool {
	return strings.ContainsAny(command, shellMetacharacters)
}

// shellCommand 按平台构建执行命令，统一以非交互 shell 运行 verifier 指令。
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "powershell", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
	}
	return exec.CommandContext(ctx, "sh", "-lc", command)
}

// commandHead 返回命令首个 token（小写）。
func commandHead(command string) string {
	fields := commandFields(command)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// gitSubcommand 提取 git 命令的二级子命令（小写）。
func gitSubcommand(command string) string {
	fields := commandFields(command)
	if len(fields) < 2 || fields[0] != "git" {
		return ""
	}
	return fields[1]
}

// hasDangerousGitArguments 校验只读 git 子命令是否携带可能产生写入副作用的参数。
func hasDangerousGitArguments(command string) (bool, string) {
	fields := commandFields(command)
	if len(fields) < 2 || fields[0] != "git" {
		return false, ""
	}
	args := fields[2:]
	for _, arg := range args {
		switch {
		case arg == "-o" || arg == "--output" || strings.HasPrefix(arg, "--output="):
			return true, "git output redirection flags are not allowed"
		case arg == "-c" || strings.HasPrefix(arg, "-c"):
			return true, "git -c config override is not allowed"
		case arg == "--config-env" || strings.HasPrefix(arg, "--config-env="):
			return true, "git --config-env is not allowed"
		case arg == "--git-dir" || strings.HasPrefix(arg, "--git-dir="):
			return true, "git --git-dir is not allowed"
		case arg == "--work-tree" || strings.HasPrefix(arg, "--work-tree="):
			return true, "git --work-tree is not allowed"
		case arg == "--ext-diff" || arg == "--external-diff":
			return true, "git external diff hooks are not allowed"
		}
	}
	return false, ""
}

// normalizedCommandSet 将命令列表规整为小写集合，便于白名单/拒绝名单判断。
func normalizedCommandSet(commands []string) map[string]struct{} {
	set := make(map[string]struct{}, len(commands))
	for _, command := range commands {
		if normalized := commandHead(command); normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return set
}

// commandFields 返回规整后的命令 token 序列。
func commandFields(command string) []string {
	fields := strings.Fields(strings.TrimSpace(command))
	for i, field := range fields {
		fields[i] = strings.ToLower(strings.TrimSpace(field))
	}
	return fields
}

// firstPositive 返回首个大于 0 的值，否则回退到最后一个默认值。
func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

type cappedBuffer struct {
	limit     int
	buffer    bytes.Buffer
	truncated bool
}

// newCappedBuffer 创建带大小上限的输出缓冲区，避免 verifier 命令输出无限膨胀。
func newCappedBuffer(limit int) *cappedBuffer {
	if limit <= 0 {
		limit = defaultVerificationOutputCap
	}
	return &cappedBuffer{limit: limit}
}

// Write 实现 io.Writer，仅保留上限范围内的输出。
func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b == nil {
		return len(p), nil
	}
	if b.buffer.Len() >= b.limit {
		b.truncated = true
		return len(p), nil
	}
	remain := b.limit - b.buffer.Len()
	if len(p) > remain {
		b.truncated = true
		_, _ = b.buffer.Write(p[:remain])
		return len(p), nil
	}
	_, _ = b.buffer.Write(p)
	return len(p), nil
}

// String 返回当前缓冲区文本。
func (b *cappedBuffer) String() string {
	if b == nil {
		return ""
	}
	return b.buffer.String()
}

// Truncated 返回输出是否发生截断。
func (b *cappedBuffer) Truncated() bool {
	if b == nil {
		return false
	}
	return b.truncated
}
