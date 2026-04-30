package tools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	providertypes "neo-code/internal/provider/types"
	"neo-code/internal/security"
)

// SpecListInput carries future session and agent context for tool filtering.
type SpecListInput struct {
	SessionID string
	Agent     string
	Query     string
	Mode      string
	ReadOnly  bool
}

// Manager is the runtime-facing tool execution and schema exposure boundary.
type Manager interface {
	ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error)
	MicroCompactPolicy(name string) MicroCompactPolicy
	MicroCompactSummarizer(name string) ContentSummarizer
	// Execute 必须支持并发调用；runtime 可能在同一轮中并行调度多个工具调用。
	Execute(ctx context.Context, input ToolCallInput) (ToolResult, error)
	RememberSessionDecision(sessionID string, action security.Action, scope SessionPermissionScope) error
}

// Executor is the concrete tool execution layer under the manager.
type Executor interface {
	ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error)
	Execute(ctx context.Context, input ToolCallInput) (ToolResult, error)
	Supports(name string) bool
}

type microCompactPolicyExecutor interface {
	MicroCompactPolicy(name string) MicroCompactPolicy
}

type microCompactSummarizerExecutor interface {
	MicroCompactSummarizer(name string) ContentSummarizer
}

// factsEnrichingExecutor 包装底层执行器，在不信任外部 metadata 的前提下补齐受信结构化事实。
type factsEnrichingExecutor struct {
	inner Executor
}

// newFactsEnrichingExecutor 创建带结构化事实补齐能力的执行器包装层。
func newFactsEnrichingExecutor(inner Executor) Executor {
	if inner == nil {
		return nil
	}
	return &factsEnrichingExecutor{inner: inner}
}

// ListAvailableSpecs 透传工具规格查询能力，不改变可见工具集。
func (e *factsEnrichingExecutor) ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error) {
	return e.inner.ListAvailableSpecs(ctx, input)
}

// Supports 透传工具支持性判断，保证原有执行路由不受包装层影响。
func (e *factsEnrichingExecutor) Supports(name string) bool {
	return e.inner.Supports(name)
}

// MicroCompactPolicy 透传被包装执行器的压缩策略，确保 UI/Runtime 行为与原实现一致。
func (e *factsEnrichingExecutor) MicroCompactPolicy(name string) MicroCompactPolicy {
	if source, ok := e.inner.(microCompactPolicyExecutor); ok {
		return source.MicroCompactPolicy(name)
	}
	return MicroCompactPolicyCompact
}

// MicroCompactSummarizer 透传被包装执行器的摘要器实现，避免包装层吞掉摘要能力。
func (e *factsEnrichingExecutor) MicroCompactSummarizer(name string) ContentSummarizer {
	if source, ok := e.inner.(microCompactSummarizerExecutor); ok {
		return source.MicroCompactSummarizer(name)
	}
	return nil
}

// Execute 在执行后按本地权限动作补齐可信 facts，避免运行时依赖远端 metadata。
func (e *factsEnrichingExecutor) Execute(ctx context.Context, input ToolCallInput) (ToolResult, error) {
	result, err := e.inner.Execute(ctx, input)
	action, actionErr := buildPermissionAction(input)
	if actionErr == nil {
		result = EnrichToolResultFacts(action, result)
	}
	return result, err
}

// WorkspaceSandbox enforces workspace-oriented constraints before execution.
type WorkspaceSandbox interface {
	Check(ctx context.Context, action security.Action) (*security.WorkspaceExecutionPlan, error)
}

// NoopWorkspaceSandbox keeps the explicit sandbox stage in the execution chain
// without changing current behavior.
type NoopWorkspaceSandbox struct{}

// Check implements WorkspaceSandbox.
func (NoopWorkspaceSandbox) Check(ctx context.Context, action security.Action) (*security.WorkspaceExecutionPlan, error) {
	return nil, ctx.Err()
}

