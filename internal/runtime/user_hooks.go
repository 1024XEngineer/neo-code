package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"neo-code/internal/config"
	runtimehooks "neo-code/internal/runtime/hooks"
)

// ConfigureRuntimeHooks 根据配置装配 runtime hook 执行器；当 hooks 被关闭时会禁用执行器。
func ConfigureRuntimeHooks(service *Service, cfg config.Config) error {
	return configureRuntimeHooksFromConfig(service, cfg)
}

// configureRuntimeHooksFromConfig 根据全局配置构建并注入 runtime hook 执行器。
func configureRuntimeHooksFromConfig(service *Service, cfg config.Config) error {
	if service == nil {
		return nil
	}
	baseExecutor := unwrapBaseHookExecutor(service.hookExecutor)
	if base, ok := baseExecutor.(*runtimehooks.Executor); ok {
		base.SetAsyncResultSink(newHookAsyncResultSink(service))
	}
	hooksCfg := cfg.Runtime.Hooks.Clone()
	hooksCfg.ApplyDefaults(config.StaticDefaults().Runtime.Hooks)
	if !hooksCfg.IsEnabled() {
		service.SetHookExecutor(nil)
		return nil
	}

	userExecutor, err := buildUserHookExecutor(service, cfg, hooksCfg)
	if err != nil {
		return err
	}
	repoExecutor, err := buildRepoHookExecutor(service, cfg, hooksCfg)
	if err != nil {
		return err
	}
	service.SetHookExecutor(composeRuntimeHookExecutors(baseExecutor, userExecutor, repoExecutor))
	return nil
}

type userComposedHookExecutor struct {
	base HookExecutor
	user HookExecutor
}

func (e *userComposedHookExecutor) Run(
	ctx context.Context,
	point runtimehooks.HookPoint,
	input runtimehooks.HookContext,
) runtimehooks.RunOutput {
	baseOutput := runHookExecutorSafely(e.base, ctx, point, input)
	if baseOutput.Blocked {
		return baseOutput
	}
	userOutput := runHookExecutorSafely(e.user, ctx, point, input)
	if len(baseOutput.Results) == 0 {
		return userOutput
	}
	if len(userOutput.Results) == 0 {
		return baseOutput
	}
	combined := runtimehooks.RunOutput{
		Results: append(append([]runtimehooks.HookResult{}, baseOutput.Results...), userOutput.Results...),
	}
	if userOutput.Blocked {
		combined.Blocked = true
		combined.BlockedBy = userOutput.BlockedBy
		combined.BlockedSource = userOutput.BlockedSource
	}
	return combined
}

func unwrapBaseHookExecutor(executor HookExecutor) HookExecutor {
	if composed, ok := executor.(*userComposedHookExecutor); ok {
		return composed.base
	}
	if composed, ok := executor.(*repoComposedHookExecutor); ok {
		return unwrapBaseHookExecutor(composed.base)
	}
	return executor
}

// repoComposedHookExecutor 将 repo hooks 串联到既有执行链末端，保持 internal -> user -> repo 顺序。
type repoComposedHookExecutor struct {
	base HookExecutor
	repo HookExecutor
}

func (e *repoComposedHookExecutor) Run(
	ctx context.Context,
	point runtimehooks.HookPoint,
	input runtimehooks.HookContext,
) runtimehooks.RunOutput {
	baseOutput := runHookExecutorSafely(e.base, ctx, point, input)
	if baseOutput.Blocked {
		return baseOutput
	}
	repoOutput := runHookExecutorSafely(e.repo, ctx, point, input)
	if len(baseOutput.Results) == 0 {
		return repoOutput
	}
	if len(repoOutput.Results) == 0 {
		return baseOutput
	}
	combined := runtimehooks.RunOutput{
		Results: append(append([]runtimehooks.HookResult{}, baseOutput.Results...), repoOutput.Results...),
	}
	if repoOutput.Blocked {
		combined.Blocked = true
		combined.BlockedBy = repoOutput.BlockedBy
		combined.BlockedSource = repoOutput.BlockedSource
	}
	return combined
}

