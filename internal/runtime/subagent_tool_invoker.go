package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"neo-code/internal/security"
	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

// runtimeSubAgentInvoker 复用 runtime.RunSubAgentTask，为工具层提供即时子代理执行能力。
type runtimeSubAgentInvoker struct {
	service    *Service
	runID      string
	sessionID  string
	callerID   string
	defaultDir string
}

// newRuntimeSubAgentInvoker 构造绑定当前运行上下文的子代理调用桥接器。
func newRuntimeSubAgentInvoker(
	service *Service,
	runID string,
	sessionID string,
	callerID string,
	workdir string,
) tools.SubAgentInvoker {
	if service == nil {
		return nil
	}
	return runtimeSubAgentInvoker{
		service:    service,
		runID:      strings.TrimSpace(runID),
		sessionID:  strings.TrimSpace(sessionID),
		callerID:   strings.TrimSpace(callerID),
		defaultDir: strings.TrimSpace(workdir),
	}
}

// Run 调用 runtime 子代理执行链路，并把结果映射为工具层统一结构。
func (i runtimeSubAgentInvoker) Run(ctx context.Context, input tools.SubAgentRunInput) (tools.SubAgentRunResult, error) {
	role := input.Role
	if !role.Valid() {
		role = subagent.RoleCoder
	}
	taskType := input.TaskType
	if strings.TrimSpace(string(taskType)) == "" {
		taskType = subagent.TaskTypeReview
	}

	taskID := strings.TrimSpace(input.TaskID)
	if taskID == "" {
		taskID = "spawn-subagent-inline"
	}
	workdir := strings.TrimSpace(input.Workdir)
	if workdir == "" {
		workdir = i.defaultDir
	}

	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = i.runID
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID = i.sessionID
	}
	callerID := strings.TrimSpace(input.CallerAgent)
	if callerID == "" {
		callerID = i.callerID
	}
	capability, err := resolveInlineSubAgentCapability(
		input.ParentCapabilityToken,
		input.AllowedTools,
		input.AllowedPaths,
		input.ToolUseMode,
		workdir,
	)
	if err != nil {
		return tools.SubAgentRunResult{}, err
	}

	runCtx := ctx
	cancel := func() {}
	if input.Timeout > 0 {
		// 若上层已设置 deadline（例如诊断链路额外 grace window），避免在这里再次收窄上下文超时。
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			runCtx, cancel = context.WithTimeout(ctx, input.Timeout)
		}
	}
	defer cancel()

	result, err := i.service.RunSubAgentTask(runCtx, SubAgentTaskInput{
		RunID:     runID,
		SessionID: sessionID,
		AgentID:   callerID,
		Role:      role,
		Task: subagent.Task{
			ID:             taskID,
			TaskType:       taskType,
			Goal:           strings.TrimSpace(input.Goal),
			ExpectedOutput: strings.TrimSpace(input.ExpectedOut),
			Workspace:      workdir,
		},
		Budget: subagent.Budget{
			MaxSteps: input.MaxSteps,
			Timeout:  input.Timeout,
		},
		Capability: capability,
	})

	return tools.SubAgentRunResult{
		Role:       result.Role,
		TaskID:     result.TaskID,
		State:      result.State,
		StopReason: result.StopReason,
		StepCount:  result.StepCount,
		Output:     result.Output,
		Error:      strings.TrimSpace(result.Error),
	}, err
}

// resolveInlineSubAgentCapability 将子代理请求能力与父 capability 做收敛，避免 inline 执行权限放大。
func resolveInlineSubAgentCapability(
	parent *security.CapabilityToken,
	requestedTools []string,
	requestedPaths []string,
	toolUseMode subagent.ToolUseMode,
	workdir string,
) (subagent.Capability, error) {
	requestedTools = normalizeAllowlistToList(requestedTools)
	requestedPaths = normalizeRequestedPathsWithWorkdir(requestedPaths, workdir)
	normalizedToolUseMode := subagent.ToolUseMode(strings.ToLower(strings.TrimSpace(string(toolUseMode))))
	if normalizedToolUseMode != "" && !normalizedToolUseMode.Valid() {
		return subagent.Capability{}, fmt.Errorf("runtime: inline subagent tool use mode %q is invalid", toolUseMode)
	}
	if parent == nil {
		return subagent.Capability{
			AllowedTools: requestedTools,
			AllowedPaths: requestedPaths,
			ToolUseMode:  normalizedToolUseMode,
		}, nil
	}

	parentToken := parent.Normalize()
	parentTools := normalizeAllowlistToList(parentToken.AllowedTools)
	toolsAllowed := intersectAllowedTools(parentTools, requestedTools)
	if len(toolsAllowed) == 0 {
		return subagent.Capability{}, fmt.Errorf("runtime: inline subagent requested tools exceed parent capability")
	}

	pathsAllowed, err := intersectAllowedPaths(parentToken.AllowedPaths, requestedPaths)
	if err != nil {
		return subagent.Capability{}, err
	}
	return subagent.Capability{
		AllowedTools:    toolsAllowed,
		AllowedPaths:    pathsAllowed,
		ToolUseMode:     normalizedToolUseMode,
		CapabilityToken: &parentToken,
	}, nil
}

