package gateway

import (
	"context"
	"log"
	"strings"
	"sync"
)

type requestSourceContextKey struct{}
type requestTokenContextKey struct{}
type connectionAuthStateContextKey struct{}
type tokenAuthenticatorContextKey struct{}
type requestACLContextKey struct{}
type gatewayMetricsContextKey struct{}
type gatewayLoggerContextKey struct{}

// ConnectionAuthState 表示单连接复用的认证状态。
type ConnectionAuthState struct {
	mu            sync.RWMutex
	authenticated bool
	subjectID     string
}

// NewConnectionAuthState 创建连接认证状态对象。
func NewConnectionAuthState() *ConnectionAuthState {
	return &ConnectionAuthState{}
}

// MarkAuthenticated 将当前连接标记为已认证。
func (s *ConnectionAuthState) MarkAuthenticated(subjectID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.authenticated = true
	s.subjectID = strings.TrimSpace(subjectID)
	s.mu.Unlock()
}

// IsAuthenticated 返回当前连接认证状态。
func (s *ConnectionAuthState) IsAuthenticated() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authenticated
}

// SubjectID 返回当前连接绑定的主体标识。
func (s *ConnectionAuthState) SubjectID() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.subjectID)
}

// WithRequestSource 向上下文写入请求来源。
func WithRequestSource(ctx context.Context, source RequestSource) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestSourceContextKey{}, NormalizeRequestSource(source))
}

// RequestSourceFromContext 从上下文读取请求来源。
func RequestSourceFromContext(ctx context.Context) RequestSource {
	if ctx == nil {
		return RequestSourceUnknown
	}
	if source, ok := ctx.Value(requestSourceContextKey{}).(RequestSource); ok {
		return NormalizeRequestSource(source)
	}
	return RequestSourceUnknown
}

// WithRequestToken 向上下文写入单请求 Token。
func WithRequestToken(ctx context.Context, token string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestTokenContextKey{}, strings.TrimSpace(token))
}

// RequestTokenFromContext 从上下文读取单请求 Token。
func RequestTokenFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	token, _ := ctx.Value(requestTokenContextKey{}).(string)
	return strings.TrimSpace(token)
}

// WithConnectionAuthState 向上下文写入连接认证状态。
func WithConnectionAuthState(ctx context.Context, state *ConnectionAuthState) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, connectionAuthStateContextKey{}, state)
}

// ConnectionAuthStateFromContext 从上下文读取连接认证状态。
func ConnectionAuthStateFromContext(ctx context.Context) (*ConnectionAuthState, bool) {
	if ctx == nil {
		return nil, false
	}
	state, ok := ctx.Value(connectionAuthStateContextKey{}).(*ConnectionAuthState)
	if !ok || state == nil {
		return nil, false
	}
	return state, true
}

// WithTokenAuthenticator 向上下文写入 Token 校验器。
func WithTokenAuthenticator(ctx context.Context, authenticator TokenAuthenticator) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, tokenAuthenticatorContextKey{}, authenticator)
}

// TokenAuthenticatorFromContext 从上下文读取 Token 校验器。
func TokenAuthenticatorFromContext(ctx context.Context) (TokenAuthenticator, bool) {
	if ctx == nil {
		return nil, false
	}
	authenticator, ok := ctx.Value(tokenAuthenticatorContextKey{}).(TokenAuthenticator)
	if !ok || authenticator == nil {
		return nil, false
	}
	return authenticator, true
}

// WithRequestACL 向上下文写入 ACL 实例。
func WithRequestACL(ctx context.Context, acl *ControlPlaneACL) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestACLContextKey{}, acl)
}

// RequestACLFromContext 从上下文读取 ACL。
func RequestACLFromContext(ctx context.Context) (*ControlPlaneACL, bool) {
	if ctx == nil {
		return nil, false
	}
	acl, ok := ctx.Value(requestACLContextKey{}).(*ControlPlaneACL)
	if !ok || acl == nil {
		return nil, false
	}
	return acl, true
}

// WithGatewayMetrics 向上下文写入网关指标收集器。
func WithGatewayMetrics(ctx context.Context, metrics *GatewayMetrics) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, gatewayMetricsContextKey{}, metrics)
}

// GatewayMetricsFromContext 从上下文读取网关指标收集器。
func GatewayMetricsFromContext(ctx context.Context) (*GatewayMetrics, bool) {
	if ctx == nil {
		return nil, false
	}
	metrics, ok := ctx.Value(gatewayMetricsContextKey{}).(*GatewayMetrics)
	if !ok || metrics == nil {
		return nil, false
	}
	return metrics, true
}

// WithGatewayLogger 向上下文写入结构化日志使用的 logger。
func WithGatewayLogger(ctx context.Context, logger *log.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, gatewayLoggerContextKey{}, logger)
}

// GatewayLoggerFromContext 从上下文读取 logger。
func GatewayLoggerFromContext(ctx context.Context) (*log.Logger, bool) {
	if ctx == nil {
		return nil, false
	}
	logger, ok := ctx.Value(gatewayLoggerContextKey{}).(*log.Logger)
	if !ok || logger == nil {
		return nil, false
	}
	return logger, true
}

// AuthenticatedSubjectIDFromContext 从上下文解析已认证主体标识。
func AuthenticatedSubjectIDFromContext(ctx context.Context) string {
	if authState, ok := ConnectionAuthStateFromContext(ctx); ok {
		if subjectID := strings.TrimSpace(authState.SubjectID()); subjectID != "" {
			return subjectID
		}
	}

	authenticator, ok := TokenAuthenticatorFromContext(ctx)
	if !ok {
		return ""
	}
	requestToken := RequestTokenFromContext(ctx)
	if requestToken == "" {
		return ""
	}

	subjectID, valid := authenticator.ResolveSubjectID(requestToken)
	if !valid {
		return ""
	}
	return strings.TrimSpace(subjectID)
}
