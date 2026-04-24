package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// WritePermissionLevel 描述 token 对写操作的授权等级。
type WritePermissionLevel string

const (
	// WritePermissionNone 表示禁止写操作。
	WritePermissionNone WritePermissionLevel = "none"
	// WritePermissionWorkspace 表示仅允许在 allowlist 路径范围内写。
	WritePermissionWorkspace WritePermissionLevel = "workspace"
	// WritePermissionAny 表示允许写操作（仍受路径与沙箱约束）。
	WritePermissionAny WritePermissionLevel = "any"
)

// Validate 校验写权限等级是否合法。
func (l WritePermissionLevel) Validate() error {
	switch l {
	case WritePermissionNone, WritePermissionWorkspace, WritePermissionAny:
		return nil
	default:
		return fmt.Errorf("security: invalid write permission %q", l)
	}
}

// rank 返回写权限等级的比较顺序值。
func (l WritePermissionLevel) rank() int {
	switch l {
	case WritePermissionNone:
		return 0
	case WritePermissionWorkspace:
		return 1
	case WritePermissionAny:
		return 2
	default:
		return -1
	}
}

// NetworkPermissionMode 描述 token 对网络访问的授权模式。
type NetworkPermissionMode string

const (
	// NetworkPermissionDenyAll 表示禁止网络访问。
	NetworkPermissionDenyAll NetworkPermissionMode = "deny_all"
	// NetworkPermissionAllowHosts 表示仅允许访问白名单 host。
	NetworkPermissionAllowHosts NetworkPermissionMode = "allow_hosts"
	// NetworkPermissionAllowAll 表示允许访问任意 host。
	NetworkPermissionAllowAll NetworkPermissionMode = "allow_all"
)

// Validate 校验网络授权模式是否合法。
func (m NetworkPermissionMode) Validate() error {
	switch m {
	case NetworkPermissionDenyAll, NetworkPermissionAllowHosts, NetworkPermissionAllowAll:
		return nil
	default:
		return fmt.Errorf("security: invalid network permission %q", m)
	}
}

// NetworkPolicy 描述 token 的网络访问策略。
type NetworkPolicy struct {
	Mode         NetworkPermissionMode `json:"mode"`
	AllowedHosts []string              `json:"allowed_hosts,omitempty"`
}

// normalize 归一化网络策略并应用默认值。
func (p NetworkPolicy) normalize() NetworkPolicy {
	out := NetworkPolicy{
		Mode:         p.Mode,
		AllowedHosts: normalizeLowerDistinctList(p.AllowedHosts),
	}
	if out.Mode == "" {
		out.Mode = NetworkPermissionDenyAll
	}
	return out
}

// Validate 校验网络策略结构是否合法。
func (p NetworkPolicy) Validate() error {
	if err := p.Mode.Validate(); err != nil {
		return err
	}
	if p.Mode == NetworkPermissionAllowHosts && len(p.AllowedHosts) == 0 {
		return errors.New("security: network allow_hosts requires at least one host")
	}
	return nil
}

// CapabilityToken 是子代理工具调用所需的能力令牌。
type CapabilityToken struct {
	ID              string               `json:"id"`
	TaskID          string               `json:"task_id"`
	AgentID         string               `json:"agent_id"`
	ParentAgentID   string               `json:"parent_agent_id,omitempty"`
	IssuedAt        time.Time            `json:"issued_at"`
	ExpiresAt       time.Time            `json:"expires_at"`
	AllowedTools    []string             `json:"allowed_tools"`
	AllowedPaths    []string             `json:"allowed_paths,omitempty"`
	NetworkPolicy   NetworkPolicy        `json:"network_policy"`
	WritePermission WritePermissionLevel `json:"write_permission"`
	Signature       string               `json:"signature"`
}

const (
	// CapabilityRuleID 是 capability 决策在权限结果中的统一规则标识。
	CapabilityRuleID = "capability-token"
)

