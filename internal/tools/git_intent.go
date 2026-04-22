package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
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

var gitSubcommandReadOnly = map[string]struct{}{
	"status":    {},
	"diff":      {},
	"log":       {},
	"show":      {},
	"rev-parse": {},
	"describe":  {},
	"blame":     {},
}

var gitSubcommandRemote = map[string]struct{}{
	"fetch":     {},
	"pull":      {},
	"push":      {},
	"clone":     {},
	"ls-remote": {},
}

var gitSubcommandLocalMutation = map[string]struct{}{
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
}

var gitFlagMayCarryValue = map[string]struct{}{
	"-c":                {},
	"-C":                {},
	"-n":                {},
	"-m":                {},
	"--max-count":       {},
	"--pretty":          {},
	"--format":          {},
	"--author":          {},
	"--since":           {},
	"--until":           {},
	"--grep":            {},
	"--work-tree":       {},
	"--git-dir":         {},
	"--depth":           {},
	"--branch":          {},
	"--strategy":        {},
	"--strategy-option": {},
	"--message":         {},
	"--file":            {},
}

// AnalyzeBashCommand 解析 bash 命令并输出稳定语义分类与权限指纹。
func AnalyzeBashCommand(command string) BashSemanticIntent {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return BashSemanticIntent{
			Classification:        BashIntentClassificationUnknown,
			PermissionFingerprint: "bash.command.empty",
		}
	}

	if containsShellControlOperators(trimmed) {
		return BashSemanticIntent{
			Classification:        BashIntentClassificationUnknown,
			NormalizedIntent:      "composite-shell-command",
			PermissionFingerprint: buildSemanticFingerprint("bash.command.composite", trimmed),
			Composite:             true,
		}
	}

	tokens, parseErr := tokenizeShellCommand(trimmed)
	if parseErr != nil || len(tokens) == 0 {
		return BashSemanticIntent{
			Classification:        BashIntentClassificationUnknown,
			NormalizedIntent:      normalizeCommandText(trimmed),
			PermissionFingerprint: buildSemanticFingerprint("bash.command.parse_error", trimmed),
			ParseError:            true,
		}
	}

	if !isGitBinary(tokens[0]) {
		normalized := normalizeCommandTokens(tokens)
		return BashSemanticIntent{
			Classification:        BashIntentClassificationUnknown,
			NormalizedIntent:      normalized,
			PermissionFingerprint: buildSemanticFingerprint("bash.command", normalized),
		}
	}
	if len(tokens) < 2 {
		return buildUnknownGitIntent(trimmed)
	}

	subcommand, flags, args, ok := parseGitCommandParts(tokens)
	if !ok {
		return buildUnknownGitIntent(trimmed)
	}
	classification := classifyGitIntent(subcommand, flags, args)
	normalizedIntent := buildNormalizedGitIntent(subcommand, flags, args)
	return BashSemanticIntent{
		IsGit:                 true,
		Classification:        classification,
		Subcommand:            subcommand,
		NormalizedIntent:      normalizedIntent,
		PermissionFingerprint: buildGitPermissionFingerprint(classification, subcommand, flags, args),
	}
}

// buildUnknownGitIntent 构建 git 命令解析失败时的统一返回结构。
func buildUnknownGitIntent(command string) BashSemanticIntent {
	return BashSemanticIntent{
		IsGit:                 true,
		Classification:        BashIntentClassificationUnknown,
		NormalizedIntent:      "git",
		PermissionFingerprint: buildSemanticFingerprint("bash.git.unknown", command),
		ParseError:            true,
	}
}

// parseGitCommandParts 解析 git 全局参数与子命令，避免将 -c 等全局参数误判为子命令。
func parseGitCommandParts(tokens []string) (string, []string, []string, bool) {
	if len(tokens) < 2 {
		return "", nil, nil, false
	}

	globalFlags := make([]string, 0, len(tokens))
	cursor := 1
	subcommand := ""
	for ; cursor < len(tokens); cursor++ {
		token := strings.TrimSpace(tokens[cursor])
		if token == "" {
			continue
		}
		if token == "--" {
			break
		}
		if strings.HasPrefix(token, "-") {
			key, value := splitGitFlagToken(token)
			if value == "" && cursor+1 < len(tokens) && shouldConsumeGitFlagValue(key, tokens[cursor+1]) {
				cursor++
				value = strings.ToLower(strings.TrimSpace(tokens[cursor]))
			}
			if value == "" {
				globalFlags = append(globalFlags, strings.ToLower(key))
			} else {
				globalFlags = append(globalFlags, strings.ToLower(key)+"="+value)
			}
			continue
		}
		subcommand = strings.ToLower(token)
		cursor++
		break
	}
	if subcommand == "" {
		return "", nil, nil, false
	}
	flags, args := normalizeGitArgs(tokens[cursor:])
	flags = append(flags, globalFlags...)
	sort.Strings(flags)
	return subcommand, flags, args, true
}

