package security

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// PolicyRule 描述一条可组合的权限策略规则。
// 规则按 Priority 从高到低匹配，同优先级保持声明顺序。
type PolicyRule struct {
	ID       string
	Priority int
	Decision Decision
	Reason   string

	ActionTypes      []ActionType
	ResourcePatterns []string
	ToolCategories   []string
	TargetTypes      []TargetType

	PathPatterns          []string
	PathSegmentKeywords   []string
	PathBasenamePatterns  []string
	RequireSensitivePath  bool
	RequirePrivateKeyPath bool

	HostPatterns       []string
	RequireHostMatch   bool
	RequireHostMissing bool
}

// PolicyEngine 基于结构化命中条件执行权限决策。
type PolicyEngine struct {
	defaultDecision Decision
	rules           []PolicyRule
}

type actionView struct {
	action              Action
	resource            string
	toolCategory        string
	targetType          TargetType
	target              string
	targetPath          string
	host                string
	sensitive           bool
	privateKeySensitive bool
}

var (
	sensitivePathSegmentKeywords  = []string{"secrets", ".ssh", ".gnupg", ".aws", ".config"}
	sensitivePathBasenamePatterns = []string{
		".env", ".env.*", "*.env", "*.secret", "*.secrets", "*.token", "*.key", "*.pem", "id_rsa", "id_ed25519",
	}
	privateKeyBasenamePatterns = []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "*.pem", "*.p12", "*.pfx", "*.key"}
)

// NewPolicyEngine 创建支持优先级与多条件匹配的权限引擎。
func NewPolicyEngine(defaultDecision Decision, rules []PolicyRule) (*PolicyEngine, error) {
	if defaultDecision == "" {
		defaultDecision = DecisionAllow
	}
	if err := defaultDecision.Validate(); err != nil {
		return nil, err
	}

	sortedRules := make([]PolicyRule, 0, len(rules))
	for idx := range rules {
		rule := rules[idx]
		if strings.TrimSpace(rule.ID) == "" {
			return nil, fmt.Errorf("security: policy rule id is empty at index %d", idx)
		}
		if err := rule.Decision.Validate(); err != nil {
			return nil, fmt.Errorf("security: policy rule %q: %w", rule.ID, err)
		}
		for _, actionType := range rule.ActionTypes {
			if actionType == "" {
				continue
			}
			if err := actionType.Validate(); err != nil {
				return nil, fmt.Errorf("security: policy rule %q: %w", rule.ID, err)
			}
		}
		sortedRules = append(sortedRules, normalizePolicyRule(rule))
	}

	// 稳定排序保证同优先级规则维持声明顺序。
	sortPolicyRulesByPriority(sortedRules)

	return &PolicyEngine{
		defaultDecision: defaultDecision,
		rules:           sortedRules,
	}, nil
}

// sortPolicyRulesByPriority 按优先级从高到低稳定排序策略规则。
func sortPolicyRulesByPriority(rules []PolicyRule) {
	slices.SortStableFunc(rules, func(a, b PolicyRule) int {
		switch {
		case a.Priority > b.Priority:
			return -1
		case a.Priority < b.Priority:
			return 1
		default:
			return 0
		}
	})
}

// Check 返回首条命中规则；若无命中则返回默认决策。
func (e *PolicyEngine) Check(ctx context.Context, action Action) (CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return CheckResult{}, err
	}
	if err := action.Validate(); err != nil {
		return CheckResult{}, err
	}
	if capResult, denied := EvaluateCapabilityForEngine(action, time.Now().UTC()); denied {
		return capResult, nil
	}

	view := newActionView(action)
	for _, rule := range e.rules {
		if !matchesPolicyRule(rule, view) {
			continue
		}
		matchedRule := Rule{
			ID:       rule.ID,
			Type:     action.Type,
			Resource: action.Payload.Resource,
			Decision: rule.Decision,
			Reason:   rule.Reason,
		}
		return CheckResult{
			Decision: rule.Decision,
			Action:   action,
			Rule:     &matchedRule,
			Reason:   strings.TrimSpace(rule.Reason),
		}, nil
	}

	return CheckResult{
		Decision: e.defaultDecision,
		Action:   action,
	}, nil
}

