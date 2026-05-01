package gateway

import (
	"strings"
)

// RequestSource 表示控制面请求来源，用于 ACL 与日志分类。
type RequestSource string

const (
	// RequestSourceIPC 表示本地 IPC 来源。
	RequestSourceIPC RequestSource = "ipc"
	// RequestSourceHTTP 表示 HTTP /rpc 来源。
	RequestSourceHTTP RequestSource = "http"
	// RequestSourceWS 表示 WebSocket 来源。
	RequestSourceWS RequestSource = "ws"
	// RequestSourceSSE 表示 SSE 来源。
	RequestSourceSSE RequestSource = "sse"
	// RequestSourceUnknown 表示未知来源。
	RequestSourceUnknown RequestSource = "unknown"
)

// ACLMode 表示控制面 ACL 的运行模式。
type ACLMode string

const (
	// ACLModeStrict 表示最小权限默认拒绝模式。
	ACLModeStrict ACLMode = "strict"
)

// TokenAuthenticator 定义 Token 校验能力。
type TokenAuthenticator interface {
	ValidateToken(token string) bool
	ResolveSubjectID(token string) (string, bool)
}

// ControlPlaneACL 表示网关控制面方法级授权策略。
type ControlPlaneACL struct {
	mode    ACLMode
	allow   map[RequestSource]map[string]struct{}
	enabled bool
}

// fullControlPlaneMethods 返回本地控制面（IPC/HTTP/WS）完整方法白名单。
func fullControlPlaneMethods() map[string]struct{} {
	methods := []string{
		"gateway.authenticate",
		"gateway.ping",
		"gateway.bindStream",
		"gateway.run",
		"gateway.compact",
		"gateway.executeSystemTool",
		"gateway.activateSessionSkill",
		"gateway.deactivateSessionSkill",
		"gateway.listSessionSkills",
		"gateway.listAvailableSkills",
		"gateway.cancel",
		"gateway.listSessions",
		"gateway.loadSession",
		"gateway.resolvePermission",
		"gateway.deleteSession",
		"gateway.renameSession",
		"gateway.listFiles",
		"gateway.listModels",
		"gateway.setSessionModel",
		"gateway.getSessionModel",
		"gateway.listProviders",
		"gateway.createCustomProvider",
		"gateway.deleteCustomProvider",
		"gateway.selectProviderModel",
		"gateway.listMCPServers",
		"gateway.upsertMCPServer",
		"gateway.setMCPServerEnabled",
		"gateway.deleteMCPServer",
		"wake.openUrl",
	}
	set := make(map[string]struct{}, len(methods))
	for _, m := range methods {
		set[strings.ToLower(strings.TrimSpace(m))] = struct{}{}
	}
	return set
}

// NewStrictControlPlaneACL 创建默认拒绝的严格 ACL。
func NewStrictControlPlaneACL() *ControlPlaneACL {
	localMethods := fullControlPlaneMethods()
	allow := map[RequestSource]map[string]struct{}{
		RequestSourceIPC:  localMethods,
		RequestSourceHTTP: localMethods,
		RequestSourceWS:   localMethods,
		RequestSourceSSE: {
			strings.ToLower(strings.TrimSpace("gateway.ping")): {},
		},
	}
	return &ControlPlaneACL{
		mode:    ACLModeStrict,
		allow:   allow,
		enabled: true,
	}
}

// IsAllowed 判断来源与方法组合是否允许通过授权校验。
func (a *ControlPlaneACL) IsAllowed(source RequestSource, method string) bool {
	if a == nil || !a.enabled {
		return true
	}
	normalizedSource := NormalizeRequestSource(source)
	normalizedMethod := strings.ToLower(strings.TrimSpace(method))
	if normalizedMethod == "" {
		return false
	}
	methodSet, exists := a.allow[normalizedSource]
	if !exists {
		return false
	}
	_, allowed := methodSet[normalizedMethod]
	return allowed
}

// Mode 返回 ACL 当前模式。
func (a *ControlPlaneACL) Mode() ACLMode {
	if a == nil {
		return ACLModeStrict
	}
	return a.mode
}

// NormalizeRequestSource 归一化请求来源值。
func NormalizeRequestSource(source RequestSource) RequestSource {
	switch RequestSource(strings.ToLower(strings.TrimSpace(string(source)))) {
	case RequestSourceIPC:
		return RequestSourceIPC
	case RequestSourceHTTP:
		return RequestSourceHTTP
	case RequestSourceWS:
		return RequestSourceWS
	case RequestSourceSSE:
		return RequestSourceSSE
	default:
		return RequestSourceUnknown
	}
}