// containsShellControlOperators 检测命令中是否存在复合控制符。
func containsShellControlOperators(command string) bool {
	inSingle := false
	inDouble := false
	escaped := false
	runes := []rune(command)
	for idx := 0; idx < len(runes); idx++ {
		ch := runes[idx]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			escaped = true
			continue
		}
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		}
		if inSingle {
			continue
		}
		if ch == '`' {
			return true
		}
		if ch == '$' && idx+1 < len(runes) && runes[idx+1] == '(' {
			return true
		}
		if inDouble {
			continue
		}
		switch ch {
		case ';', '\n', '\r', '|', '>', '<':
			return true
		case '&':
			return true
		}
	}
	return false
}

// tokenizeShellCommand 以轻量规则切分命令行，保留引号内空格。
func tokenizeShellCommand(command string) ([]string, error) {
	tokens := make([]string, 0, 8)
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	for _, ch := range command {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' && inDouble {
			escaped = true
			continue
		}
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
				continue
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
				continue
			}
		}
		if unicode.IsSpace(ch) && !inSingle && !inDouble {
			if current.Len() > 0 {
				tokens = append(tokens, strings.TrimSpace(current.String()))
				current.Reset()
			}
			continue
		}
		current.WriteRune(ch)
	}
	if escaped || inSingle || inDouble {
		return nil, strconv.ErrSyntax
	}
	if current.Len() > 0 {
		tokens = append(tokens, strings.TrimSpace(current.String()))
	}
	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if strings.TrimSpace(token) == "" {
			continue
		}
		filtered = append(filtered, token)
	}
	return filtered, nil
}

// isGitBinary 判断首 token 是否为 git 可执行文件。
func isGitBinary(token string) bool {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(token)))
	base = strings.TrimSuffix(base, ".exe")
	return base == "git"
}

// normalizeGitArgs 归一化 git 参数，输出稳定 flags 与 positional 参数。
func normalizeGitArgs(tokens []string) ([]string, []string) {
	flags := make([]string, 0, len(tokens))
	args := make([]string, 0, len(tokens))
	for idx := 0; idx < len(tokens); idx++ {
		token := strings.TrimSpace(tokens[idx])
		if token == "" {
			continue
		}
		if token == "--" {
			for j := idx + 1; j < len(tokens); j++ {
				arg := strings.ToLower(strings.TrimSpace(tokens[j]))
				if arg != "" {
					args = append(args, arg)
				}
			}
			break
		}
		if strings.HasPrefix(token, "-") {
			key, value := splitGitFlagToken(token)
			if value == "" && idx+1 < len(tokens) && shouldConsumeGitFlagValue(key, tokens[idx+1]) {
				idx++
				value = strings.ToLower(strings.TrimSpace(tokens[idx]))
			}
			if value == "" {
				flags = append(flags, strings.ToLower(key))
			} else {
				flags = append(flags, strings.ToLower(key)+"="+value)
			}
			continue
		}
		args = append(args, strings.ToLower(token))
	}
	sort.Strings(flags)
	return flags, args
}

// splitGitFlagToken 将 --flag=value 形式拆分为 key/value。
func splitGitFlagToken(token string) (string, string) {
	trimmed := strings.TrimSpace(token)
	if idx := strings.Index(trimmed, "="); idx > 0 {
		key := strings.TrimSpace(trimmed[:idx])
		value := strings.ToLower(strings.TrimSpace(trimmed[idx+1:]))
		return key, value
	}
	return trimmed, ""
}

// shouldConsumeGitFlagValue 判断当前 flag 是否应消费后继值。
func shouldConsumeGitFlagValue(flag string, _ string) bool {
	flag = strings.TrimSpace(strings.ToLower(flag))
	if _, ok := gitFlagMayCarryValue[flag]; ok {
		return true
	}
	return false
}