var (
	// ErrPermissionDenied 标记工具请求被权限系统拒绝。
	ErrPermissionDenied = errors.New("tools: permission denied")
	// ErrPermissionApprovalRequired 标记工具请求需要用户审批。
	ErrPermissionApprovalRequired = errors.New("tools: permission approval required")
	// ErrCapabilityDenied 标记拒绝由 capability token 触发。
	ErrCapabilityDenied = errors.New("tools: capability denied")
)

const (
	// sandboxExternalWriteApprovalRuleID 是工作区外低风险写入的审批规则标识。
	sandboxExternalWriteApprovalRuleID = "workspace-sandbox:external-write-ask"
	// sandboxExternalWriteApprovalReason 是工作区外低风险写入需要审批时的统一提示。
	sandboxExternalWriteApprovalReason = "workspace write outside workdir requires approval"
)

// PermissionDecisionError reports a non-allow permission decision.
type PermissionDecisionError struct {
	decision security.Decision
	toolName string
	action   security.Action
	reason   string
	ruleID   string
	scope    SessionPermissionScope
}

// Error returns a stable error message for the blocked tool call.
func (e *PermissionDecisionError) Error() string {
	if e == nil {
		return ""
	}

	reason := strings.TrimSpace(e.reason)
	switch e.decision {
	case security.DecisionAsk:
		if reason == "" {
			reason = "permission approval required"
		}
	default:
		if reason == "" {
			reason = "permission denied"
		}
	}
	return "tools: " + reason
}

// Unwrap 返回可用于 errors.Is 判定的哨兵错误集合。
func (e *PermissionDecisionError) Unwrap() []error {
	if e == nil {
		return nil
	}
	switch e.decision {
	case security.DecisionAsk:
		return []error{ErrPermissionApprovalRequired}
	default:
		if strings.EqualFold(strings.TrimSpace(e.ruleID), security.CapabilityRuleID) {
			return []error{ErrPermissionDenied, ErrCapabilityDenied}
		}
		return []error{ErrPermissionDenied}
	}
}

// Decision returns the blocking engine decision.
func (e *PermissionDecisionError) Decision() string {
	if e == nil {
		return ""
	}
	return string(e.decision)
}

// ToolName returns the tool that was blocked.
func (e *PermissionDecisionError) ToolName() string {
	if e == nil {
		return ""
	}
	return e.toolName
}

// Action 返回触发权限决策时的结构化动作上下文。
func (e *PermissionDecisionError) Action() security.Action {
	if e == nil {
		return security.Action{}
	}
	return e.action
}

// Reason 返回权限网关给出的拒绝或审批原因。
func (e *PermissionDecisionError) Reason() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.reason)
}

// RuleID 返回命中规则的标识，未命中时为空字符串。
func (e *PermissionDecisionError) RuleID() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.ruleID)
}

// RememberScope 返回触发该权限结果时命中的会话记忆范围。
func (e *PermissionDecisionError) RememberScope() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(string(e.scope))
}

// DefaultManager routes tool calls through the permission engine, workspace
// sandbox, and executor.
type DefaultManager struct {
	executor         Executor
	engine           security.PermissionEngine
	sandbox          WorkspaceSandbox
	sessionDecisions *sessionPermissionMemory
	capabilityMu     sync.RWMutex
	capabilitySigner *security.CapabilitySigner
}

// NewManager creates a manager that wraps an executor with security checks.
func NewManager(executor Executor, engine security.PermissionEngine, sandbox WorkspaceSandbox) (*DefaultManager, error) {
	if executor == nil {
		return nil, errors.New("tools: executor is nil")
	}
	if engine == nil {
		return nil, errors.New("tools: permission engine is nil")
	}
	if sandbox == nil {
		sandbox = NoopWorkspaceSandbox{}
	}
	capabilitySigner, err := security.NewEphemeralCapabilitySigner()
	if err != nil {
		return nil, err
	}

	return &DefaultManager{
		executor:         newFactsEnrichingExecutor(executor),
		engine:           engine,
		sandbox:          sandbox,
		sessionDecisions: newSessionPermissionMemory(),
		capabilitySigner: capabilitySigner,
	}, nil
}

