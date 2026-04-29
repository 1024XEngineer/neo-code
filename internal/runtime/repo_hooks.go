package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"neo-code/internal/config"
	runtimehooks "neo-code/internal/runtime/hooks"
)

const (
	repoHooksRelativePath          = ".neocode/hooks.yaml"
	repoHooksTrustStoreFileName    = "trusted-workspaces.json"
	repoHooksTrustStoreVersion     = 1
	repoHookScopeValue             = "repo"
	repoHookKindBuiltIn            = "builtin"
	repoHookModeSync               = "sync"
	repoHookFailurePolicyWarnOnly  = "warn_only"
	repoHookFailurePolicyFailOpen  = "fail_open"
	repoHookFailurePolicyFailClose = "fail_closed"
)

// repoHooksConfigFile 描述仓库 hooks 配置文件结构。
type repoHooksConfigFile struct {
	Hooks repoHooksSection `yaml:"hooks"`
}

// repoHooksSection 收敛仓库 hooks 列表。
type repoHooksSection struct {
	Items []config.RuntimeHookItemConfig `yaml:"items"`
}

// trustedWorkspaceStore 描述 trusted-workspaces.json 的数据结构。
type trustedWorkspaceStore struct {
	Version    int      `json:"version"`
	Workspaces []string `json:"workspaces"`
}

// trustDecision 封装 trust gate 判定结果与诊断信息。
type trustDecision struct {
	Trusted        bool
	TrustStorePath string
	InvalidReason  string
}

// dynamicRepoHookExecutor 按运行时 workdir 惰性解析 repo hooks，避免绑定启动时 cfg.Workdir。
type dynamicRepoHookExecutor struct {
	service         *Service
	hooksCfg        config.RuntimeHooksConfig
	fallbackWorkdir string

	mu    sync.RWMutex
	cache map[string]HookExecutor
}

// buildUserHookExecutor 根据 runtime.hooks.items 构建 user hooks 执行器。
func buildUserHookExecutor(
	service *Service,
	cfg config.Config,
	hooksCfg config.RuntimeHooksConfig,
) (HookExecutor, error) {
	if !hooksCfg.IsUserHooksEnabled() {
		return nil, nil
	}
	registry := runtimehooks.NewRegistry()
	registered := 0
	for index, item := range hooksCfg.Items {
		if !item.IsEnabled() {
			continue
		}
		spec, err := buildUserHookSpec(item, cfg.Workdir)
		if err != nil {
			return nil, fmt.Errorf("runtime.hooks.items[%d]: %w", index, err)
		}
		if err := registry.Register(spec); err != nil {
			return nil, fmt.Errorf("runtime.hooks.items[%d]: %w", index, err)
		}
		registered++
	}
	if registered == 0 {
		return nil, nil
	}
	return runtimehooks.NewExecutor(
		registry,
		newHookRuntimeEventEmitter(service),
		time.Duration(hooksCfg.DefaultTimeoutSec)*time.Second,
	), nil
}

// buildRepoHookExecutor 在 workspace 受信任时构建 repo hooks 执行器。
func buildRepoHookExecutor(
	service *Service,
	cfg config.Config,
	hooksCfg config.RuntimeHooksConfig,
) (HookExecutor, error) {
	return &dynamicRepoHookExecutor{
		service:         service,
		hooksCfg:        hooksCfg,
		fallbackWorkdir: strings.TrimSpace(cfg.Workdir),
		cache:           make(map[string]HookExecutor),
	}, nil
}

// Run 在每个 hook point 调用时按输入 workdir 解析对应 workspace 的 repo hooks 执行器。
func (e *dynamicRepoHookExecutor) Run(
	ctx context.Context,
	point runtimehooks.HookPoint,
	input runtimehooks.HookContext,
) runtimehooks.RunOutput {
	if e == nil {
		return runtimehooks.RunOutput{}
	}
	workspace := resolveHookWorkdir(input, e.fallbackWorkdir)
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return runtimehooks.RunOutput{}
	}
	normalizedWorkspace, err := normalizeTrustedWorkspacePath(workspace)
	if err != nil {
		return runtimehooks.RunOutput{}
	}

	e.mu.RLock()
	executor, ok := e.cache[normalizedWorkspace]
	e.mu.RUnlock()
	if ok {
		return runHookExecutorSafely(executor, ctx, point, input)
	}

	loaded, loadErr := buildRepoHookExecutorForWorkspace(e.service, workspace, e.hooksCfg)
	if loadErr != nil {
		return runtimehooks.RunOutput{}
	}
	e.mu.Lock()
	e.cache[normalizedWorkspace] = loaded
	e.mu.Unlock()
	return runHookExecutorSafely(loaded, ctx, point, input)
}

