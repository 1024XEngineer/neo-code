package tools

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"neo-code/internal/security"
)

// SessionPermissionScope 表示 session 级权限记忆的作用范围。
type SessionPermissionScope string

const (
	// SessionPermissionScopeOnce 表示仅当前一次请求放行。
	SessionPermissionScopeOnce SessionPermissionScope = "once"
	// SessionPermissionScopeAlways 表示当前会话内同类请求持续放行。
	SessionPermissionScopeAlways SessionPermissionScope = "always_session"
	// SessionPermissionScopeReject 表示当前会话内同类请求持续拒绝。
	SessionPermissionScopeReject SessionPermissionScope = "reject"
)

type sessionPermissionEntry struct {
	decision  security.Decision
	scope     SessionPermissionScope
	remaining int
}

// sessionPermissionMemory 管理按 session/action 维度的审批记忆。
type sessionPermissionMemory struct {
	mu      sync.Mutex
	entries map[string]map[string]sessionPermissionEntry
}

// newSessionPermissionMemory 创建 session 级权限记忆存储。
func newSessionPermissionMemory() *sessionPermissionMemory {
	return &sessionPermissionMemory{
		entries: make(map[string]map[string]sessionPermissionEntry),
	}
}

// remember 记录一条 session 级权限决策。
func (m *sessionPermissionMemory) remember(sessionID string, action security.Action, scope SessionPermissionScope) error {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return errors.New("tools: session id is empty")
	}
	if err := action.Validate(); err != nil {
		return err
	}

	var entry sessionPermissionEntry
	switch scope {
	case SessionPermissionScopeOnce:
		entry = sessionPermissionEntry{
			decision:  security.DecisionAllow,
			scope:     scope,
			remaining: 1,
		}
	case SessionPermissionScopeAlways:
		entry = sessionPermissionEntry{
			decision:  security.DecisionAllow,
			scope:     scope,
			remaining: -1,
		}
	case SessionPermissionScopeReject:
		entry = sessionPermissionEntry{
			decision:  security.DecisionDeny,
			scope:     scope,
			remaining: -1,
		}
	default:
		return fmt.Errorf("tools: unsupported session permission scope %q", scope)
	}
	if shouldSkipSessionPermissionRemember(action, scope) {
		return nil
	}

	actionKey := sessionPermissionActionKey(action)
	m.mu.Lock()
	defer m.mu.Unlock()
	sessionEntries, ok := m.entries[trimmedSessionID]
	if !ok {
		sessionEntries = make(map[string]sessionPermissionEntry)
		m.entries[trimmedSessionID] = sessionEntries
	}
	sessionEntries[actionKey] = entry
	return nil
}

// resolve 查询并按 scope 规则消费 session 级权限记忆。
func (m *sessionPermissionMemory) resolve(sessionID string, action security.Action) (security.Decision, SessionPermissionScope, bool) {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return "", "", false
	}
	actionKey := sessionPermissionActionKey(action)

	m.mu.Lock()
	defer m.mu.Unlock()

	sessionEntries, ok := m.entries[trimmedSessionID]
	if !ok {
		return "", "", false
	}
	entry, ok := sessionEntries[actionKey]
	if !ok {
		return "", "", false
	}

	if entry.scope == SessionPermissionScopeOnce && entry.remaining > 0 {
		entry.remaining--
		if entry.remaining <= 0 {
			delete(sessionEntries, actionKey)
		} else {
			sessionEntries[actionKey] = entry
		}
	}

	if len(sessionEntries) == 0 {
		delete(m.entries, trimmedSessionID)
	}

	return entry.decision, entry.scope, true
}

// sessionPermissionActionKey 基于结构化 action 生成稳定匹配键。
func sessionPermissionActionKey(action security.Action) string {
	return strings.Join([]string{
		string(action.Type),
		sessionPermissionCategory(action),
		sessionPermissionTargetScope(action),
	}, "|")
}

