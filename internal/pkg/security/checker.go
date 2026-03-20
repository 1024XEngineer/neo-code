package security

import (
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// 1. 定义三种决策状态的枚举 (Go 语言中用自定义类型 + const 常量来模拟枚举)
type Action string

const (
	ActionDeny  Action = "deny"  // 黑名单：拒绝
	ActionAllow Action = "allow" // 白名单：放行
	ActionAsk   Action = "ask"   // 黄名单：询问用户
)

// Check 是整个安全模块对外暴露的唯一 API 入口
// 大模型想要干什么，底层的 Agent 必须先调用这个函数来拿批文
func (sm *SecurityManager) Check(toolType string, target string) Action {
	// 漏斗设计原则：优先级从高到低

	// 1. 最高优先级：检查黑名单
	// 只要黑名单里明确写了 deny，天王老子来了也得拦住
	if sm.matchInConfig(&sm.BlackList, toolType, target) == "deny" {
		return ActionDeny
	}

	// 2. 第二优先级：检查白名单
	if sm.matchInConfig(&sm.WhiteList, toolType, target) == "allow" {
		return ActionAllow
	}

	// 3. 第三优先级：检查黄名单
	if sm.matchInConfig(&sm.YellowList, toolType, target) == "ask" {
		return ActionAsk
	}

	// 4. 兜底策略 (Fallback)
	// 如果 AI 提出了一个在三个名单里都没写过的请求，默认走最安全的黄名单（弹窗问用户）
	return ActionAsk
}

// matchInConfig 是一个内部私有函数 (首字母小写)，用来遍历某一个名单里的所有规则
func (sm *SecurityManager) matchInConfig(config *Config, toolType string, target string) string {
	// 这就是 Go 语言的范围遍历，等价于 C++11 的 for (auto rule : config.Rules)
	// '_' 表示我们不需要索引(index)，只需要 rule 的值
	for _, rule := range config.Rules {
		if isMatch(rule, toolType, target) {
			// 修复点：提取权限后，判断一下是不是空字符串
			perm := extractPermission(rule, toolType)
			if perm != "" {
				// 只有当这条规则真的配置了该权限（比如明确写了 deny 或 allow），才返回
				return perm
			}
			// 如果 perm 是 ""，说明这条规则管的是别的动作（比如管了 W 没管 R），
			// 此时不能 return，要让 for 循环继续往下找！
		}
	}
	return "" // 遍历完没命中，返回空字符串
}

// isMatch 是真正干底层脏活的正则引擎
func isMatch(rule Rule, toolType string, target string) bool {
	var pattern string

	// Go 语言的 switch-case 极其强大！
	// 最关键的是：它不需要像 C/C++ 那样在每个 case 结尾写 break，匹配到自动就跳出了！
	switch toolType {
	case "Read", "Write":
		pattern = rule.Target // 文件操作，我们去匹配 YAML 里的 target 字段
	case "Bash":
		pattern = rule.Command // 终端命令，匹配 command 字段
	case "WebFetch":
		pattern = rule.Domain // 网络请求，匹配 domain 字段
	default:
		return false
	}

	// 如果 YAML 里根本没填这个字段（比如防误触），直接算不匹配
	if pattern == "" {
		return false
	}

	// 对于 Bash 命令使用自定义匹配，其他仍用 doublestar
	if toolType == "Bash" {
		return matchCommand(pattern, target)
	}

	// 最重要的重构点在这里：
	// 我们彻底删除了 filepath.Match 和那段补丁，直接调用 doublestar.Match！
	// 这个方法原生完美支持像 "src/**/*.go"、".git/**"、"*.mining.com" 这样的复杂逻辑。
	matched, err := doublestar.Match(pattern, target)
	if err != nil {
		return false // 如果用户在 YAML 里的正则写错了，为了安全直接当做没匹配上
	}

	return matched
}

// matchCommand 使用正则实现命令字符串的通配符匹配，支持 * 和 **
func matchCommand(pattern, command string) bool {
	// 转义正则元字符，避免用户输入中的 . ? + 等被误解
	rePattern := regexp.QuoteMeta(pattern)
	// 先处理 **，再处理 *（顺序重要）
	rePattern = strings.ReplaceAll(rePattern, `\*\*`, `.*`)
	rePattern = strings.ReplaceAll(rePattern, `\*`, `.*`)
	// 完整匹配
	re, err := regexp.Compile("^" + rePattern + "$")
	if err != nil {
		return false
	}
	return re.MatchString(command)
}

// extractPermission 是一个简单的映射器，根据工具类型，去结构体里拿对应的 r, w, x, n 字段
func extractPermission(rule Rule, toolType string) string {
	switch toolType {
	case "Read":
		return rule.R
	case "Write":
		return rule.W
	case "Bash":
		return rule.X
	case "WebFetch":
		return rule.N
	}
	return ""
}