// SetCapabilitySigner 设置用于 capability token 验签的签名器。
func (m *DefaultManager) SetCapabilitySigner(signer *security.CapabilitySigner) error {
	if m == nil {
		return errors.New("tools: manager is nil")
	}
	if signer == nil {
		return errors.New("tools: capability signer is nil")
	}
	m.capabilityMu.Lock()
	defer m.capabilityMu.Unlock()
	m.capabilitySigner = signer
	return nil
}

// CapabilitySigner 返回当前 manager 使用的 capability 签名器。
func (m *DefaultManager) CapabilitySigner() *security.CapabilitySigner {
	if m == nil {
		return nil
	}
	m.capabilityMu.RLock()
	defer m.capabilityMu.RUnlock()
	return m.capabilitySigner
}

// capabilitySignerSnapshot 返回当前 capability signer 的并发安全快照。
func (m *DefaultManager) capabilitySignerSnapshot() *security.CapabilitySigner {
	if m == nil {
		return nil
	}
	m.capabilityMu.RLock()
	defer m.capabilityMu.RUnlock()
	return m.capabilitySigner
}

// ListAvailableSpecs returns the currently visible tool specs from the executor.
func (m *DefaultManager) ListAvailableSpecs(ctx context.Context, input SpecListInput) ([]providertypes.ToolSpec, error) {
	if m == nil || m.executor == nil {
		return nil, errors.New("tools: manager executor is nil")
	}
	specs, err := m.executor.ListAvailableSpecs(ctx, input)
	if err != nil {
		return nil, err
	}
	if !input.ReadOnly {
		return specs, nil
	}
	filtered := make([]providertypes.ToolSpec, 0, len(specs))
	for _, spec := range specs {
		if isReadOnlyVisibleTool(spec.Name) {
			filtered = append(filtered, spec)
		}
	}
	return filtered, nil
}

// MicroCompactPolicy 返回工具的 micro compact 策略；无法判断时按默认可压缩处理。
func (m *DefaultManager) MicroCompactPolicy(name string) MicroCompactPolicy {
	if m == nil || m.executor == nil {
		return MicroCompactPolicyCompact
	}
	if source, ok := m.executor.(microCompactPolicyExecutor); ok {
		return source.MicroCompactPolicy(name)
	}
	return MicroCompactPolicyCompact
}

// MicroCompactSummarizer 返回工具的内容摘要器；未注册时返回 nil。
func (m *DefaultManager) MicroCompactSummarizer(name string) ContentSummarizer {
	if m == nil || m.executor == nil {
		return nil
	}
	if source, ok := m.executor.(microCompactSummarizerExecutor); ok {
		return source.MicroCompactSummarizer(name)
	}
	return nil
}