// Normalize 返回去重、清洗后的 token 副本。
func (t CapabilityToken) Normalize() CapabilityToken {
	out := t
	out.ID = strings.TrimSpace(out.ID)
	out.TaskID = strings.TrimSpace(out.TaskID)
	out.AgentID = strings.TrimSpace(out.AgentID)
	out.ParentAgentID = strings.TrimSpace(out.ParentAgentID)
	if !out.IssuedAt.IsZero() {
		out.IssuedAt = out.IssuedAt.UTC()
	}
	if !out.ExpiresAt.IsZero() {
		out.ExpiresAt = out.ExpiresAt.UTC()
	}
	out.AllowedTools = normalizeLowerDistinctList(out.AllowedTools)
	out.AllowedPaths = normalizePathDistinctList(out.AllowedPaths)
	out.NetworkPolicy = out.NetworkPolicy.normalize()
	if out.WritePermission == "" {
		out.WritePermission = WritePermissionNone
	}
	out.Signature = strings.TrimSpace(out.Signature)
	return out
}

// ValidateShape 校验 token 结构合法性（不校验时钟窗口）。
func (t CapabilityToken) ValidateShape() error {
	normalized := t.Normalize()
	if normalized.ID == "" {
		return errors.New("security: capability token id is empty")
	}
	if normalized.TaskID == "" {
		return errors.New("security: capability token task_id is empty")
	}
	if normalized.AgentID == "" {
		return errors.New("security: capability token agent_id is empty")
	}
	if normalized.IssuedAt.IsZero() || normalized.ExpiresAt.IsZero() {
		return errors.New("security: capability token issued_at/expires_at is required")
	}
	if !normalized.ExpiresAt.After(normalized.IssuedAt) {
		return errors.New("security: capability token expires_at must be after issued_at")
	}
	if len(normalized.AllowedTools) == 0 {
		return errors.New("security: capability token allowed_tools is empty")
	}
	if err := normalized.NetworkPolicy.Validate(); err != nil {
		return err
	}
	if err := normalized.WritePermission.Validate(); err != nil {
		return err
	}
	return nil
}

// ValidateAt 校验 token 在给定时间点是否有效。
func (t CapabilityToken) ValidateAt(now time.Time) error {
	normalized := t.Normalize()
	if err := normalized.ValidateShape(); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	if now.Before(normalized.IssuedAt) {
		return errors.New("security: capability token not active yet")
	}
	if !now.Before(normalized.ExpiresAt) {
		return errors.New("security: capability token expired")
	}
	return nil
}

type capabilitySigningPayload struct {
	ID              string               `json:"id"`
	TaskID          string               `json:"task_id"`
	AgentID         string               `json:"agent_id"`
	ParentAgentID   string               `json:"parent_agent_id,omitempty"`
	IssuedAtUnix    int64                `json:"issued_at_unix"`
	ExpiresAtUnix   int64                `json:"expires_at_unix"`
	AllowedTools    []string             `json:"allowed_tools"`
	AllowedPaths    []string             `json:"allowed_paths,omitempty"`
	NetworkPolicy   NetworkPolicy        `json:"network_policy"`
	WritePermission WritePermissionLevel `json:"write_permission"`
}

// payloadForSigning 返回 token 的稳定签名载荷。
func (t CapabilityToken) payloadForSigning() capabilitySigningPayload {
	normalized := t.Normalize()
	return capabilitySigningPayload{
		ID:              normalized.ID,
		TaskID:          normalized.TaskID,
		AgentID:         normalized.AgentID,
		ParentAgentID:   normalized.ParentAgentID,
		IssuedAtUnix:    normalized.IssuedAt.Unix(),
		ExpiresAtUnix:   normalized.ExpiresAt.Unix(),
		AllowedTools:    append([]string(nil), normalized.AllowedTools...),
		AllowedPaths:    append([]string(nil), normalized.AllowedPaths...),
		NetworkPolicy:   normalized.NetworkPolicy,
		WritePermission: normalized.WritePermission,
	}
}

