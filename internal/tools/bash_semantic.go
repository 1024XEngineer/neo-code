package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	// BashIntentClassificationUnknown 表示无法确认语义或复合命令。
	BashIntentClassificationUnknown = "unknown"
	// BashIntentClassificationReadOnly 表示只读 Git 操作。
	BashIntentClassificationReadOnly = "read_only"
	// BashIntentClassificationLocalMutation 表示本地变更型 Git 操作。
	BashIntentClassificationLocalMutation = "local_mutation"
	// BashIntentClassificationRemoteOp 表示远端交互型 Git 操作。
	BashIntentClassificationRemoteOp = "remote_op"
	// BashIntentClassificationDestructive 表示破坏性 Git 操作。
	BashIntentClassificationDestructive = "destructive"
)

// BashSemanticIntent 描述 bash 命令的语义解析结果。
type BashSemanticIntent struct {
	IsGit                 bool
	Classification        string
	Subcommand            string
	NormalizedIntent      string
	PermissionFingerprint string
	Composite             bool
	ParseError            bool
}

var gitReadOnlySubcommands = map[string]struct{}{
	"status":    {},
	"rev-parse": {},
	"describe":  {},
	"diff":      {},
	"log":       {},
	"show":      {},
}

var gitRemoteSubcommands = map[string]struct{}{
	"fetch":     {},
	"pull":      {},
	"push":      {},
	"clone":     {},
	"ls-remote": {},
}

var gitLocalMutationSubcommands = map[string]struct{}{
	"add":         {},
	"commit":      {},
	"merge":       {},
	"rebase":      {},
	"cherry-pick": {},
	"restore":     {},
	"revert":      {},
	"stash":       {},
	"checkout":    {},
	"switch":      {},
	"rm":          {},
	"mv":          {},
	"apply":       {},
	"am":          {},
	"tag":         {},
	"branch":      {},
	"reset":       {},
	"clean":       {},
	"remote":      {},
}

// AnalyzeBashCommand 对 bash 命令做轻量语义分类，供权限与会话记忆层使用。
func AnalyzeBashCommand(command string) BashSemanticIntent {
	normalized := normalizeCommand(command)
	if normalized == "" {
		return BashSemanticIntent{
			Classification:        BashIntentClassificationUnknown,
			PermissionFingerprint: buildSemanticFingerprint("bash.command.empty", ""),
		}
	}
	if containsShellControlOperators(normalized) {
		return BashSemanticIntent{
			Classification:        BashIntentClassificationUnknown,
			NormalizedIntent:      normalized,
			PermissionFingerprint: buildSemanticFingerprint("bash.command.composite", normalized),
			Composite:             true,
		}
	}

	tokens := strings.Fields(normalized)
	if len(tokens) == 0 {
		return BashSemanticIntent{
			Classification:        BashIntentClassificationUnknown,
			PermissionFingerprint: buildSemanticFingerprint("bash.command.empty", ""),
		}
	}

	if !isGitBinary(tokens[0]) {
		return BashSemanticIntent{
			Classification:        BashIntentClassificationUnknown,
			NormalizedIntent:      normalized,
			PermissionFingerprint: buildCommandFingerprint(normalized),
		}
	}

	subcommand, flags, args, ok := parseGitSubcommand(tokens)
	if !ok {
		return BashSemanticIntent{
			IsGit:                 true,
			Classification:        BashIntentClassificationUnknown,
			Subcommand:            "",
			NormalizedIntent:      "git",
			PermissionFingerprint: buildUnknownGitFingerprint(normalized),
			ParseError:            true,
		}
	}

	classification := classifyGitIntent(subcommand, flags, args)
	return BashSemanticIntent{
		IsGit:                 true,
		Classification:        classification,
		Subcommand:            subcommand,
		NormalizedIntent:      "git " + subcommand,
		PermissionFingerprint: buildGitFingerprint(classification, subcommand, normalized),
	}
}

// parseGitSubcommand 从 git 命令中提取子命令及参数，保持最小必要解析能力。
func parseGitSubcommand(tokens []string) (string, []string, []string, bool) {
	if len(tokens) < 2 {
		return "", nil, nil, false
	}

	flags := make([]string, 0, len(tokens))
	args := make([]string, 0, len(tokens))
	cursor := 1
	for ; cursor < len(tokens); cursor++ {
		token := strings.TrimSpace(tokens[cursor])
		if token == "" {
			continue
		}
		if token == "--" {
			cursor++
			break
		}
		if strings.HasPrefix(token, "-") {
			flags = append(flags, token)
			if shouldConsumeGitFlagValue(token) && cursor+1 < len(tokens) {
				cursor++
				flags = append(flags, tokens[cursor])
			}
			continue
		}
		subcommand := strings.ToLower(token)
		args = append(args, tokens[cursor+1:]...)
		return subcommand, flags, args, true
	}
	return "", flags, args, false
}

// shouldConsumeGitFlagValue 判断当前 git 全局参数是否需要消费下一个参数值。
func shouldConsumeGitFlagValue(flag string) bool {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return false
	}
	if strings.HasPrefix(flag, "--") {
		return !strings.Contains(flag, "=") && hasToken([]string{gitFlagKey(flag)}, "--config-env", "--git-dir", "--work-tree")
	}
	if strings.HasPrefix(flag, "-c") || strings.HasPrefix(flag, "-C") {
		return len(flag) == 2
	}
	return flag == "--git-dir" || flag == "--work-tree"
}