// Execute runs the tool if the permission engine allows it and the sandbox
// check passes.
func (m *DefaultManager) Execute(ctx context.Context, input ToolCallInput) (ToolResult, error) {
	if m == nil || m.executor == nil {
		return ToolResult{}, errors.New("tools: manager executor is nil")
	}

	if !m.executor.Supports(input.Name) {
		return m.executor.Execute(ctx, input)
	}

	action, err := buildPermissionAction(input)
	if err != nil {
		result := NewErrorResult(input.Name, "invalid permission action", err.Error(), nil)
		result.ToolCallID = input.ID
		return result, err
	}
	if input.ReadOnly && !isReadOnlyActionAllowed(action) {
		err := fmt.Errorf("tools: tool %q is not available in read-only mode", strings.TrimSpace(input.Name))
		result := NewErrorResult(input.Name, "tool blocked in read-only mode", err.Error(), actionMetadata(action))
		result.ToolCallID = input.ID
		return result, err
	}
	if err := m.verifyCapabilityToken(action); err != nil {
		decision := capabilityDenyDecision(action, err.Error())
		m.auditCapabilityDecision(action, string(decision.Decision), decision.Reason)
		result := blockedToolResult(input, decision)
		return result, permissionErrorFromDecision(decision)
	}

	decision, err := m.engine.Check(ctx, action)
	if err != nil {
		result := NewErrorResult(input.Name, "permission evaluation failed", err.Error(), nil)
		result.ToolCallID = input.ID
		return result, err
	}
	// deny 规则始终优先，避免 session 记忆覆盖硬性安全策略。
	if decision.Decision == security.DecisionDeny {
		if security.IsCapabilityDeniedResult(decision) {
			m.auditCapabilityDecision(action, string(decision.Decision), decision.Reason)
		}
		result := blockedToolResult(input, decision)
		return result, permissionErrorFromDecision(decision)
	}
	// session 记忆仅用于自动处理 ask，不提升原本已 allow 的策略结果。
	if decision.Decision == security.DecisionAsk && m.sessionDecisions != nil {
		if rememberedDecision, rememberedScope, ok := m.sessionDecisions.resolve(input.SessionID, action); ok {
			decision = security.CheckResult{
				Decision: rememberedDecision,
				Action:   action,
				Reason:   sessionDecisionReason(rememberedScope),
			}
			if rememberedScope != "" {
				decision.Rule = &security.Rule{
					ID:       "session-memory:" + string(rememberedScope),
					Decision: rememberedDecision,
					Reason:   decision.Reason,
				}
			}
		}
	}
	if decision.Decision != security.DecisionAllow {
		result := blockedToolResult(input, decision)
		return result, permissionErrorFromDecision(decision)
	}

	plan, err := m.sandbox.Check(ctx, action)
	if err != nil {
		if decision, decisionMatched := resolveSandboxOutsideWriteDecision(input, action, err, m.sessionDecisions); decisionMatched {
			if decision.Decision != security.DecisionAllow {
				result := blockedToolResult(input, decision)
				return result, permissionErrorFromDecision(decision)
			}
			m.auditCapabilityDecision(action, string(security.DecisionAllow), decision.Reason)
			return m.executor.Execute(ctx, input)
		} else {
			result := NewErrorResult(input.Name, "workspace sandbox rejected action", sandboxErrorDetails(action, err), actionMetadata(action))
			result.ToolCallID = input.ID
			return result, err
		}
	} else if plan != nil {
		input.WorkspacePlan = plan
	}
	m.auditCapabilityDecision(action, string(security.DecisionAllow), "")

	return m.executor.Execute(ctx, input)
}

// resolveSandboxOutsideWriteDecision 将“工作区外低风险写入”沙箱拒绝收敛为 ask/remembered allow/remembered deny。
func resolveSandboxOutsideWriteDecision(
	input ToolCallInput,
	action security.Action,
	sandboxErr error,
	sessionMemory *sessionPermissionMemory,
) (security.CheckResult, bool) {
	if !isSandboxOutsideWriteApprovalCandidate(action, sandboxErr) {
		return security.CheckResult{}, false
	}

	decision := security.CheckResult{
		Decision: security.DecisionAsk,
		Action:   action,
		Rule: &security.Rule{
			ID:       sandboxExternalWriteApprovalRuleID,
			Type:     action.Type,
			Resource: action.Payload.Resource,
			Decision: security.DecisionAsk,
			Reason:   sandboxExternalWriteApprovalReason,
		},
		Reason: sandboxExternalWriteApprovalReason,
	}

	if sessionMemory != nil {
		if rememberedDecision, rememberedScope, ok := sessionMemory.resolve(input.SessionID, action); ok {
			decision = security.CheckResult{
				Decision: rememberedDecision,
				Action:   action,
				Rule: &security.Rule{
					ID:       "session-memory:" + string(rememberedScope),
					Type:     action.Type,
					Resource: action.Payload.Resource,
					Decision: rememberedDecision,
					Reason:   sessionDecisionReason(rememberedScope),
				},
				Reason: sessionDecisionReason(rememberedScope),
			}
		}
	}

	return decision, true
}