// normalizeRequestedPathsWithWorkdir 在运行时 workdir 上下文中规整路径，避免相对路径与父 capability 的绝对路径比较失配。
func normalizeRequestedPathsWithWorkdir(paths []string, workdir string) []string {
	base := strings.TrimSpace(workdir)
	normalized := make([]string, 0, len(paths))
	for _, item := range paths {
		path := strings.TrimSpace(item)
		if path == "" {
			continue
		}
		if !isAbsolutePathLike(path) && base != "" {
			path = filepath.Join(base, path)
		}
		path = filepath.Clean(path)
		if filepath.IsAbs(path) {
			if absolute, err := filepath.Abs(path); err == nil {
				path = absolute
			}
		}
		normalized = append(normalized, path)
	}
	return normalizePathAllowlist(normalized)
}

// isAbsolutePathLike 判断路径是否已经具备绝对语义（含类 Unix 根路径），避免误把 /x 当相对路径拼接到 workdir。
func isAbsolutePathLike(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	if filepath.IsAbs(trimmed) {
		return true
	}
	return strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, `\`)
}

// intersectAllowedTools 在父能力范围内收敛 requested 工具；未显式请求时默认继承父能力。
func intersectAllowedTools(parent []string, requested []string) []string {
	parent = normalizeAllowlistToList(parent)
	requested = normalizeAllowlistToList(requested)
	if len(parent) == 0 {
		return requested
	}
	if len(requested) == 0 {
		return append([]string(nil), parent...)
	}
	allowedSet := make(map[string]struct{}, len(parent))
	for _, toolName := range parent {
		allowedSet[strings.ToLower(strings.TrimSpace(toolName))] = struct{}{}
	}
	out := make([]string, 0, len(requested))
	for _, toolName := range requested {
		normalized := strings.ToLower(strings.TrimSpace(toolName))
		if _, ok := allowedSet[normalized]; !ok {
			continue
		}
		out = append(out, normalized)
	}
	return normalizeAllowlistToList(out)
}

// intersectAllowedPaths 在父路径边界内收敛 requested 路径；未显式请求时默认继承父路径。
func intersectAllowedPaths(parent []string, requested []string) ([]string, error) {
	parent = normalizePathAllowlist(parent)
	requested = normalizePathAllowlist(requested)
	if len(parent) == 0 {
		return requested, nil
	}
	if len(requested) == 0 {
		return append([]string(nil), parent...), nil
	}

	out := make([]string, 0, len(requested))
	for _, path := range requested {
		if pathCoveredByAllowlist(path, parent) {
			out = append(out, path)
		}
	}
	out = normalizePathAllowlist(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("runtime: inline subagent requested paths exceed parent capability")
	}
	return out, nil
}

// pathCoveredByAllowlist 判断路径是否落在 allowlist 任一根路径范围内。
func pathCoveredByAllowlist(target string, allowlist []string) bool {
	targetKey := normalizeAllowPathKey(target)
	if targetKey == "" || targetKey == "." {
		return false
	}
	for _, root := range allowlist {
		rootKey := normalizeAllowPathKey(root)
		if rootKey == "" || rootKey == "." {
			continue
		}
		if targetKey == rootKey {
			return true
		}
		if strings.HasPrefix(targetKey, rootKey+"/") {
			return true
		}
	}
	return false
}

// normalizeAllowPathKey 统一路径比较键，屏蔽分隔符差异并在 Windows 下忽略大小写。
func normalizeAllowPathKey(path string) string {
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