// NewRecommendedPolicyEngine 返回推荐安全策略：
// git 只读语义默认放行（受语义解析和安全拦截约束），其余 git 语义需审批，filesystem write=ask，
// filesystem read 敏感路径=ask/deny，webfetch 白名单 allow 其余 ask。
func NewRecommendedPolicyEngine() (*PolicyEngine, error) {
	const (
		reasonAllowGitReadOnly    = "git read-only operation allowed by policy"
		reasonDenyGitPrivateRead  = "git read-only access to private key material is blocked"
		reasonAskGitSensitiveRead = "git read-only access to sensitive path requires approval"
		reasonAskGitRemote        = "git remote operation requires approval"
		reasonAskGitDestructive   = "git destructive operation requires approval"
		reasonAskGitMutation      = "git local mutation requires approval"
		reasonAskGitUnknown       = "git command semantic unknown, requires approval"
		reasonAskBash             = "bash command requires approval"
		reasonAskFilesystemWrite  = "filesystem write requires approval"
		reasonDenyPrivateKeyRead  = "reading private key material is blocked"
		reasonAskSensitiveRead    = "reading sensitive path requires approval"
		reasonAllowWebfetchDomain = "approved web domain"
		reasonAskWebfetchDomain   = "external web domain requires approval"
		reasonAskMCP              = "mcp tool requires approval"
	)

	rules := []PolicyRule{
		{
			ID:                    "deny-sensitive-private-keys",
			Priority:              1000,
			Decision:              DecisionDeny,
			Reason:                reasonDenyPrivateKeyRead,
			ActionTypes:           []ActionType{ActionTypeRead},
			ToolCategories:        []string{"filesystem_read"},
			RequirePrivateKeyPath: true,
		},
		{
			ID:                   "ask-sensitive-filesystem-read",
			Priority:             900,
			Decision:             DecisionAsk,
			Reason:               reasonAskSensitiveRead,
			ActionTypes:          []ActionType{ActionTypeRead},
			ToolCategories:       []string{"filesystem_read"},
			RequireSensitivePath: true,
			PathSegmentKeywords:  sensitivePathSegmentKeywords,
			PathBasenamePatterns: []string{".env", ".env.*", "*.env", "*.secret", "*.secrets", "*.token"},
			ResourcePatterns:     []string{"filesystem_read_*", "filesystem_grep", "filesystem_glob"},
			TargetTypes:          []TargetType{TargetTypePath, TargetTypeDirectory},
			RequireHostMissing:   false,
			RequireHostMatch:     false,
		},
		{
			ID:                    "deny-bash-git-read-only-private-keys",
			Priority:              875,
			Decision:              DecisionDeny,
			Reason:                reasonDenyGitPrivateRead,
			ActionTypes:           []ActionType{ActionTypeBash},
			ResourcePatterns:      []string{"bash_git_read_only"},
			RequirePrivateKeyPath: true,
		},
		{
			ID:                   "ask-bash-git-read-only-sensitive",
			Priority:             870,
			Decision:             DecisionAsk,
			Reason:               reasonAskGitSensitiveRead,
			ActionTypes:          []ActionType{ActionTypeBash},
			ResourcePatterns:     []string{"bash_git_read_only"},
			RequireSensitivePath: true,
		},
		{
			ID:               "allow-bash-git-read-only",
			Priority:         860,
			Decision:         DecisionAllow,
			Reason:           reasonAllowGitReadOnly,
			ActionTypes:      []ActionType{ActionTypeBash},
			ResourcePatterns: []string{"bash_git_read_only"},
		},
		{
			ID:               "ask-bash-git-remote-op",
			Priority:         855,
			Decision:         DecisionAsk,
			Reason:           reasonAskGitRemote,
			ActionTypes:      []ActionType{ActionTypeBash},
			ResourcePatterns: []string{"bash_git_remote_op"},
		},
		{
			ID:               "ask-bash-git-destructive",
			Priority:         850,
			Decision:         DecisionAsk,
			Reason:           reasonAskGitDestructive,
			ActionTypes:      []ActionType{ActionTypeBash},
			ResourcePatterns: []string{"bash_git_destructive"},
		},
		{
			ID:               "ask-bash-git-local-mutation",
			Priority:         845,
			Decision:         DecisionAsk,
			Reason:           reasonAskGitMutation,
			ActionTypes:      []ActionType{ActionTypeBash},
			ResourcePatterns: []string{"bash_git_local_mutation"},
		},
		{
			ID:               "ask-bash-git-unknown",
			Priority:         840,
			Decision:         DecisionAsk,
			Reason:           reasonAskGitUnknown,
			ActionTypes:      []ActionType{ActionTypeBash},
			ResourcePatterns: []string{"bash_git_unknown"},
		},
		{
			ID:               "ask-all-bash",
			Priority:         800,
			Decision:         DecisionAsk,
			Reason:           reasonAskBash,
			ActionTypes:      []ActionType{ActionTypeBash},
			ResourcePatterns: []string{"bash"},
		},
		{
			ID:               "ask-filesystem-write",
			Priority:         780,
			Decision:         DecisionAsk,
			Reason:           reasonAskFilesystemWrite,
			ActionTypes:      []ActionType{ActionTypeWrite},
			ResourcePatterns: []string{"filesystem_write_*", "filesystem_edit"},
		},
		{
			ID:               "allow-webfetch-whitelist",
			Priority:         760,
			Decision:         DecisionAllow,
			Reason:           reasonAllowWebfetchDomain,
			ActionTypes:      []ActionType{ActionTypeRead},
			ResourcePatterns: []string{"webfetch"},
			HostPatterns:     []string{"github.com", "*.github.com"},
			RequireHostMatch: true,
		},
		{
			ID:                 "ask-webfetch-non-whitelist",
			Priority:           740,
			Decision:           DecisionAsk,
			Reason:             reasonAskWebfetchDomain,
			ActionTypes:        []ActionType{ActionTypeRead},
			ResourcePatterns:   []string{"webfetch"},
			HostPatterns:       []string{"github.com", "*.github.com"},
			RequireHostMissing: true,
		},
		{
			ID:          "ask-all-mcp",
			Priority:    720,
			Decision:    DecisionAsk,
			Reason:      reasonAskMCP,
			ActionTypes: []ActionType{ActionTypeMCP},
			TargetTypes: []TargetType{TargetTypeMCP},
		},
	}

	return NewPolicyEngine(DecisionAllow, rules)
}