// isSandboxOutsideWriteApprovalCandidate 判断当前沙箱错误是否可升级为“工作区外低风险写入审批”。
func isSandboxOutsideWriteApprovalCandidate(action security.Action, sandboxErr error) bool {
	if isWorkspaceSymlinkViolationError(sandboxErr) {
		return false
	}
	if !isWorkspaceBoundaryViolationError(sandboxErr) {
		return false
	}
	if action.Type != security.ActionTypeWrite {
		return false
	}
	resource := strings.TrimSpace(strings.ToLower(action.Payload.Resource))
	toolName := strings.TrimSpace(strings.ToLower(action.Payload.ToolName))
	if resource != ToolNameFilesystemWriteFile && toolName != ToolNameFilesystemWriteFile {
		return false
	}

	targetPath := resolveActionSandboxTargetPath(action)
	if targetPath == "" {
		return false
	}
	return isLowRiskExternalWritePath(targetPath)
}

// isWorkspaceBoundaryViolationError 判断错误是否由工作区边界校验触发。
func isWorkspaceBoundaryViolationError(err error) bool {
	return errors.Is(err, security.ErrWorkspaceBoundaryViolation) ||
		errors.Is(err, security.ErrWorkspaceVolumeMismatch)
}

// isWorkspaceSymlinkViolationError 判断沙箱拒绝是否来自符号链接越界逃逸。
func isWorkspaceSymlinkViolationError(err error) bool {
	return errors.Is(err, security.ErrWorkspaceSymlinkViolation)
}

// resolveActionSandboxTargetPath 将 action 的 sandbox target 解析为可判定风险的绝对路径。
func resolveActionSandboxTargetPath(action security.Action) string {
	target := strings.TrimSpace(action.Payload.SandboxTarget)
	if target == "" {
		target = strings.TrimSpace(action.Payload.Target)
	}
	if target == "" {
		return ""
	}
	if !filepath.IsAbs(target) && strings.TrimSpace(action.Payload.Workdir) != "" {
		target = filepath.Join(strings.TrimSpace(action.Payload.Workdir), target)
	}
	if absoluteTarget, err := filepath.Abs(target); err == nil {
		target = absoluteTarget
	}
	return filepath.Clean(target)
}

// isLowRiskExternalWritePath 判断工作区外写入目标是否属于可审批放行的低风险路径。
func isLowRiskExternalWritePath(targetPath string) bool {
	cleaned := strings.TrimSpace(filepath.Clean(targetPath))
	if cleaned == "" || cleaned == "." {
		return false
	}
	if isSystemProtectedPath(cleaned) {
		return false
	}
	if isUserStartupProfilePath(cleaned) {
		return false
	}
	if isHighRiskExecutableExtension(filepath.Ext(cleaned)) {
		return false
	}
	return true
}

// isUserStartupProfilePath 判断路径是否命中用户级 shell/profile 启动文件，命中后必须保持硬拒绝。
func isUserStartupProfilePath(path string) bool {
	return isUserStartupProfilePathForOS(path, runtime.GOOS)
}

// isUserStartupProfilePathForOS 按指定操作系统判定路径是否命中用户级 shell/profile 启动文件。
func isUserStartupProfilePathForOS(path string, goos string) bool {
	cleaned := strings.ToLower(strings.TrimSpace(filepath.Clean(path)))
	if cleaned == "" || cleaned == "." {
		return false
	}

	base := filepath.Base(cleaned)
	switch base {
	case ".bashrc", ".bash_profile", ".bash_login", ".profile",
		".zshrc", ".zprofile", ".zlogin", ".zshenv", ".cshrc", ".tcshrc",
		"profile.ps1", "microsoft.powershell_profile.ps1",
		"microsoft.vscode_profile.ps1", "profile":
		return true
	}

	segments := splitPathSegments(cleaned)
	if len(segments) == 0 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		for i := 0; i+2 < len(segments); i++ {
			if segments[i] == "documents" && segments[i+1] == "windowspowershell" && strings.HasSuffix(base, ".ps1") {
				return true
			}
			if segments[i] == "documents" && segments[i+1] == "powershell" && strings.HasSuffix(base, ".ps1") {
				return true
			}
		}
		return false
	}
	for i := 0; i+2 < len(segments); i++ {
		if segments[i] == ".config" && segments[i+1] == "fish" && base == "config.fish" {
			return true
		}
	}
	return false
}

// isSystemProtectedPath 判定路径是否命中系统受保护目录，命中后必须保持硬拒绝。
func isSystemProtectedPath(path string) bool {
	return isSystemProtectedPathForOS(path, runtime.GOOS)
}