// classifyGitIntent 按子命令与关键参数做一级权限分类。
func classifyGitIntent(subcommand string, flags []string, args []string) string {
	if hasRiskyGitConfigFlag(flags) {
		return BashIntentClassificationUnknown
	}
	if _, ok := gitRemoteSubcommands[subcommand]; ok {
		return BashIntentClassificationRemoteOp
	}
	if _, ok := gitReadOnlySubcommands[subcommand]; ok {
		if hasGitReadOnlyWriteSideEffect(subcommand, args) {
			return BashIntentClassificationUnknown
		}
		return BashIntentClassificationReadOnly
	}
	switch subcommand {
	case "reset":
		if hasToken(flags, "--hard") || hasToken(args, "--hard") {
			return BashIntentClassificationDestructive
		}
		return BashIntentClassificationLocalMutation
	case "clean":
		return BashIntentClassificationDestructive
	case "checkout":
		if hasToken(args, ".") {
			return BashIntentClassificationDestructive
		}
		return BashIntentClassificationLocalMutation
	case "branch":
		if hasToken(flags, "-d", "-D", "--delete") {
			return BashIntentClassificationDestructive
		}
	case "tag":
		if hasToken(flags, "-d", "--delete") {
			return BashIntentClassificationDestructive
		}
	}
	if _, ok := gitLocalMutationSubcommands[subcommand]; ok {
		return BashIntentClassificationLocalMutation
	}
	return BashIntentClassificationUnknown
}

// hasGitReadOnlyWriteSideEffect 检查只读 Git 子命令是否携带会产生写副作用的参数。
func hasGitReadOnlyWriteSideEffect(subcommand string, args []string) bool {
	switch subcommand {
	case "diff", "log", "show":
	default:
		return false
	}

	for index := 0; index < len(args); index++ {
		token := strings.TrimSpace(args[index])
		if token == "" {
			continue
		}
		if token == "--" {
			break
		}
		key := gitFlagKey(token)
		if key != "--output" {
			continue
		}
		return true
	}
	return false
}

// hasRiskyGitConfigFlag 判断命令是否带有可能注入执行语义的高风险配置参数。
func hasRiskyGitConfigFlag(flags []string) bool {
	for _, flag := range flags {
		switch gitFlagKey(flag) {
		case "-c", "--config-env":
			return true
		}
	}
	return false
}

// gitFlagKey 将 git 参数归一化到“参数名”维度，兼容 --flag=value 与 -fVALUE 形式。
func gitFlagKey(flag string) string {
	trimmed := strings.TrimSpace(flag)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "--") {
		key := trimmed
		if idx := strings.Index(key, "="); idx >= 0 {
			key = key[:idx]
		}
		return strings.ToLower(key)
	}
	if strings.HasPrefix(trimmed, "-C") {
		return "-C"
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "-c") {
		return "-c"
	}
	return lower
}

// hasToken 判断输入集合中是否包含指定 token（大小写不敏感）。
func hasToken(tokens []string, candidates ...string) bool {
	for _, token := range tokens {
		trimmed := strings.ToLower(strings.TrimSpace(token))
		for _, candidate := range candidates {
			if trimmed == strings.ToLower(strings.TrimSpace(candidate)) {
				return true
			}
		}
	}
	return false
}

// containsShellControlOperators 检测是否存在复合 shell 控制符，存在则按 unknown 处理。
func containsShellControlOperators(command string) bool {
	if strings.Contains(command, "\n") || strings.Contains(command, "\r") {
		return true
	}
	var inSingleQuote bool
	var inDoubleQuote bool
	escaped := false
	for i := 0; i < len(command); i++ {
		ch := command[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && !inSingleQuote {
			escaped = true
			continue
		}
		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			continue
		}
		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			continue
		}
		if inSingleQuote {
			continue
		}
		if ch == '$' && i+1 < len(command) && command[i+1] == '(' {
			return true
		}
		if ch == '`' {
			return true
		}
		if !inDoubleQuote && strings.ContainsRune("&;|><", rune(ch)) {
			return true
		}
	}
	return false
}

// isGitBinary 判断首 token 是否为裸 git 可执行文件，拒绝路径伪装形式。
func isGitBinary(token string) bool {
	normalized := strings.ToLower(strings.TrimSpace(token))
	if normalized == "" {
		return false
	}
	if strings.ContainsAny(normalized, `/\:`) {
		return false
	}
	return normalized == "git" || normalized == "git.exe"
}

// normalizeCommand 将命令文本归一为稳定形式，避免仅空白差异影响语义判定。
func normalizeCommand(command string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(command)), " ")
}

// buildSemanticFingerprint 生成不会泄露原文参数的稳定指纹。
func buildSemanticFingerprint(prefix string, raw string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(raw))))
	return prefix + ":" + hex.EncodeToString(sum[:8])
}

// buildCommandFingerprint 生成通用 bash 命令权限记忆指纹，兼容历史格式。
func buildCommandFingerprint(raw string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(raw))))
	return "bash.command|sha256=" + hex.EncodeToString(sum[:8])
}

// buildUnknownGitFingerprint 生成未知 git 语义的权限记忆指纹，兼容历史格式。
func buildUnknownGitFingerprint(raw string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(raw))))
	return "bash.git.unknown|sha256=" + hex.EncodeToString(sum[:8])
}

// buildGitFingerprint 生成 git 语义权限记忆指纹，兼容历史格式并带最小语义维度。
func buildGitFingerprint(classification string, subcommand string, raw string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(raw))))
	return "bash.git|" + classification + "|" + subcommand + "|sha256=" + hex.EncodeToString(sum[:8])
}
