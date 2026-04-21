package runtime

import (
	"context"
	"fmt"
	"path/filepath"
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
	)
	if err != nil {
		return tools.SubAgentRunResult{}, err
	}

	result, err := i.service.RunSubAgentTask(ctx, SubAgentTaskInput{
		RunID:     runID,
		SessionID: sessionID,
		AgentID:   callerID,
		Role:      role,
		Task: subagent.Task{
			ID:             taskID,
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
) (subagent.Capability, error) {
	requestedTools = normalizeAllowlistToList(requestedTools)
	requestedPaths = normalizePathAllowlist(requestedPaths)
	if parent == nil {
		return subagent.Capability{
			AllowedTools: requestedTools,
			AllowedPaths: requestedPaths,
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
		AllowedTools: toolsAllowed,
		AllowedPaths: pathsAllowed,
	}, nil
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
	targetClean := filepath.Clean(strings.TrimSpace(target))
	if targetClean == "" || targetClean == "." {
		return false
	}
	for _, root := range allowlist {
		rootClean := filepath.Clean(strings.TrimSpace(root))
		if rootClean == "" || rootClean == "." {
			continue
		}
		if targetClean == rootClean {
			return true
		}
		prefix := rootClean + string(filepath.Separator)
		if strings.HasPrefix(targetClean, prefix) {
			return true
		}
		// Windows 场景下 separator 可能混用，补充统一前缀判定。
		altPrefix := rootClean + "/"
		if strings.HasPrefix(targetClean, altPrefix) {
			return true
		}
	}
	return false
}