// isSystemProtectedPathForOS 按指定操作系统判定路径是否命中系统受保护目录。
func isSystemProtectedPathForOS(path string, goos string) bool {
	normalized := strings.ToLower(filepath.Clean(path))
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		volume := strings.ToLower(filepath.VolumeName(normalized))
		if volume == "" && len(normalized) >= 2 && normalized[1] == ':' {
			volume = normalized[:2]
		}
		rest := strings.TrimPrefix(normalized, volume)
		rest = strings.TrimLeft(rest, `\/`)
		if rest == "" {
			return true
		}
		segments := splitPathSegments(rest)
		switch segments[0] {
		case "windows", "program files", "program files (x86)", "programdata",
			"$recycle.bin", "system volume information", "recovery", "boot":
			return true
		}
		if len(segments) >= 3 && segments[0] == "users" && segments[2] == "appdata" {
			return true
		}
	} else {
		trimmed := strings.TrimLeft(normalized, "/")
		segments := splitPathSegments(trimmed)
		if len(segments) == 0 {
			return true
		}
		switch segments[0] {
		case "etc", "bin", "sbin", "usr", "var", "lib", "lib64", "boot", "proc", "sys", "dev", "run", "root":
			return true
		}
	}

	for _, segment := range splitPathSegments(normalized) {
		if segment == ".ssh" {
			return true
		}
	}
	return false
}

// isHighRiskExecutableExtension 识别高风险可执行文件后缀，命中后不走审批放行链路。
func isHighRiskExecutableExtension(extension string) bool {
	switch strings.ToLower(strings.TrimSpace(extension)) {
	case ".exe", ".dll", ".sys", ".bat", ".cmd", ".com", ".scr", ".msi", ".reg":
		return true
	default:
		return false
	}
}

// splitPathSegments 把路径按目录分隔符拆成稳定片段，忽略空片段。
func splitPathSegments(path string) []string {
	normalized := strings.ReplaceAll(path, "\\", "/")
	rawSegments := strings.Split(normalized, "/")
	segments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			continue
		}
		segments = append(segments, trimmed)
	}
	return segments
}

// sandboxErrorDetails 生成可回灌给模型的沙箱拒绝详情，便于模型正确感知失败原因。
func sandboxErrorDetails(action security.Action, sandboxErr error) string {
	securityMessage := strings.TrimSpace(errorMessage(sandboxErr))
	if securityMessage == "" {
		securityMessage = "sandbox rejected action"
	}
	if !strings.HasPrefix(strings.ToLower(securityMessage), "security:") {
		securityMessage = "security: " + securityMessage
	}
	parts := []string{
		securityMessage,
	}
	if workdir := strings.TrimSpace(action.Payload.Workdir); workdir != "" {
		parts = append(parts, "workdir: "+workdir)
	}
	if target := strings.TrimSpace(action.Payload.Target); target != "" {
		parts = append(parts, "target: "+target)
	}
	if sandboxTarget := strings.TrimSpace(action.Payload.SandboxTarget); sandboxTarget != "" {
		parts = append(parts, "sandbox_target: "+sandboxTarget)
	}
	return strings.Join(parts, "\n")
}

// errorMessage 提取错误文本，统一处理 nil 输入避免重复分支。
func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// verifyCapabilityToken 校验 capability token 的签名、绑定关系与时效性。
func (m *DefaultManager) verifyCapabilityToken(action security.Action) error {
	token := action.Payload.CapabilityToken
	if token == nil {
		return nil
	}
	signer := m.capabilitySignerSnapshot()
	if signer == nil {
		return errors.New("capability signer is unavailable")
	}
	if err := signer.Verify(*token); err != nil {
		return fmt.Errorf("invalid capability token signature: %w", err)
	}

	normalized := token.Normalize()
	taskID := strings.TrimSpace(action.Payload.TaskID)
	if taskID == "" {
		return errors.New("capability token requires non-empty action task_id")
	}
	if normalized.TaskID != taskID {
		return errors.New("capability token task_id does not match action")
	}
	agentID := strings.TrimSpace(action.Payload.AgentID)
	if agentID == "" {
		return errors.New("capability token requires non-empty action agent_id")
	}
	if normalized.AgentID != agentID {
		return errors.New("capability token agent_id does not match action")
	}
	if err := normalized.ValidateAt(time.Now().UTC()); err != nil {
		return fmt.Errorf("invalid capability token: %w", err)
	}
	return nil
}