// buildRepoHookExecutorForWorkspace 在指定 workspace 受信任时构建 repo hooks 执行器。
func buildRepoHookExecutorForWorkspace(
	service *Service,
	workspace string,
	hooksCfg config.RuntimeHooksConfig,
) (HookExecutor, error) {
	hooksPath, found, err := resolveRepoHooksPath(workspace)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	emitRepoHooksLifecycleEvent(service, EventRepoHooksDiscovered, RepoHooksLifecyclePayload{
		Workspace: workspace,
		HooksPath: hooksPath,
	})

	trust := evaluateWorkspaceTrust(workspace)
	if strings.TrimSpace(trust.InvalidReason) != "" {
		emitRepoHooksTrustStoreInvalidEvent(service, RepoHooksTrustStoreInvalidPayload{
			TrustStorePath: trust.TrustStorePath,
			Reason:         trust.InvalidReason,
		})
	}
	if !trust.Trusted {
		emitRepoHooksLifecycleEvent(service, EventRepoHooksSkippedUntrusted, RepoHooksLifecyclePayload{
			Workspace:      workspace,
			HooksPath:      hooksPath,
			TrustStorePath: trust.TrustStorePath,
			Reason:         coalesceHookMessage(trust.InvalidReason, "workspace is not trusted"),
		})
		return nil, nil
	}

	items, err := loadRepoHookItems(hooksPath, hooksCfg)
	if err != nil {
		return nil, fmt.Errorf("load repo hooks: %w", err)
	}
	if len(items) == 0 {
		emitRepoHooksLifecycleEvent(service, EventRepoHooksLoaded, RepoHooksLifecyclePayload{
			Workspace:      workspace,
			HooksPath:      hooksPath,
			TrustStorePath: trust.TrustStorePath,
			HookCount:      0,
		})
		return nil, nil
	}

	registry := runtimehooks.NewRegistry()
	for index, item := range items {
		spec, err := buildRepoHookSpec(item, workspace)
		if err != nil {
			return nil, fmt.Errorf("%s: hooks.items[%d]: %w", hooksPath, index, err)
		}
		if err := registry.Register(spec); err != nil {
			return nil, fmt.Errorf("%s: hooks.items[%d]: %w", hooksPath, index, err)
		}
	}

	emitRepoHooksLifecycleEvent(service, EventRepoHooksLoaded, RepoHooksLifecyclePayload{
		Workspace:      workspace,
		HooksPath:      hooksPath,
		TrustStorePath: trust.TrustStorePath,
		HookCount:      len(items),
	})
	return runtimehooks.NewExecutor(
		registry,
		newHookRuntimeEventEmitter(service),
		time.Duration(hooksCfg.DefaultTimeoutSec)*time.Second,
	), nil
}

// resolveRepoHooksPath 解析 workspace 下 repo hooks 文件并判断是否存在。
func resolveRepoHooksPath(workspace string) (string, bool, error) {
	base := strings.TrimSpace(workspace)
	if base == "" {
		return "", false, nil
	}
	resolvedWorkspace, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return "", false, fmt.Errorf("resolve workspace: %w", err)
	}
	hooksPath := filepath.Join(resolvedWorkspace, repoHooksRelativePath)
	info, err := os.Stat(hooksPath)
	if os.IsNotExist(err) {
		return hooksPath, false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("stat repo hooks: %w", err)
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("repo hooks path is a directory: %s", hooksPath)
	}
	return hooksPath, true, nil
}

// loadRepoHookItems 读取并校验 repo hooks 文件，输出可执行 item 列表。
func loadRepoHookItems(path string, defaults config.RuntimeHooksConfig) ([]config.RuntimeHookItemConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read repo hooks file: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("repo hooks file is empty")
	}

	var file repoHooksConfigFile
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("parse repo hooks yaml: %w", err)
	}

	items := make([]config.RuntimeHookItemConfig, 0, len(file.Hooks.Items))
	seen := make(map[string]struct{}, len(file.Hooks.Items))
	for index := range file.Hooks.Items {
		item := file.Hooks.Items[index].Clone()
		applyRepoHookItemDefaults(&item, defaults)
		if !item.IsEnabled() {
			continue
		}
		if err := validateRepoHookItem(item); err != nil {
			return nil, fmt.Errorf("hooks.items[%d]: %w", index, err)
		}
		normalizedID := strings.ToLower(strings.TrimSpace(item.ID))
		if _, exists := seen[normalizedID]; exists {
			return nil, fmt.Errorf("hooks.items[%d].id duplicates %q", index, item.ID)
		}
		seen[normalizedID] = struct{}{}
		items = append(items, item)
	}
	return items, nil
}