// composeRuntimeHookExecutors 将 internal/user/repo 三段执行器按固定顺序串联。
func composeRuntimeHookExecutors(base HookExecutor, user HookExecutor, repo HookExecutor) HookExecutor {
	composed := base
	if user != nil {
		composed = &userComposedHookExecutor{base: composed, user: user}
	}
	if repo != nil {
		composed = &repoComposedHookExecutor{base: composed, repo: repo}
	}
	return composed
}

func runHookExecutorSafely(
	executor HookExecutor,
	ctx context.Context,
	point runtimehooks.HookPoint,
	input runtimehooks.HookContext,
) runtimehooks.RunOutput {
	if executor == nil {
		return runtimehooks.RunOutput{}
	}
	return executor.Run(ctx, point, input)
}

// buildUserHookSpec 将 user builtin hook 配置转换为 runtime 可执行 HookSpec。
func buildUserHookSpec(item config.RuntimeHookItemConfig, defaultWorkdir string) (runtimehooks.HookSpec, error) {
	return buildConfiguredHookSpec(
		item,
		defaultWorkdir,
		runtimehooks.HookScopeUser,
		runtimehooks.HookSourceUser,
	)
}

// buildRepoHookSpec 将 repo builtin hook 配置转换为 runtime 可执行 HookSpec。
func buildRepoHookSpec(item config.RuntimeHookItemConfig, defaultWorkdir string) (runtimehooks.HookSpec, error) {
	return buildConfiguredHookSpec(
		item,
		defaultWorkdir,
		runtimehooks.HookScopeRepo,
		runtimehooks.HookSourceRepo,
	)
}

// buildConfiguredHookSpec 按给定 scope/source 构建 builtin hook 执行定义。
func buildConfiguredHookSpec(
	item config.RuntimeHookItemConfig,
	defaultWorkdir string,
	scope runtimehooks.HookScope,
	source runtimehooks.HookSource,
) (runtimehooks.HookSpec, error) {
	if err := validateConfiguredHookItemForP6Lite(item, scope); err != nil {
		return runtimehooks.HookSpec{}, err
	}
	handler, err := buildUserBuiltinHookHandler(strings.TrimSpace(item.Handler), item.Params, defaultWorkdir)
	if err != nil {
		return runtimehooks.HookSpec{}, err
	}
	return runtimehooks.HookSpec{
		ID:            strings.TrimSpace(item.ID),
		Point:         runtimehooks.HookPoint(strings.TrimSpace(item.Point)),
		Scope:         scope,
		Source:        source,
		Kind:          runtimehooks.HookKindFunction,
		Mode:          runtimehooks.HookModeSync,
		Priority:      item.Priority,
		Timeout:       time.Duration(item.TimeoutSec) * time.Second,
		FailurePolicy: mapRuntimeHookFailurePolicy(item.FailurePolicy),
		Handler:       handler,
	}, nil
}

// validateConfiguredHookItemForP6Lite 在 runtime 装配阶段执行兜底校验，防止绕过配置层校验后出现半生效。
func validateConfiguredHookItemForP6Lite(item config.RuntimeHookItemConfig, scope runtimehooks.HookScope) error {
	expectedScope := strings.TrimSpace(string(scope))
	actualScope := strings.ToLower(strings.TrimSpace(item.Scope))
	if actualScope != expectedScope {
		return fmt.Errorf("scope %q is not supported", item.Scope)
	}

	kind := strings.ToLower(strings.TrimSpace(item.Kind))
	if kind != "builtin" {
		if isExternalHookKind(kind) {
			return fmt.Errorf(
				"external hook kind %q is not supported in P6-lite; only builtin hooks are enabled",
				item.Kind,
			)
		}
		return fmt.Errorf("kind %q is not supported", item.Kind)
	}

	mode := strings.ToLower(strings.TrimSpace(item.Mode))
	if mode != "sync" {
		return fmt.Errorf("mode %q is not supported", item.Mode)
	}
	return nil
}

func isExternalHookKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "command", "http", "prompt", "agent":
		return true
	default:
		return false
	}
}

func mapRuntimeHookFailurePolicy(policy string) runtimehooks.FailurePolicy {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "fail_closed":
		return runtimehooks.FailurePolicyFailClosed
	case "warn_only", "fail_open":
		return runtimehooks.FailurePolicyFailOpen
	default:
		return runtimehooks.FailurePolicyFailOpen
	}
}

func buildUserBuiltinHookHandler(
	handlerName string,
	params map[string]any,
	defaultWorkdir string,
) (runtimehooks.HookHandler, error) {
	normalizedHandler := strings.ToLower(strings.TrimSpace(handlerName))
	switch normalizedHandler {
	case "require_file_exists":
		path := strings.TrimSpace(readHookParamString(params, "path"))
		if path == "" {
			return nil, fmt.Errorf("handler require_file_exists requires params.path")
		}
		message := strings.TrimSpace(readHookParamString(params, "message"))
		return func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
			_ = ctx
			workdir := resolveHookWorkdir(input, defaultWorkdir)
			resolvedPath, err := resolveHookPathWithinWorkdir(workdir, path)
			if err != nil {
				detail := fmt.Sprintf("require_file_exists(%s) denied: %v", path, err)
				return runtimehooks.HookResult{Status: runtimehooks.HookResultFailed, Message: detail, Error: detail}
			}
			info, statErr := os.Stat(resolvedPath)
			if statErr != nil {
				detail := fmt.Sprintf("required file missing: %s", path)
				if message != "" {
					detail = message
				}
				return runtimehooks.HookResult{Status: runtimehooks.HookResultFailed, Message: detail, Error: statErr.Error()}
			}
			if info.IsDir() {
				detail := fmt.Sprintf("required file is a directory: %s", path)
				return runtimehooks.HookResult{Status: runtimehooks.HookResultFailed, Message: detail, Error: detail}
			}
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		}, nil
	case "warn_on_tool_call":
		targetTool := strings.ToLower(strings.TrimSpace(readHookParamString(params, "tool_name")))
		targetTools := normalizeHookParamStringSlice(readHookParamStringSlice(params, "tool_names"))
		if targetTool == "" && len(targetTools) == 0 {
			return nil, fmt.Errorf("handler warn_on_tool_call requires params.tool_name or params.tool_names")
		}
		defaultMessage := "tool call matched warn_on_tool_call"
		if customMessage := strings.TrimSpace(readHookParamString(params, "message")); customMessage != "" {
			defaultMessage = customMessage
		}
		return func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
			_ = ctx
			toolName := strings.ToLower(strings.TrimSpace(readHookContextMetadataString(input, "tool_name")))
			if toolName == "" {
				return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
			}
			if targetTool != "" && toolName == targetTool {
				return runtimehooks.HookResult{Status: runtimehooks.HookResultPass, Message: defaultMessage}
			}
			if len(targetTools) > 0 && slices.Contains(targetTools, toolName) {
				return runtimehooks.HookResult{Status: runtimehooks.HookResultPass, Message: defaultMessage}
			}
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
		}, nil
	case "add_context_note":
		note := strings.TrimSpace(readHookParamString(params, "note"))
		if note == "" {
			note = strings.TrimSpace(readHookParamString(params, "message"))
		}
		if note == "" {
			return nil, fmt.Errorf("handler add_context_note requires params.note or params.message")
		}
		return func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
			_ = ctx
			_ = input
			return runtimehooks.HookResult{Status: runtimehooks.HookResultPass, Message: note}
		}, nil
	default:
		return nil, fmt.Errorf("unsupported user builtin handler %q", handlerName)
	}
}