// capabilityDenyDecision 构造 capability 拒绝时统一的权限结果结构。
func capabilityDenyDecision(action security.Action, reason string) security.CheckResult {
	trimmedReason := strings.TrimSpace(reason)
	if trimmedReason == "" {
		trimmedReason = "capability token denied"
	}
	return security.CheckResult{
		Decision: security.DecisionDeny,
		Action:   action,
		Rule: &security.Rule{
			ID:       security.CapabilityRuleID,
			Type:     action.Type,
			Resource: action.Payload.Resource,
			Decision: security.DecisionDeny,
			Reason:   trimmedReason,
		},
		Reason: trimmedReason,
	}
}

// auditCapabilityDecision 记录 capability 的 allow/deny 决策日志，便于追踪任务权限收敛。
func (m *DefaultManager) auditCapabilityDecision(action security.Action, decision string, reason string) {
	if action.Payload.CapabilityToken == nil {
		return
	}
	taskID := strings.TrimSpace(action.Payload.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(action.Payload.CapabilityToken.TaskID)
	}
	agentID := strings.TrimSpace(action.Payload.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(action.Payload.CapabilityToken.AgentID)
	}
	log.Printf(
		"tools capability audit: decision=%s task_id=%s agent_id=%s tool=%s reason=%s",
		strings.TrimSpace(decision),
		taskID,
		agentID,
		strings.TrimSpace(action.Payload.ToolName),
		strings.TrimSpace(reason),
	)
}

// RememberSessionDecision 记录会话内权限记忆，用于后续同类 action 快速决策。
func (m *DefaultManager) RememberSessionDecision(sessionID string, action security.Action, scope SessionPermissionScope) error {
	if m == nil {
		return errors.New("tools: manager is nil")
	}
	if m.sessionDecisions == nil {
		m.sessionDecisions = newSessionPermissionMemory()
	}
	return m.sessionDecisions.remember(sessionID, action, scope)
}

func blockedToolResult(input ToolCallInput, decision security.CheckResult) ToolResult {
	reason := "permission denied"
	if decision.Decision == security.DecisionAsk {
		reason = "permission approval required"
	}
	if strings.TrimSpace(decision.Reason) != "" {
		reason = strings.TrimSpace(decision.Reason)
	}

	result := NewErrorResult(input.Name, reason, permissionDetails(decision), permissionMetadata(decision))
	result.ToolCallID = input.ID
	return result
}

func permissionErrorFromDecision(decision security.CheckResult) error {
	ruleID := ""
	if decision.Rule != nil {
		ruleID = decision.Rule.ID
	}
	return &PermissionDecisionError{
		decision: decision.Decision,
		toolName: decision.Action.Payload.ToolName,
		action:   decision.Action,
		reason:   decision.Reason,
		ruleID:   ruleID,
		scope:    extractRememberScope(decision),
	}
}

// extractRememberScope 从决策规则中提取会话记忆范围。
func extractRememberScope(decision security.CheckResult) SessionPermissionScope {
	if decision.Rule == nil {
		return ""
	}
	ruleID := strings.TrimSpace(decision.Rule.ID)
	switch ruleID {
	case "session-memory:" + string(SessionPermissionScopeOnce):
		return SessionPermissionScopeOnce
	case "session-memory:" + string(SessionPermissionScopeAlways):
		return SessionPermissionScopeAlways
	case "session-memory:" + string(SessionPermissionScopeReject):
		return SessionPermissionScopeReject
	default:
		return ""
	}
}