// classifyGitIntent 根据子命令与参数推导权限分类。
func classifyGitIntent(subcommand string, flags []string, args []string) string {
	if hasRiskyGitConfigFlag(flags) {
		return BashIntentClassificationUnknown
	}
	if _, ok := gitSubcommandRemote[subcommand]; ok {
		return BashIntentClassificationRemoteOp
	}
	if _, ok := gitSubcommandReadOnly[subcommand]; ok {
		return BashIntentClassificationReadOnly
	}

	switch subcommand {
	case "branch":
		if hasGitFlag(flags, "-d", "-D", "--delete") {
			return BashIntentClassificationDestructive
		}
		if len(args) > 0 || hasGitFlag(
			flags,
			"-m",
			"-M",
			"--move",
			"-c",
			"-C",
			"--copy",
			"-f",
			"--force",
			"--set-upstream-to",
			"--unset-upstream",
		) {
			return BashIntentClassificationLocalMutation
		}
		return BashIntentClassificationReadOnly
	case "tag":
		if hasGitFlag(flags, "-d", "--delete") {
			return BashIntentClassificationDestructive
		}
		if len(args) == 0 && hasGitFlag(flags, "-l", "--list", "--contains", "--points-at") {
			return BashIntentClassificationReadOnly
		}
		return BashIntentClassificationLocalMutation
	case "reset":
		if hasGitFlag(flags, "--hard") {
			return BashIntentClassificationDestructive
		}
		return BashIntentClassificationLocalMutation
	case "clean":
		return BashIntentClassificationDestructive
	case "remote":
		return BashIntentClassificationRemoteOp
	case "checkout":
		if hasGitArgument(args, ".") {
			return BashIntentClassificationDestructive
		}
		return BashIntentClassificationLocalMutation
	}

	if _, ok := gitSubcommandLocalMutation[subcommand]; ok {
		return BashIntentClassificationLocalMutation
	}
	return BashIntentClassificationUnknown
}

// hasRiskyGitConfigFlag 判断是否包含可能改变 Git 执行语义的配置注入参数。
func hasRiskyGitConfigFlag(flags []string) bool {
	return hasGitFlag(flags, "-c", "--config-env")
}

// hasGitFlag 判断 flags 中是否命中指定 flag（支持 key=value 形式）。
func hasGitFlag(flags []string, candidates ...string) bool {
	for _, raw := range flags {
		key := raw
		if idx := strings.Index(key, "="); idx > 0 {
			key = key[:idx]
		}
		for _, candidate := range candidates {
			if strings.EqualFold(key, candidate) {
				return true
			}
		}
	}
	return false
}

// hasGitArgument 判断 positional 参数中是否存在目标值。
func hasGitArgument(args []string, candidate string) bool {
	want := strings.ToLower(strings.TrimSpace(candidate))
	for _, arg := range args {
		if strings.EqualFold(strings.TrimSpace(arg), want) {
			return true
		}
	}
	return false
}

// buildNormalizedGitIntent 构造用于展示的稳定 git 语义文本，避免暴露原始参数。
func buildNormalizedGitIntent(subcommand string, flags []string, args []string) string {
	parts := []string{"git", subcommand}
	if len(flags) > 0 {
		keys := make([]string, 0, len(flags))
		for _, flag := range flags {
			key := strings.TrimSpace(flag)
			if idx := strings.Index(key, "="); idx > 0 {
				key = key[:idx]
			}
			if key == "" {
				continue
			}
			keys = append(keys, strings.ToLower(key))
		}
		sort.Strings(keys)
		parts = append(parts, "flags:"+strings.Join(keys, ","))
	}
	if len(args) > 0 {
		parts = append(parts, "args_count:"+strconv.Itoa(len(args)))
	}
	return strings.Join(parts, " ")
}

// buildGitPermissionFingerprint 构造用于权限记忆的稳定指纹。
func buildGitPermissionFingerprint(classification string, subcommand string, flags []string, args []string) string {
	normalizedIntent := buildGitFingerprintMaterial(subcommand, flags, args)
	return buildSemanticFingerprint(
		strings.Join([]string{
			"bash.git",
			strings.ToLower(strings.TrimSpace(classification)),
			strings.ToLower(strings.TrimSpace(subcommand)),
		}, "|"),
		normalizedIntent,
	)
}

// buildGitFingerprintMaterial 构造用于哈希指纹的稳定原始语义材料，允许包含参数明文。
func buildGitFingerprintMaterial(subcommand string, flags []string, args []string) string {
	parts := []string{"git", subcommand}
	if len(flags) > 0 {
		parts = append(parts, "flags:"+strings.Join(flags, ","))
	}
	if len(args) > 0 {
		parts = append(parts, "args:"+strings.Join(args, ","))
	}
	return strings.Join(parts, " ")
}

// normalizeCommandTokens 将普通命令 token 归一为稳定文本。
func normalizeCommandTokens(tokens []string) string {
	if len(tokens) == 0 {
		return "empty"
	}
	normalized := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		normalized = append(normalized, token)
	}
	if len(normalized) == 0 {
		return "empty"
	}
	return strings.Join(normalized, " ")
}

// normalizeCommandText 将原始命令文本压缩为空白归一形式。
func normalizeCommandText(command string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(command)), " "))
}

// buildSemanticFingerprint 基于归一化语义文本生成哈希指纹，避免泄漏原始参数。
func buildSemanticFingerprint(prefix string, normalizedText string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(normalizedText)))
	return strings.TrimSpace(prefix) + "|sha256=" + hex.EncodeToString(digest[:12])
}
