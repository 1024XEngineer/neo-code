package service

import (
	"regexp"
	"strings"

	"go-llm-demo/internal/server/domain"

	"github.com/bmatcuk/doublestar/v4"
)

type SecurityService interface {
	Check(toolType string, target string) domain.Action
}

// securityServiceImpl 是 SecurityService 的具体实现
type securityServiceImpl struct {
	configRepo domain.SecurityConfigRepository
	blackList  *domain.Config
	whiteList  *domain.Config
	yellowList *domain.Config
}

func NewSecurityService(configRepo domain.SecurityConfigRepository) SecurityService {
	return &securityServiceImpl{
		configRepo: configRepo,
	}
}

func (s *securityServiceImpl) Initialize(configDir string) error {
	blackList, whiteList, yellowList, err := s.configRepo.LoadAll(configDir)
	if err != nil {
		return err
	}
	s.blackList = blackList
	s.whiteList = whiteList
	s.yellowList = yellowList
	return nil
}

func (s *securityServiceImpl) Check(toolType string, target string) domain.Action {
	// 业务规则 1：黑名单优先级最高（直接拒绝）
	if s.matchesList(s.blackList, toolType, target) {
		return domain.ActionDeny
	}

	// 业务规则 2：白名单次之（直接允许）
	if s.matchesList(s.whiteList, toolType, target) {
		return domain.ActionAllow
	}

	// 业务规则 3：黄名单再次之（询问用户）
	if s.matchesList(s.yellowList, toolType, target) {
		return domain.ActionAsk
	}

	// 兜底策略：默认询问用户
	return domain.ActionAsk
}

func (s *securityServiceImpl) matchesList(config *domain.Config, toolType, target string) bool {
	if config == nil {
		return false
	}

	for _, rule := range config.Rules {
		if ruleMatches(rule, toolType, target) {
			return true
		}
	}
	return false
}

func ruleMatches(rule domain.Rule, toolType string, target string) bool {
	var pattern string

	switch toolType {
	case "Read", "Write":
		pattern = rule.Target
	case "Bash":
		pattern = rule.Command
	case "WebFetch":
		pattern = rule.Domain
	default:
		return false
	}

	if pattern == "" {
		return false
	}

	if toolType == "Bash" {
		return matchCommand(pattern, target)
	}

	matched, err := doublestar.Match(pattern, target)
	if err != nil {
		return false
	}

	return matched
}

func matchCommand(pattern, command string) bool {
	rePattern := regexp.QuoteMeta(pattern)
	rePattern = strings.ReplaceAll(rePattern, `\*\*`, `.*`)
	rePattern = strings.ReplaceAll(rePattern, `\*`, `.*`)
	re, err := regexp.Compile("^" + rePattern + "$")
	if err != nil {
		return false
	}
	return re.MatchString(command)
}