// sessionDecisionReason 生成会话记忆命中的统一原因文本。
func sessionDecisionReason(scope SessionPermissionScope) string {
	switch scope {
	case SessionPermissionScopeOnce:
		return "session permission remembered: once"
	case SessionPermissionScopeAlways:
		return "session permission remembered: always(session)"
	case SessionPermissionScopeReject:
		return "session permission remembered: reject"
	default:
		return "session permission remembered"
	}
}

func permissionMetadata(decision security.CheckResult) map[string]any {
	metadata := actionMetadata(decision.Action)
	metadata["permission_decision"] = string(decision.Decision)
	if decision.Rule != nil && strings.TrimSpace(decision.Rule.ID) != "" {
		metadata["permission_rule_id"] = decision.Rule.ID
	}
	return metadata
}

func actionMetadata(action security.Action) map[string]any {
	metadata := map[string]any{
		"permission_action_type": string(action.Type),
		"permission_resource":    action.Payload.Resource,
		"permission_operation":   action.Payload.Operation,
	}
	if action.Payload.TargetType != "" {
		metadata["permission_target_type"] = string(action.Payload.TargetType)
	}
	if action.Payload.Target != "" {
		metadata["permission_target"] = action.Payload.Target
	}
	if action.Payload.SandboxTargetType != "" {
		metadata["permission_sandbox_target_type"] = string(action.Payload.SandboxTargetType)
	}
	if action.Payload.SandboxTarget != "" {
		metadata["permission_sandbox_target"] = action.Payload.SandboxTarget
	}
	if semanticType := strings.TrimSpace(action.Payload.SemanticType); semanticType != "" {
		metadata["permission_semantic_type"] = semanticType
	}
	if semanticClass := strings.TrimSpace(action.Payload.SemanticClass); semanticClass != "" {
		metadata["permission_semantic_class"] = semanticClass
	}
	if normalizedIntent := strings.TrimSpace(action.Payload.NormalizedIntent); normalizedIntent != "" {
		metadata["permission_normalized_intent"] = normalizedIntent
	}
	if fingerprint := strings.TrimSpace(action.Payload.PermissionFingerprint); fingerprint != "" {
		metadata["permission_fingerprint"] = fingerprint
	}
	if strings.TrimSpace(action.Payload.TaskID) != "" {
		metadata["permission_task_id"] = strings.TrimSpace(action.Payload.TaskID)
	}
	if strings.TrimSpace(action.Payload.AgentID) != "" {
		metadata["permission_agent_id"] = strings.TrimSpace(action.Payload.AgentID)
	}
	return metadata
}

func permissionDetails(decision security.CheckResult) string {
	parts := make([]string, 0, 5)
	parts = append(parts, "type: "+string(decision.Action.Type))
	if strings.TrimSpace(decision.Action.Payload.Resource) != "" {
		parts = append(parts, "resource: "+decision.Action.Payload.Resource)
	}
	if strings.TrimSpace(decision.Action.Payload.Operation) != "" {
		parts = append(parts, "operation: "+decision.Action.Payload.Operation)
	}
	if decision.Action.Payload.TargetType != "" && strings.TrimSpace(decision.Action.Payload.Target) != "" {
		parts = append(parts, fmt.Sprintf("%s: %s", decision.Action.Payload.TargetType, decision.Action.Payload.Target))
	}
	if semanticType := strings.TrimSpace(decision.Action.Payload.SemanticType); semanticType != "" {
		parts = append(parts, "semantic_type: "+semanticType)
	}
	if semanticClass := strings.TrimSpace(decision.Action.Payload.SemanticClass); semanticClass != "" {
		parts = append(parts, "semantic_class: "+semanticClass)
	}
	if normalizedIntent := strings.TrimSpace(decision.Action.Payload.NormalizedIntent); normalizedIntent != "" {
		parts = append(parts, "normalized_intent: "+normalizedIntent)
	}
	if fingerprint := strings.TrimSpace(decision.Action.Payload.PermissionFingerprint); fingerprint != "" {
		parts = append(parts, "permission_fingerprint: "+fingerprint)
	}
	if strings.TrimSpace(decision.Reason) != "" {
		parts = append(parts, "policy: "+strings.TrimSpace(decision.Reason))
	}
	return strings.Join(parts, "\n")
}