// applyRepoHookItemDefaults 为 repo hook item 注入默认值并锁定 scope/kind/mode 约束。
func applyRepoHookItemDefaults(item *config.RuntimeHookItemConfig, defaults config.RuntimeHooksConfig) {
	if item == nil {
		return
	}
	if item.Enabled == nil {
		item.Enabled = repoHookBoolPtr(true)
	}
	if strings.TrimSpace(item.Scope) == "" {
		item.Scope = repoHookScopeValue
	}
	if strings.TrimSpace(item.Kind) == "" {
		item.Kind = repoHookKindBuiltIn
	}
	if strings.TrimSpace(item.Mode) == "" {
		item.Mode = repoHookModeSync
	}
	if item.TimeoutSec <= 0 {
		item.TimeoutSec = defaults.DefaultTimeoutSec
	}
	if strings.TrimSpace(item.FailurePolicy) == "" {
		item.FailurePolicy = defaults.DefaultFailurePolicy
	}
}

// validateRepoHookItem 校验 repo hook item 是否满足 P3 限定能力范围。
func validateRepoHookItem(item config.RuntimeHookItemConfig) error {
	if strings.TrimSpace(item.ID) == "" {
		return fmt.Errorf("id is required")
	}
	point := strings.ToLower(strings.TrimSpace(item.Point))
	switch point {
	case string(runtimehooks.HookPointBeforeToolCall),
		string(runtimehooks.HookPointAfterToolResult),
		string(runtimehooks.HookPointBeforeCompletionDecision),
		string(runtimehooks.HookPointBeforePermissionDecision),
		string(runtimehooks.HookPointAfterToolFailure),
		string(runtimehooks.HookPointSessionStart),
		string(runtimehooks.HookPointSessionEnd),
		string(runtimehooks.HookPointUserPromptSubmit),
		string(runtimehooks.HookPointPreCompact),
		string(runtimehooks.HookPointPostCompact),
		string(runtimehooks.HookPointSubAgentStart),
		string(runtimehooks.HookPointSubAgentStop):
	default:
		return fmt.Errorf("point %q is not supported", item.Point)
	}
	if capability, ok := runtimehooks.HookPointCapabilities(runtimehooks.HookPoint(point)); ok && !capability.UserAllowed {
		return fmt.Errorf("point %q does not allow repo hooks", item.Point)
	}
	if strings.ToLower(strings.TrimSpace(item.Scope)) != repoHookScopeValue {
		return fmt.Errorf("scope %q is not supported", item.Scope)
	}
	if strings.ToLower(strings.TrimSpace(item.Kind)) != repoHookKindBuiltIn {
		return fmt.Errorf("kind %q is not supported", item.Kind)
	}
	if strings.ToLower(strings.TrimSpace(item.Mode)) != repoHookModeSync {
		return fmt.Errorf("mode %q is not supported", item.Mode)
	}
	if item.TimeoutSec <= 0 {
		return fmt.Errorf("timeout_sec must be greater than 0")
	}
	switch strings.ToLower(strings.TrimSpace(item.FailurePolicy)) {
	case repoHookFailurePolicyWarnOnly, repoHookFailurePolicyFailOpen, repoHookFailurePolicyFailClose:
	default:
		return fmt.Errorf("failure_policy %q is invalid", item.FailurePolicy)
	}
	handler := strings.ToLower(strings.TrimSpace(item.Handler))
	switch handler {
	case "require_file_exists", "warn_on_tool_call", "add_context_note":
	default:
		return fmt.Errorf("handler %q is not supported", item.Handler)
	}
	if handler == "warn_on_tool_call" && !runtimeHasWarnOnToolCallTargets(item.Params) {
		return fmt.Errorf("handler %q requires params.tool_name or params.tool_names", item.Handler)
	}
	return nil
}