func normalizePolicyRule(rule PolicyRule) PolicyRule {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Reason = strings.TrimSpace(rule.Reason)
	rule.ActionTypes = normalizeTypedList(rule.ActionTypes)
	rule.ResourcePatterns = normalizeLowerList(rule.ResourcePatterns)
	rule.ToolCategories = normalizeLowerList(rule.ToolCategories)
	rule.TargetTypes = normalizeTypedList(rule.TargetTypes)
	rule.PathPatterns = normalizePathPatterns(rule.PathPatterns)
	rule.PathSegmentKeywords = normalizeLowerList(rule.PathSegmentKeywords)
	rule.PathBasenamePatterns = normalizeLowerList(rule.PathBasenamePatterns)
	rule.HostPatterns = normalizeHostPatterns(rule.HostPatterns)
	return rule
}

// normalizeTypedList 统一裁剪基于字符串别名的枚举列表，避免重复保留空白值。
func normalizeTypedList[T ~string](values []T) []T {
	out := make([]T, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(string(value)) == "" {
			continue
		}
		out = append(out, T(strings.TrimSpace(string(value))))
	}
	return out
}

func normalizeLowerList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func normalizePathPatterns(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := filepath.ToSlash(strings.ToLower(strings.TrimSpace(value)))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func normalizeHostPatterns(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		trimmed = strings.TrimPrefix(trimmed, ".")
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func newActionView(action Action) actionView {
	resource := strings.ToLower(strings.TrimSpace(action.Payload.Resource))
	target := strings.TrimSpace(action.Payload.Target)
	host := ""
	if action.Payload.TargetType == TargetTypeURL || resource == "webfetch" {
		host = parseURLHost(target)
	}
	targetPath := filepath.ToSlash(strings.ToLower(strings.TrimSpace(target)))
	category := deriveToolCategory(action)
	sensitive := classifySensitivePath(targetPath)
	privateKeySensitive := classifyPrivateKeyPath(targetPath)
	if !sensitive && resource == "bash_git_read_only" {
		sensitive = classifySensitiveGitReadOnlyCommand(target)
	}
	if !privateKeySensitive && resource == "bash_git_read_only" {
		privateKeySensitive = classifyPrivateKeyGitReadOnlyCommand(target)
	}

	return actionView{
		action:              action,
		resource:            resource,
		toolCategory:        category,
		targetType:          action.Payload.TargetType,
		target:              target,
		targetPath:          targetPath,
		host:                host,
		sensitive:           sensitive,
		privateKeySensitive: privateKeySensitive,
	}
}

func matchesPolicyRule(rule PolicyRule, view actionView) bool {
	if len(rule.ActionTypes) > 0 && !containsValue(rule.ActionTypes, view.action.Type) {
		return false
	}

	if len(rule.ResourcePatterns) > 0 && !matchesAnyPattern(view.resource, rule.ResourcePatterns) {
		return false
	}
	if len(rule.ToolCategories) > 0 && !containsString(rule.ToolCategories, view.toolCategory) {
		return false
	}

	if len(rule.TargetTypes) > 0 && !containsValue(rule.TargetTypes, view.targetType) {
		return false
	}

	if rule.RequireSensitivePath && !view.sensitive {
		return false
	}
	if rule.RequirePrivateKeyPath && !view.privateKeySensitive {
		return false
	}
	pathMatcherCount := len(rule.PathPatterns) + len(rule.PathSegmentKeywords) + len(rule.PathBasenamePatterns)
	if pathMatcherCount > 0 {
		pathMatched := false
		if len(rule.PathPatterns) > 0 && matchesAnyPattern(view.targetPath, rule.PathPatterns) {
			pathMatched = true
		}
		if len(rule.PathSegmentKeywords) > 0 && matchesPathSegmentKeyword(view.targetPath, rule.PathSegmentKeywords) {
			pathMatched = true
		}
		if len(rule.PathBasenamePatterns) > 0 && matchesPathBasenamePattern(view.targetPath, rule.PathBasenamePatterns) {
			pathMatched = true
		}
		if !pathMatched {
			return false
		}
	}

	hostMatched := len(rule.HostPatterns) == 0 || matchesAnyPattern(view.host, rule.HostPatterns)
	if rule.RequireHostMatch && !hostMatched {
		return false
	}
	if rule.RequireHostMissing && hostMatched {
		return false
	}

	return true
}

func deriveToolCategory(action Action) string {
	resource := strings.ToLower(strings.TrimSpace(action.Payload.Resource))
	switch action.Type {
	case ActionTypeRead:
		if strings.HasPrefix(resource, "filesystem_") {
			return "filesystem_read"
		}
	case ActionTypeWrite:
		if strings.HasPrefix(resource, "filesystem_") {
			return "filesystem_write"
		}
	case ActionTypeBash:
		return "bash"
	case ActionTypeMCP:
		if serverIdentity := mcpServerIdentity(action); serverIdentity != "" {
			return serverIdentity
		}
		return "mcp"
	}
	if resource != "" {
		return resource
	}
	return strings.ToLower(strings.TrimSpace(action.Payload.ToolName))
}

// newMCPServerPolicyRule 生成 MCP server 级规则模板；优先级按 deny > ask > allow 固定。
func newMCPServerPolicyRule(id string, decision Decision, serverID string, reason string) PolicyRule {
	serverIdentity := canonicalMCPServerIdentity(serverID)
	return PolicyRule{
		ID:             strings.TrimSpace(id),
		Priority:       mcpPolicyPriority(decision),
		Decision:       decision,
		Reason:         strings.TrimSpace(reason),
		ActionTypes:    []ActionType{ActionTypeMCP},
		ToolCategories: []string{serverIdentity},
		TargetTypes:    []TargetType{TargetTypeMCP},
	}
}

// newMCPToolPolicyRule 生成 MCP tool 级规则模板；target/resource 均命中 mcp.<server>.<tool> identity。
func newMCPToolPolicyRule(id string, decision Decision, serverID string, toolName string, reason string) PolicyRule {
	toolIdentity := canonicalMCPToolIdentity(serverID, toolName)
	serverIdentity := canonicalMCPServerIdentity(serverID)
	return PolicyRule{
		ID:               strings.TrimSpace(id),
		Priority:         mcpPolicyPriority(decision),
		Decision:         decision,
		Reason:           strings.TrimSpace(reason),
		ActionTypes:      []ActionType{ActionTypeMCP},
		ResourcePatterns: []string{toolIdentity},
		ToolCategories:   []string{serverIdentity},
		TargetTypes:      []TargetType{TargetTypeMCP},
	}
}

// mcpPolicyPriority 返回 MCP 权限规则的固定优先级，确保 deny > ask > allow。
func mcpPolicyPriority(decision Decision) int {
	switch decision {
	case DecisionDeny:
		return 830
	case DecisionAsk:
		return 820
	case DecisionAllow:
		return 810
	default:
		return 0
	}
}

// mcpServerIdentity 从 action 中提取 MCP server identity：mcp.<server>。
func mcpServerIdentity(action Action) string {
	if action.Type != ActionTypeMCP {
		return ""
	}
	candidates := []string{
		strings.TrimSpace(action.Payload.Target),
		strings.TrimSpace(action.Payload.Resource),
		strings.TrimSpace(action.Payload.ToolName),
	}
	for _, candidate := range candidates {
		if identity := canonicalMCPServerIdentity(candidate); identity != "" {
			return identity
		}
	}
	return ""
}

// CanonicalMCPServerIdentity 将输入标识归一为 MCP server 级 identity（mcp.<server>）。
func CanonicalMCPServerIdentity(raw string) string {
	return canonicalMCPServerIdentity(raw)
}

// canonicalMCPServerIdentity 将 server 标识归一为 mcp.<server> 形式。
//
// 命名约定 (naming contract)：
//   - 以 "mcp." 开头的输入被视为完整的 tool identity（mcp.<server>.<tool>）；
//     函数将从中提取 server 部分并返回 mcp.<server>。
//   - 不带 "mcp." 前缀的输入被视为纯 server 名称，函数直接补全为 mcp.<server>。
//   - 调用方传入纯 server 名称时 **不应** 携带 "mcp." 前缀；
//     如需从 tool identity 提取 server，传入完整 mcp.<server>.<tool> 即可。
func canonicalMCPServerIdentity(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" || trimmed == "mcp" || trimmed == "mcp." {
		return ""
	}
	if strings.HasPrefix(trimmed, "mcp.") {
		body := strings.TrimPrefix(trimmed, "mcp.")
		// MCP 工具 identity 采用 mcp.<server>.<tool>，server 允许包含 "."；
		// 因此按最后一个 "." 分隔 server 与 tool，避免将 server 错误截断到第二段。
		lastDot := strings.LastIndex(body, ".")
		if lastDot == -1 {
			return "mcp." + body
		}
		if lastDot == 0 || lastDot == len(body)-1 {
			return ""
		}
		return "mcp." + body[:lastDot]
	}
	return "mcp." + trimmed
}

// canonicalMCPToolIdentity 将 server/tool 标识归一为 mcp.<server>.<tool>。
//
// tool 名称不得包含 "."；含 "." 的 toolName 会返回空串并被视为非法输入。
// 这可防止 server/tool 边界解析歧义，例如：
//
//	server="github", toolName="enterprise.create_issue"
//	→ 若允许，拼接后 identity 为 mcp.github.enterprise.create_issue
//	→ canonicalMCPServerIdentity 将错误地提取 server 为 mcp.github.enterprise
//	  而非正确的 mcp.github，导致权限绕过。
func canonicalMCPToolIdentity(serverID string, toolName string) string {
	serverIdentity := canonicalMCPServerIdentity(serverID)
	tool := strings.ToLower(strings.TrimSpace(toolName))
	if serverIdentity == "" || tool == "" {
		return ""
	}
	// Reject tool names containing "." to prevent ambiguous server/tool boundary parsing.
	if strings.Contains(tool, ".") {
		return ""
	}
	return serverIdentity + "." + tool
}

func classifySensitivePath(normalizedTargetPath string) bool {
	if normalizedTargetPath == "" {
		return false
	}
	return matchesPathSegmentKeyword(normalizedTargetPath, sensitivePathSegmentKeywords) ||
		matchesPathBasenamePattern(normalizedTargetPath, sensitivePathBasenamePatterns)
}

// classifyPrivateKeyPath 判断路径是否命中私钥材料命名模式。
func classifyPrivateKeyPath(normalizedTargetPath string) bool {
	if normalizedTargetPath == "" {
		return false
	}
	return matchesPathBasenamePattern(normalizedTargetPath, privateKeyBasenamePatterns)
}

// classifySensitiveGitReadOnlyCommand 从 git 只读命令中提取潜在路径片段，并复用敏感路径判定。
func classifySensitiveGitReadOnlyCommand(command string) bool {
	return classifyGitReadOnlyCommandPath(command, classifySensitivePath)
}

// classifyPrivateKeyGitReadOnlyCommand 从 git 只读命令中提取潜在路径并识别私钥材料。
func classifyPrivateKeyGitReadOnlyCommand(command string) bool {
	return classifyGitReadOnlyCommandPath(command, classifyPrivateKeyPath)
}

// classifyGitReadOnlyCommandPath 复用 git 只读命令路径提取逻辑，对候选路径执行具体分类判定。
func classifyGitReadOnlyCommandPath(command string, classify func(string) bool) bool {
	for _, candidate := range extractGitReadOnlyPathCandidates(command) {
		if classify(candidate) {
			return true
		}
	}
	return false
}

// extractGitReadOnlyPathCandidates 解析 git 只读命令中的潜在路径片段，支持 HEAD:path 与 pathspec 魔法语法。
func extractGitReadOnlyPathCandidates(command string) []string {
	normalized := strings.ToLower(strings.TrimSpace(command))
	if normalized == "" {
		return nil
	}
	candidates := make([]string, 0, 8)
	appendCandidate := func(raw string) {
		cleaned := filepath.ToSlash(strings.TrimSpace(raw))
		if cleaned == "" {
			return
		}
		candidates = append(candidates, cleaned)
	}

	appendCandidate(normalized)
	for _, token := range strings.Fields(normalized) {
		cleaned := normalizeGitPathToken(token)
		if cleaned == "" {
			continue
		}
		appendCandidate(cleaned)
		if idx := strings.LastIndex(cleaned, ":"); idx >= 0 && idx+1 < len(cleaned) {
			appendCandidate(cleaned[idx+1:])
		}
	}
	return candidates
}

// normalizeGitPathToken 清洗 git 参数 token，尽量提取稳定路径片段并去掉 pathspec 语法前缀。
func normalizeGitPathToken(token string) string {
	cleaned := strings.Trim(strings.TrimSpace(token), `"'()[]{}<>,;`)
	if cleaned == "" {
		return ""
	}
	if strings.HasPrefix(cleaned, ":(") {
		if end := strings.Index(cleaned, ")"); end >= 0 && end+1 < len(cleaned) {
			cleaned = cleaned[end+1:]
		}
	}
	cleaned = strings.TrimPrefix(cleaned, "./")
	cleaned = strings.TrimPrefix(cleaned, ".\\")
	return strings.TrimSpace(cleaned)
}

func matchesPathSegmentKeyword(normalizedTargetPath string, keywords []string) bool {
	if normalizedTargetPath == "" || len(keywords) == 0 {
		return false
	}
	segments := strings.Split(normalizedTargetPath, "/")
	for _, segment := range segments {
		token := strings.ToLower(strings.TrimSpace(segment))
		if token == "" {
			continue
		}
		for _, keyword := range keywords {
			if token == keyword || strings.Contains(token, keyword) {
				return true
			}
		}
	}
	return false
}

func matchesPathBasenamePattern(normalizedTargetPath string, patterns []string) bool {
	if normalizedTargetPath == "" || len(patterns) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(normalizedTargetPath))
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, base)
		if err != nil {
			continue
		}
		if matched {
			return true
		}
	}
	return false
}

func matchesAnyPattern(value string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, pattern := range patterns {
		p := strings.ToLower(strings.TrimSpace(pattern))
		if p == "" {
			continue
		}
		matched, err := filepath.Match(p, normalized)
		if err == nil && matched {
			return true
		}
		if p == normalized {
			return true
		}
		if strings.HasPrefix(p, "*.") && strings.HasSuffix(normalized, p[1:]) {
			return true
		}
	}
	return false
}

func parseURLHost(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed == nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return strings.TrimPrefix(host, ".")
}

// containsValue 判断枚举切片中是否包含给定值，供规则匹配复用。
func containsValue[T comparable](values []T, target T) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}
