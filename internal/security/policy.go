package security

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
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

	PathPatterns         []string
	PathSegmentKeywords  []string
	PathBasenamePatterns []string
	RequireSensitivePath bool

	HostPatterns       []string
	RequireHostMatch   bool
	RequireHostMissing bool
}

// PolicyEngine 基于结构化命中条件执行权限决策。
type PolicyEngine struct {
	defaultDecision Decision
	rules           []PolicyRule
}

type compiledRule struct {
	rule  PolicyRule
	order int
}

type actionView struct {
	action       Action
	resource     string
	toolCategory string
	targetType   TargetType
	target       string
	targetPath   string
	host         string
	sensitive    bool
}

// NewPolicyEngine 创建支持优先级与多条件匹配的权限引擎。
func NewPolicyEngine(defaultDecision Decision, rules []PolicyRule) (*PolicyEngine, error) {
	if defaultDecision == "" {
		defaultDecision = DecisionAllow
	}
	if err := defaultDecision.Validate(); err != nil {
		return nil, err
	}

	compiled := make([]compiledRule, 0, len(rules))
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
		compiled = append(compiled, compiledRule{
			rule:  normalizePolicyRule(rule),
			order: idx,
		})
	}

	sort.SliceStable(compiled, func(i, j int) bool {
		if compiled[i].rule.Priority == compiled[j].rule.Priority {
			return compiled[i].order < compiled[j].order
		}
		return compiled[i].rule.Priority > compiled[j].rule.Priority
	})

	sortedRules := make([]PolicyRule, 0, len(compiled))
	for _, item := range compiled {
		sortedRules = append(sortedRules, item.rule)
	}

	return &PolicyEngine{
		defaultDecision: defaultDecision,
		rules:           sortedRules,
	}, nil
}

// Check 返回首条命中规则；若无命中则返回默认决策。
func (e *PolicyEngine) Check(ctx context.Context, action Action) (CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return CheckResult{}, err
	}
	if err := action.Validate(); err != nil {
		return CheckResult{}, err
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
// bash=ask, filesystem write=ask, filesystem read敏感路径=ask/deny, webfetch白名单allow其余ask。
func NewRecommendedPolicyEngine() (*PolicyEngine, error) {
	const (
		reasonAskBash             = "bash command requires approval"
		reasonAskFilesystemWrite  = "filesystem write requires approval"
		reasonDenyPrivateKeyRead  = "reading private key material is blocked"
		reasonAskSensitiveRead    = "reading sensitive path requires approval"
		reasonAllowWebfetchDomain = "approved web domain"
		reasonAskWebfetchDomain   = "external web domain requires approval"
	)

	rules := []PolicyRule{
		{
			ID:                   "deny-sensitive-private-keys",
			Priority:             1000,
			Decision:             DecisionDeny,
			Reason:               reasonDenyPrivateKeyRead,
			ActionTypes:          []ActionType{ActionTypeRead},
			ToolCategories:       []string{"filesystem_read"},
			PathBasenamePatterns: []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "*.pem", "*.p12", "*.pfx", "*.key"},
		},
		{
			ID:                   "ask-sensitive-filesystem-read",
			Priority:             900,
			Decision:             DecisionAsk,
			Reason:               reasonAskSensitiveRead,
			ActionTypes:          []ActionType{ActionTypeRead},
			ToolCategories:       []string{"filesystem_read"},
			RequireSensitivePath: true,
			PathSegmentKeywords:  []string{"secrets", ".ssh", ".gnupg", ".aws", ".config"},
			PathBasenamePatterns: []string{".env", ".env.*", "*.env", "*.secret", "*.secrets", "*.token"},
			ResourcePatterns:     []string{"filesystem_read_*", "filesystem_grep", "filesystem_glob"},
			TargetTypes:          []TargetType{TargetTypePath, TargetTypeDirectory},
			RequireHostMissing:   false,
			RequireHostMatch:     false,
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
	}

	return NewPolicyEngine(DecisionAllow, rules)
}

func normalizePolicyRule(rule PolicyRule) PolicyRule {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Reason = strings.TrimSpace(rule.Reason)
	rule.ActionTypes = normalizeActionTypes(rule.ActionTypes)
	rule.ResourcePatterns = normalizeLowerList(rule.ResourcePatterns)
	rule.ToolCategories = normalizeLowerList(rule.ToolCategories)
	rule.TargetTypes = normalizeTargetTypes(rule.TargetTypes)
	rule.PathPatterns = normalizePathPatterns(rule.PathPatterns)
	rule.PathSegmentKeywords = normalizeLowerList(rule.PathSegmentKeywords)
	rule.PathBasenamePatterns = normalizeLowerList(rule.PathBasenamePatterns)
	rule.HostPatterns = normalizeHostPatterns(rule.HostPatterns)
	return rule
}

func normalizeActionTypes(values []ActionType) []ActionType {
	out := make([]ActionType, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(string(value)) == "" {
			continue
		}
		out = append(out, ActionType(strings.TrimSpace(string(value))))
	}
	return out
}

func normalizeTargetTypes(values []TargetType) []TargetType {
	out := make([]TargetType, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(string(value)) == "" {
			continue
		}
		out = append(out, TargetType(strings.TrimSpace(string(value))))
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

	return actionView{
		action:       action,
		resource:     resource,
		toolCategory: category,
		targetType:   action.Payload.TargetType,
		target:       target,
		targetPath:   targetPath,
		host:         host,
		sensitive:    sensitive,
	}
}

func matchesPolicyRule(rule PolicyRule, view actionView) bool {
	if len(rule.ActionTypes) > 0 {
		matched := false
		for _, actionType := range rule.ActionTypes {
			if view.action.Type == actionType {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if len(rule.ResourcePatterns) > 0 && !matchesAnyPattern(view.resource, rule.ResourcePatterns) {
		return false
	}
	if len(rule.ToolCategories) > 0 && !containsString(rule.ToolCategories, view.toolCategory) {
		return false
	}

	if len(rule.TargetTypes) > 0 {
		matched := false
		for _, targetType := range rule.TargetTypes {
			if view.targetType == targetType {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if rule.RequireSensitivePath && !view.sensitive {
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
	}
	if resource != "" {
		return resource
	}
	return strings.ToLower(strings.TrimSpace(action.Payload.ToolName))
}

func classifySensitivePath(normalizedTargetPath string) bool {
	if normalizedTargetPath == "" {
		return false
	}
	return matchesPathSegmentKeyword(normalizedTargetPath, []string{"secrets", ".ssh", ".gnupg", ".aws", ".config"}) ||
		matchesPathBasenamePattern(normalizedTargetPath, []string{".env", ".env.*", "*.env", "*.secret", "*.secrets", "*.token", "*.key", "*.pem", "id_rsa", "id_ed25519"})
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

func containsString(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}