// runtimeHasWarnOnToolCallTargets 判断 warn_on_tool_call 是否配置了至少一个目标工具。
func runtimeHasWarnOnToolCallTargets(params map[string]any) bool {
	if len(params) == 0 {
		return false
	}
	if name := strings.TrimSpace(readHookParamString(params, "tool_name")); name != "" {
		return true
	}
	for _, value := range readHookParamStringSlice(params, "tool_names") {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

// evaluateWorkspaceTrust 根据 trust store 判断 workspace 是否可信并附带容错诊断。
func evaluateWorkspaceTrust(workspace string) trustDecision {
	storePath := resolveTrustedWorkspacesPath()
	targetPath, err := normalizeTrustedWorkspacePath(workspace)
	if err != nil {
		return trustDecision{
			Trusted:        false,
			TrustStorePath: storePath,
			InvalidReason:  fmt.Sprintf("invalid workspace path: %v", err),
		}
	}

	raw, err := os.ReadFile(storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return trustDecision{
				Trusted:        false,
				TrustStorePath: storePath,
				InvalidReason:  "trust store is missing",
			}
		}
		return trustDecision{
			Trusted:        false,
			TrustStorePath: storePath,
			InvalidReason:  fmt.Sprintf("read trust store failed: %v", err),
		}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return trustDecision{
			Trusted:        false,
			TrustStorePath: storePath,
			InvalidReason:  "trust store is empty",
		}
	}

	var store trustedWorkspaceStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return trustDecision{
			Trusted:        false,
			TrustStorePath: storePath,
			InvalidReason:  fmt.Sprintf("trust store json is invalid: %v", err),
		}
	}
	if store.Version != repoHooksTrustStoreVersion {
		return trustDecision{
			Trusted:        false,
			TrustStorePath: storePath,
			InvalidReason:  fmt.Sprintf("trust store version must be %d", repoHooksTrustStoreVersion),
		}
	}
	if store.Workspaces == nil {
		return trustDecision{
			Trusted:        false,
			TrustStorePath: storePath,
			InvalidReason:  "trust store workspaces field is required",
		}
	}

	for _, entry := range store.Workspaces {
		normalizedEntry, normalizeErr := normalizeTrustedWorkspacePath(entry)
		if normalizeErr != nil {
			return trustDecision{
				Trusted:        false,
				TrustStorePath: storePath,
				InvalidReason:  fmt.Sprintf("trust store workspace path is invalid: %v", normalizeErr),
			}
		}
		if normalizedEntry == targetPath {
			return trustDecision{
				Trusted:        true,
				TrustStorePath: storePath,
			}
		}
	}
	return trustDecision{
		Trusted:        false,
		TrustStorePath: storePath,
	}
}

// resolveTrustedWorkspacesPath 返回固定 trust store 文件路径 ~/.neocode/trusted-workspaces.json。
func resolveTrustedWorkspacesPath() string {
	homeDir := strings.TrimSpace(os.Getenv("HOME"))
	if !filepath.IsAbs(homeDir) {
		resolved, err := os.UserHomeDir()
		if err == nil {
			homeDir = strings.TrimSpace(resolved)
		}
	}
	if homeDir == "" {
		homeDir = "."
	}
	return filepath.Join(homeDir, ".neocode", repoHooksTrustStoreFileName)
}

// normalizeTrustedWorkspacePath 统一 canonical 路径形式用于 trust 匹配。
func normalizeTrustedWorkspacePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path is empty")
	}
	if !filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("path must be absolute")
	}
	resolved, err := filepath.Abs(filepath.Clean(trimmed))
	if err != nil {
		return "", err
	}
	return normalizeHookComparablePath(resolved), nil
}

// emitRepoHooksLifecycleEvent 发出 repo hooks 生命周期事件，失败时保持 best-effort。
func emitRepoHooksLifecycleEvent(service *Service, eventType EventType, payload RepoHooksLifecyclePayload) {
	if service == nil {
		return
	}
	_ = service.emit(context.Background(), eventType, "", "", payload)
}

// emitRepoHooksTrustStoreInvalidEvent 发出 trust store 无效事件，失败时保持 best-effort。
func emitRepoHooksTrustStoreInvalidEvent(service *Service, payload RepoHooksTrustStoreInvalidPayload) {
	if service == nil {
		return
	}
	_ = service.emit(context.Background(), EventRepoHooksTrustStoreInvalid, "", "", payload)
}

// coalesceHookMessage 返回首个非空诊断文案。
func coalesceHookMessage(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func repoHookBoolPtr(value bool) *bool {
	return &value
}