// sessionPermissionCategory 将安全动作归一为稳定的工具类别。
// 类别用于聚合同类工具，再配合 target scope 控制最小授权范围。
func sessionPermissionCategory(action security.Action) string {
	resource := strings.ToLower(strings.TrimSpace(action.Payload.Resource))
	switch action.Type {
	case security.ActionTypeRead:
		if strings.HasPrefix(resource, "filesystem_") {
			return "filesystem_read"
		}
		if resource == "webfetch" {
			return "webfetch"
		}
	case security.ActionTypeWrite:
		if strings.HasPrefix(resource, "filesystem_") {
			return "filesystem_write"
		}
	case security.ActionTypeBash:
		if strings.EqualFold(strings.TrimSpace(action.Payload.SemanticType), "git") {
			semanticClass := strings.ToLower(strings.TrimSpace(action.Payload.SemanticClass))
			if semanticClass == "" {
				semanticClass = BashIntentClassificationUnknown
			}
			return "bash_git_" + semanticClass
		}
		return "bash"
	case security.ActionTypeMCP:
		if serverIdentity := mcpServerTarget(action.Payload.Target); serverIdentity != "" {
			return serverIdentity
		}
		return "mcp"
	}

	toolName := strings.ToLower(strings.TrimSpace(action.Payload.ToolName))
	if toolName != "" {
		return toolName
	}
	return resource
}

// sessionPermissionTargetScope 基于 action 的 target 生成最小授权范围键。
func sessionPermissionTargetScope(action security.Action) string {
	if action.Type == security.ActionTypeBash {
		if fingerprint := strings.TrimSpace(action.Payload.PermissionFingerprint); fingerprint != "" {
			return fingerprint
		}
	}
	target := strings.TrimSpace(action.Payload.Target)
	if target == "" {
		return "*"
	}

	switch action.Payload.TargetType {
	case security.TargetTypeURL:
		return normalizePermissionURLTarget(target)
	case security.TargetTypePath:
		return normalizePermissionPathTarget(filepath.Dir(target))
	case security.TargetTypeDirectory:
		return normalizePermissionPathTarget(target)
	case security.TargetTypeCommand:
		return normalizePermissionCommandTarget(target)
	case security.TargetTypeMCP:
		return normalizeMCPToolIdentity(target)
	default:
		return strings.ToLower(target)
	}
}

// normalizePermissionURLTarget 将 URL 归一到 host[:port] 维度。
func normalizePermissionURLTarget(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(parsed.Host) == "" {
		return strings.ToLower(strings.TrimSpace(raw))
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if port := strings.TrimSpace(parsed.Port()); port != "" {
		host += ":" + port
	}
	return host
}

// normalizePermissionPathTarget 统一路径分隔并按平台无关形式生成匹配键。
func normalizePermissionPathTarget(raw string) string {
	cleaned := filepath.Clean(strings.TrimSpace(raw))
	if cleaned == "." || cleaned == "" {
		return "."
	}
	return strings.ToLower(filepath.ToSlash(cleaned))
}

// normalizePermissionCommandTarget 归一化命令目标，降低仅空白/换行差异导致的会话授权失配。
func normalizePermissionCommandTarget(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "*"
	}
	trimmed = strings.ReplaceAll(trimmed, "\r\n", "\n")
	trimmed = strings.ReplaceAll(trimmed, "\r", "\n")
	return strings.ToLower(strings.Join(strings.Fields(trimmed), " "))
}

// shouldSkipSessionPermissionRemember 判断当前 action 在给定 scope 下是否应跳过会话级记忆。
func shouldSkipSessionPermissionRemember(action security.Action, scope SessionPermissionScope) bool {
	if action.Type != security.ActionTypeBash {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(action.Payload.SemanticType), "git") {
		return false
	}
	return scope == SessionPermissionScopeAlways &&
		strings.EqualFold(strings.TrimSpace(action.Payload.SemanticClass), BashIntentClassificationRemoteOp)
}
