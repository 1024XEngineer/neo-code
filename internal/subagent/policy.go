package subagent

import (
	"strings"
	"time"
)

const (
	defaultPolicyMaxSteps = 6
	defaultPolicyTimeout  = 30
)

// RolePolicy 定义不同角色的执行策略。
type RolePolicy struct {
	Role             Role
	SystemPrompt     string
	AllowedTools     []string
	DefaultBudget    Budget
	RequiredSections []string
}

// Validate 校验角色策略是否合法。
func (p RolePolicy) Validate() error {
	if !p.Role.Valid() {
		return errorsf("invalid policy role %q", p.Role)
	}
	if strings.TrimSpace(p.SystemPrompt) == "" {
		return errorsf("role policy prompt is required")
	}
	if len(dedupeAndTrim(p.AllowedTools)) == 0 {
		return errorsf("role policy allowed tools is empty")
	}
	if len(dedupeAndTrim(p.RequiredSections)) == 0 {
		return errorsf("role policy required sections is empty")
	}
	if _, err := normalizeRequiredSections(p.RequiredSections); err != nil {
		return err
	}
	return nil
}

// DefaultRolePolicy 返回内置角色策略。
func DefaultRolePolicy(role Role) (RolePolicy, error) {
	if !role.Valid() {
		return RolePolicy{}, errorsf("unsupported role %q", role)
	}

	policy := RolePolicy{
		Role: role,
		DefaultBudget: Budget{
			MaxSteps: defaultPolicyMaxSteps,
			Timeout:  defaultPolicyTimeout * time.Second,
		},
		RequiredSections: []string{
			"summary",
			"findings",
			"patches",
			"risks",
			"next_actions",
			"artifacts",
		},
	}

	switch role {
	case RoleResearcher:
		policy.SystemPrompt = "你是研究型子代理，负责检索证据并形成结论。"
		policy.AllowedTools = []string{"filesystem_read_file", "filesystem_glob", "filesystem_grep", "webfetch"}
	case RoleCoder:
		policy.SystemPrompt = "你是实现型子代理，负责修改代码并给出验证结果。"
		policy.AllowedTools = []string{
			"filesystem_read_file",
			"filesystem_write_file",
			"filesystem_edit",
			"filesystem_glob",
			"filesystem_grep",
			"bash",
		}
	case RoleReviewer:
		policy.SystemPrompt = "你是审查型子代理，负责识别缺陷、风险与测试缺口。"
		policy.AllowedTools = []string{"filesystem_read_file", "filesystem_glob", "filesystem_grep"}
	}

	policy.AllowedTools = dedupeAndTrim(policy.AllowedTools)
	policy.RequiredSections = dedupeAndTrim(policy.RequiredSections)
	return policy, nil
}