// CapabilitySigner 负责 capability token 的签名与验签。
type CapabilitySigner struct {
	secret []byte
}

// NewCapabilitySigner 用固定密钥构造签名器。
func NewCapabilitySigner(secret []byte) (*CapabilitySigner, error) {
	if len(secret) < 16 {
		return nil, errors.New("security: capability signer secret is too short")
	}
	cloned := append([]byte(nil), secret...)
	return &CapabilitySigner{secret: cloned}, nil
}

// NewEphemeralCapabilitySigner 创建进程内临时签名器。
func NewEphemeralCapabilitySigner() (*CapabilitySigner, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("security: generate capability signer key: %w", err)
	}
	return NewCapabilitySigner(key)
}

// Sign 对 token 执行签名并返回带 signature 的副本。
func (s *CapabilitySigner) Sign(token CapabilityToken) (CapabilityToken, error) {
	if s == nil {
		return CapabilityToken{}, errors.New("security: capability signer is nil")
	}
	normalized := token.Normalize()
	normalized.Signature = ""
	if err := normalized.ValidateShape(); err != nil {
		return CapabilityToken{}, err
	}

	signature, err := s.signature(normalized.payloadForSigning())
	if err != nil {
		return CapabilityToken{}, err
	}
	normalized.Signature = signature
	return normalized, nil
}

// Verify 校验 token 的完整性签名。
func (s *CapabilitySigner) Verify(token CapabilityToken) error {
	if s == nil {
		return errors.New("security: capability signer is nil")
	}
	normalized := token.Normalize()
	if strings.TrimSpace(normalized.Signature) == "" {
		return errors.New("security: capability token signature is empty")
	}
	provided := normalized.Signature
	normalized.Signature = ""
	if err := normalized.ValidateShape(); err != nil {
		return err
	}
	expected, err := s.signature(normalized.payloadForSigning())
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(provided), []byte(expected)) {
		return errors.New("security: capability token signature mismatch")
	}
	return nil
}