func readHookParamString(params map[string]any, key string) string {
	if len(params) == 0 {
		return ""
	}
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func readHookParamStringSlice(params map[string]any, key string) []string {
	if len(params) == 0 {
		return nil
	}
	value, ok := params[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item == nil {
				continue
			}
			out = append(out, strings.TrimSpace(fmt.Sprintf("%v", item)))
		}
		return out
	default:
		return nil
	}
}

func normalizeHookParamStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

func readHookContextMetadataString(input runtimehooks.HookContext, key string) string {
	if len(input.Metadata) == 0 {
		return ""
	}
	value, ok := input.Metadata[strings.ToLower(strings.TrimSpace(key))]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func resolveHookWorkdir(input runtimehooks.HookContext, fallback string) string {
	workdir := strings.TrimSpace(readHookContextMetadataString(input, "workdir"))
	if workdir != "" {
		return workdir
	}
	return strings.TrimSpace(fallback)
}

func resolveHookPathWithinWorkdir(workdir string, rawPath string) (string, error) {
	normalizedWorkdir := strings.TrimSpace(workdir)
	if normalizedWorkdir == "" {
		return "", fmt.Errorf("workdir is empty")
	}
	workdirAbs, err := filepath.Abs(filepath.Clean(normalizedWorkdir))
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}

	normalizedPath := strings.TrimSpace(rawPath)
	if normalizedPath == "" {
		return "", fmt.Errorf("path is empty")
	}
	resolvedPath := normalizedPath
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(workdirAbs, resolvedPath)
	}
	resolvedPath, err = filepath.Abs(filepath.Clean(resolvedPath))
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if err := ensureHookPathWithinBase(workdirAbs, resolvedPath); err != nil {
		return "", fmt.Errorf("path %q is outside workdir", rawPath)
	}

	needsRealpathCheck, err := hookPathContainsSymlink(workdirAbs, resolvedPath)
	if err != nil {
		return "", fmt.Errorf("inspect path symlinks: %w", err)
	}
	if !needsRealpathCheck {
		return resolvedPath, nil
	}

	workdirReal, err := filepath.EvalSymlinks(workdirAbs)
	if err != nil {
		workdirReal = workdirAbs
	}
	resolvedReal, err := filepath.EvalSymlinks(resolvedPath)
	switch {
	case err == nil:
		if err := ensureHookPathWithinBase(workdirReal, resolvedReal); err != nil {
			return "", fmt.Errorf("path %q resolves outside workdir", rawPath)
		}
	case os.IsNotExist(err):
	default:
		return "", fmt.Errorf("resolve symlink path: %w", err)
	}
	return resolvedPath, nil
}

func ensureHookPathWithinBase(base string, target string) error {
	normalizedBase := normalizeHookComparablePath(base)
	normalizedTarget := normalizeHookComparablePath(target)
	if normalizedBase == "" || normalizedTarget == "" {
		return fmt.Errorf("empty comparable path")
	}
	if normalizedTarget == normalizedBase {
		return nil
	}
	prefix := normalizedBase
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if !strings.HasPrefix(normalizedTarget, prefix) {
		return fmt.Errorf("outside base path")
	}
	return nil
}

func normalizeHookComparablePath(path string) string {
	normalized := filepath.Clean(strings.TrimSpace(path))
	if runtime.GOOS == "windows" {
		normalized = strings.TrimPrefix(normalized, `\\?\`)
		normalized = strings.ToLower(normalized)
	}
	return normalized
}

func hookPathContainsSymlink(base string, target string) (bool, error) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false, fmt.Errorf("check path relation: %w", err)
	}
	if rel == "." {
		info, err := os.Lstat(target)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return info.Mode()&os.ModeSymlink != 0, nil
	}

	current := base
	for _, segment := range strings.Split(rel, string(filepath.Separator)) {
		if segment == "" || segment == "." {
			continue
		}
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
	}
	return false, nil
}
