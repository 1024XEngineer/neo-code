package security

import (
	"context"
	"strings"
)

// PermissionEngine evaluates actions deterministically.
type PermissionEngine interface {
	Check(ctx context.Context, action Action) (CheckResult, error)
}

// StaticGateway evaluates actions against an in-memory ordered rule list.
type StaticGateway struct {
	defaultDecision Decision
	rules           []Rule
}

// NewStaticGateway creates a permission engine with a default decision and ordered rules.
func NewStaticGateway(defaultDecision Decision, rules []Rule) (*StaticGateway, error) {
	if defaultDecision == "" {
		defaultDecision = DecisionAllow
	}
	if err := defaultDecision.Validate(); err != nil {
		return nil, err
	}

	cloned := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if err := rule.Validate(); err != nil {
			return nil, err
		}
		cloned = append(cloned, rule)
	}

	return &StaticGateway{
		defaultDecision: defaultDecision,
		rules:           cloned,
	}, nil
}

// Check returns the first matching rule result, or the default decision.
func (g *StaticGateway) Check(ctx context.Context, action Action) (CheckResult, error) {
	if err := ctx.Err(); err != nil {
		return CheckResult{}, err
	}
	if err := action.Validate(); err != nil {
		return CheckResult{}, err
	}

	for idx := range g.rules {
		rule := g.rules[idx]
		if !matchesRule(rule, action) {
			continue
		}
		return CheckResult{
			Decision: rule.Decision,
			Action:   action,
			Rule:     &g.rules[idx],
			Reason:   strings.TrimSpace(rule.Reason),
		}, nil
	}

	return CheckResult{
		Decision: g.defaultDecision,
		Action:   action,
	}, nil
}

func matchesRule(rule Rule, action Action) bool {
	if rule.Type != "" && rule.Type != action.Type {
		return false
	}
	if !matchesResource(rule.Resource, action.Payload.Resource) {
		return false
	}

	prefix := strings.TrimSpace(rule.TargetPrefix)
	if prefix == "" {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(action.Payload.Target), prefix)
}

func matchesResource(ruleResource string, actionResource string) bool {
	trimmed := strings.TrimSpace(ruleResource)
	if trimmed == "" || trimmed == "*" {
		return true
	}
	return strings.EqualFold(trimmed, strings.TrimSpace(actionResource))
}