// signature 计算稳定 payload 的 HMAC-SHA256 签名。
func (s *CapabilitySigner) signature(payload capabilitySigningPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("security: marshal capability signing payload: %w", err)
	}
	mac := hmac.New(sha256.New, s.secret)
	if _, err := mac.Write(raw); err != nil {
		return "", fmt.Errorf("security: sign capability payload: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// EnsureCapabilitySubset 校验 child token 是否为 parent token 的严格子集。
func EnsureCapabilitySubset(parent CapabilityToken, child CapabilityToken) error {
	parent = parent.Normalize()
	child = child.Normalize()
	if err := parent.ValidateShape(); err != nil {
		return fmt.Errorf("security: invalid parent capability token: %w", err)
	}
	if err := child.ValidateShape(); err != nil {
		return fmt.Errorf("security: invalid child capability token: %w", err)
	}
	if child.ExpiresAt.After(parent.ExpiresAt) {
		return errors.New("security: child capability expires_at exceeds parent")
	}
	if child.WritePermission.rank() > parent.WritePermission.rank() {
		return errors.New("security: child write permission exceeds parent")
	}
	if !isSubsetExact(parent.AllowedTools, child.AllowedTools) {
		return errors.New("security: child allowed_tools exceeds parent")
	}
	if !isPathSubset(parent.AllowedPaths, child.AllowedPaths) {
		return errors.New("security: child allowed_paths exceeds parent")
	}
	if err := ensureNetworkSubset(parent.NetworkPolicy, child.NetworkPolicy); err != nil {
		return err
	}
	return nil
}

// EvaluateCapabilityAction 判断 token 是否允许当前 action。
func EvaluateCapabilityAction(token CapabilityToken, action Action, now time.Time) (bool, string) {
	token = token.Normalize()
	if err := token.ValidateAt(now); err != nil {
		return false, err.Error()
	}
	if err := action.Validate(); err != nil {
		return false, err.Error()
	}

	toolName := strings.ToLower(strings.TrimSpace(action.Payload.ToolName))
	resource := strings.ToLower(strings.TrimSpace(action.Payload.Resource))
	if !matchesCapabilityTool(token.AllowedTools, toolName, resource) {
		return false, "capability token tool not allowed"
	}

	if action.Type == ActionTypeWrite && token.WritePermission == WritePermissionNone {
		return false, "capability token write permission denied"
	}

	if host, ok := extractActionNetworkHost(action); ok {
		if allowed, reason := allowNetworkHost(token.NetworkPolicy, host); !allowed {
			return false, reason
		}
	}

	if targetPath, ok, traversal := extractActionPath(action); ok {
		if traversal {
			return false, "capability token blocked path traversal target"
		}
		if !allowPathByList(token.AllowedPaths, targetPath) {
			return false, "capability token path not allowed"
		}
	}

	return true, ""
}

// ValidateCapabilityForWorkspace 在沙箱阶段复核 token 的路径约束。
func ValidateCapabilityForWorkspace(action Action) error {
	token := action.Payload.CapabilityToken
	if token == nil {
		return nil
	}

	targetPath, ok, traversal := extractActionPath(action)
	if !ok {
		return nil
	}
	if traversal {
		return errors.New("security: capability token blocked path traversal target")
	}
	if !allowPathByList(token.Normalize().AllowedPaths, targetPath) {
		return errors.New("security: capability token path not allowed")
	}
	return nil
}

// extractActionPath 返回 action 中用于路径授权判定的目标路径。
func extractActionPath(action Action) (string, bool, bool) {
	targetType := action.Payload.SandboxTargetType
	if targetType == "" {
		targetType = action.Payload.TargetType
	}

	switch targetType {
	case TargetTypePath, TargetTypeDirectory:
	default:
		return "", false, false
	}

	raw := strings.TrimSpace(action.Payload.SandboxTarget)
	if raw == "" {
		raw = strings.TrimSpace(action.Payload.Target)
	}
	if raw == "" {
		return "", false, false
	}
	return resolveActionPath(raw, action.Payload.Workdir), true, hasTraversal(raw)
}

// extractActionNetworkHost 返回 action 中的网络目标 host。
func extractActionNetworkHost(action Action) (string, bool) {
	targetType := action.Payload.TargetType
	resource := strings.ToLower(strings.TrimSpace(action.Payload.Resource))
	if targetType != TargetTypeURL && resource != "webfetch" {
		return "", false
	}

	raw := strings.TrimSpace(action.Payload.Target)
	if raw == "" {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return "", false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", false
	}
	return host, true
}

// allowNetworkHost 依据 token 网络策略判断 host 是否可访问。
func allowNetworkHost(policy NetworkPolicy, host string) (bool, string) {
	policy = policy.normalize()
	host = strings.ToLower(strings.TrimSpace(host))
	switch policy.Mode {
	case NetworkPermissionAllowAll:
		return true, ""
	case NetworkPermissionDenyAll:
		return false, "capability token network policy denies all"
	case NetworkPermissionAllowHosts:
		if matchesCapabilityHost(policy.AllowedHosts, host) {
			return true, ""
		}
		return false, "capability token host not allowed"
	default:
		return false, "capability token network policy is invalid"
	}
}

// matchesCapabilityTool 判断工具名或资源是否命中 token allowlist。
func matchesCapabilityTool(allowlist []string, toolName string, resource string) bool {
	if len(allowlist) == 0 {
		return false
	}
	for _, pattern := range allowlist {
		if pattern == toolName || pattern == resource {
			return true
		}
		matchedTool, errTool := filepath.Match(pattern, toolName)
		if errTool == nil && matchedTool {
			return true
		}
		matchedResource, errResource := filepath.Match(pattern, resource)
		if errResource == nil && matchedResource {
			return true
		}
	}
	return false
}

// matchesCapabilityHost 支持 host 精确与 *.domain 通配匹配。
func matchesCapabilityHost(allowHosts []string, host string) bool {
	for _, allowed := range allowHosts {
		if allowed == host {
			return true
		}
		if strings.HasPrefix(allowed, "*.") && strings.HasSuffix(host, allowed[1:]) {
			return true
		}
	}
	return false
}

// allowPathByList 判断目标路径是否落在 allowlist 前缀范围内。
func allowPathByList(allowlist []string, target string) bool {
	if len(allowlist) == 0 {
		return false
	}
	target = normalizePathKey(target)
	for _, allowed := range allowlist {
		base := normalizePathKey(allowed)
		if base == "" {
			continue
		}
		if target == base || strings.HasPrefix(target, base+"/") {
			return true
		}
	}
	return false
}

// normalizePathDistinctList 对路径列表做清洗、去重并排序，保证签名稳定。
func normalizePathDistinctList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizePathKey(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

// normalizeLowerDistinctList 对字符串列表做 lower/trim/去重并排序。
func normalizeLowerDistinctList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

// normalizePathKey 生成统一比较用路径键：始终清理路径与分隔符，仅在 Windows 下忽略大小写。
func normalizePathKey(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	normalized := filepath.ToSlash(filepath.Clean(trimmed))
	if runtime.GOOS == "windows" {
		return strings.ToLower(normalized)
	}
	return normalized
}

// resolveActionPath 基于 workdir 解析 action 的路径目标，确保相对路径能与 allowlist 比较。
func resolveActionPath(target string, workdir string) string {
	resolved := strings.TrimSpace(target)
	if resolved == "" {
		return ""
	}
	if !filepath.IsAbs(resolved) {
		base := strings.TrimSpace(workdir)
		if base != "" {
			resolved = filepath.Join(base, resolved)
		}
	}
	return normalizePathKey(resolved)
}

// hasTraversal 判断原始路径文本是否包含明显 traversal 段。
func hasTraversal(path string) bool {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	normalized = strings.ReplaceAll(normalized, `\`, "/")
	if normalized == "" {
		return false
	}
	return normalized == ".." ||
		strings.HasPrefix(normalized, "../") ||
		strings.Contains(normalized, "/../")
}

// isSubsetExact 判断 child 是否是 parent 的集合子集。
func isSubsetExact(parent []string, child []string) bool {
	parentSet := make(map[string]struct{}, len(parent))
	for _, value := range parent {
		parentSet[value] = struct{}{}
	}
	for _, value := range child {
		if _, ok := parentSet[value]; !ok {
			return false
		}
	}
	return true
}

// isPathSubset 判断 child 路径是否全部落在 parent 路径前缀范围内。
func isPathSubset(parent []string, child []string) bool {
	if len(child) == 0 {
		return true
	}
	if len(parent) == 0 {
		return false
	}
	for _, candidate := range child {
		matched := false
		for _, base := range parent {
			if candidate == base || strings.HasPrefix(candidate, base+"/") {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// ensureNetworkSubset 判断 child 网络策略是否是 parent 的子集。
func ensureNetworkSubset(parent NetworkPolicy, child NetworkPolicy) error {
	parent = parent.normalize()
	child = child.normalize()
	switch parent.Mode {
	case NetworkPermissionAllowAll:
		return nil
	case NetworkPermissionDenyAll:
		if child.Mode != NetworkPermissionDenyAll {
			return errors.New("security: child network policy exceeds parent")
		}
		return nil
	case NetworkPermissionAllowHosts:
		if child.Mode == NetworkPermissionAllowAll {
			return errors.New("security: child network policy exceeds parent")
		}
		if child.Mode == NetworkPermissionDenyAll {
			return nil
		}
		if !isSubsetExact(parent.AllowedHosts, child.AllowedHosts) {
			return errors.New("security: child allowed_hosts exceeds parent")
		}
		return nil
	default:
		return errors.New("security: invalid parent network policy")
	}
}
