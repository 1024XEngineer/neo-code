package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"neo-code/internal/config"
	runtimehooks "neo-code/internal/runtime/hooks"
)

const (
	configuredHookKindBuiltin = "builtin"
	configuredHookKindHTTP    = "http"
	configuredHookModeSync    = "sync"
	configuredHookModeObserve = "observe"
	// httpObserveDrainLimitBytes 用于限制成功路径响应体的最大清空字节，避免被大响应拖慢。
	httpObserveDrainLimitBytes = 64 * 1024
)

var httpObserveSensitiveMetadataKeys = map[string]struct{}{
	"result_content_preview": {},
	"execution_error":        {},
}

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

// buildUserHookSpec 将 user hook 配置转换为 runtime 可执行 HookSpec。
func buildUserHookSpec(item config.RuntimeHookItemConfig, defaultWorkdir string) (runtimehooks.HookSpec, error) {
	return buildConfiguredHookSpec(
		item,
		defaultWorkdir,
		runtimehooks.HookScopeUser,
		runtimehooks.HookSourceUser,
	)
}

// buildRepoHookSpec 将 repo hook 配置转换为 runtime 可执行 HookSpec。
func buildRepoHookSpec(item config.RuntimeHookItemConfig, defaultWorkdir string) (runtimehooks.HookSpec, error) {
	return buildConfiguredHookSpec(
		item,
		defaultWorkdir,
		runtimehooks.HookScopeRepo,
		runtimehooks.HookSourceRepo,
	)
}

// buildConfiguredHookSpec 按给定 scope/source 构建配置化 hook 执行定义。
func buildConfiguredHookSpec(
	item config.RuntimeHookItemConfig,
	defaultWorkdir string,
	scope runtimehooks.HookScope,
	source runtimehooks.HookSource,
) (runtimehooks.HookSpec, error) {
	if err := validateConfiguredHookItemForP6Lite(item, scope); err != nil {
		return runtimehooks.HookSpec{}, err
	}
	kind := strings.ToLower(strings.TrimSpace(item.Kind))
	specKind := runtimehooks.HookKindFunction
	specMode := runtimehooks.HookModeSync
	var (
		handler runtimehooks.HookHandler
		err     error
	)
	switch kind {
	case configuredHookKindBuiltin:
		handler, err = buildUserBuiltinHookHandler(strings.TrimSpace(item.Handler), item.Params, defaultWorkdir)
		specKind = runtimehooks.HookKindFunction
		specMode = runtimehooks.HookModeSync
	case configuredHookKindHTTP:
		handler, err = buildUserHTTPObserveHookHandler(item)
		specKind = runtimehooks.HookKindHTTP
		specMode = runtimehooks.HookModeObserve
	default:
		return runtimehooks.HookSpec{}, fmt.Errorf("kind %q is not supported", item.Kind)
	}
	if err != nil {
		return runtimehooks.HookSpec{}, err
	}
	return runtimehooks.HookSpec{
		ID:            strings.TrimSpace(item.ID),
		Point:         runtimehooks.HookPoint(strings.TrimSpace(item.Point)),
		Scope:         scope,
		Source:        source,
		Kind:          specKind,
		Mode:          specMode,
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
	mode := strings.ToLower(strings.TrimSpace(item.Mode))
	switch kind {
	case configuredHookKindBuiltin:
		if mode != configuredHookModeSync {
			return fmt.Errorf("mode %q is not supported", item.Mode)
		}
	case configuredHookKindHTTP:
		if mode != configuredHookModeObserve {
			return fmt.Errorf("mode %q is not supported for kind http (only observe)", item.Mode)
		}
		policy := strings.ToLower(strings.TrimSpace(item.FailurePolicy))
		if policy == "fail_closed" {
			return fmt.Errorf("failure_policy %q is not supported for kind http observe", item.FailurePolicy)
		}
	default:
		if isExternalHookKind(kind) {
			return fmt.Errorf(
				"external hook kind %q is not supported in P6-lite; only builtin/http-observe hooks are enabled",
				item.Kind,
			)
		}
		return fmt.Errorf("kind %q is not supported", item.Kind)
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

// buildUserHTTPObserveHookHandler 将 kind=http 的 observe 配置转换为观测回调处理器。
func buildUserHTTPObserveHookHandler(item config.RuntimeHookItemConfig) (runtimehooks.HookHandler, error) {
	endpoint := strings.TrimSpace(readHookParamString(item.Params, "url"))
	if endpoint == "" {
		return nil, fmt.Errorf("kind http requires params.url")
	}
	if err := validateHTTPObserveEndpoint(endpoint); err != nil {
		return nil, err
	}
	method := strings.ToUpper(strings.TrimSpace(readHookParamString(item.Params, "method")))
	if method == "" {
		method = http.MethodPost
	}
	headers, err := buildHTTPObserveHeaders(item.Params)
	if err != nil {
		return nil, err
	}
	includeMetadata := readHookParamBool(item.Params, "include_metadata", false)
	hookID := strings.TrimSpace(item.ID)
	point := strings.TrimSpace(item.Point)

	return func(ctx context.Context, input runtimehooks.HookContext) runtimehooks.HookResult {
		payload := map[string]any{
			"hook_id":      hookID,
			"point":        point,
			"scope":        "user",
			"kind":         configuredHookKindHTTP,
			"mode":         configuredHookModeObserve,
			"run_id":       strings.TrimSpace(input.RunID),
			"session_id":   strings.TrimSpace(input.SessionID),
			"triggered_at": time.Now().UTC().Format(time.RFC3339Nano),
		}
		if includeMetadata && len(input.Metadata) > 0 {
			payload["metadata"] = sanitizeHTTPObserveMetadata(input.Metadata)
		}
		body, err := json.Marshal(payload)
		if err != nil {
			detail := fmt.Sprintf("http observe marshal payload failed: %v", err)
			return runtimehooks.HookResult{Status: runtimehooks.HookResultFailed, Message: detail, Error: detail}
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			detail := fmt.Sprintf("http observe build request failed: %v", err)
			return runtimehooks.HookResult{Status: runtimehooks.HookResultFailed, Message: detail, Error: detail}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "neocode-hook-observe/1.0")
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			detail := fmt.Sprintf("http observe request failed: %v", err)
			return runtimehooks.HookResult{Status: runtimehooks.HookResultFailed, Message: detail, Error: detail}
		}
		defer drainAndCloseHTTPResponseBody(resp)
		if resp.StatusCode >= http.StatusBadRequest {
			preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			detail := fmt.Sprintf("http observe response status=%d", resp.StatusCode)
			errText := detail
			if trimmed := strings.TrimSpace(string(preview)); trimmed != "" {
				errText = detail + ": " + trimmed
			}
			return runtimehooks.HookResult{Status: runtimehooks.HookResultFailed, Message: detail, Error: errText}
		}
		return runtimehooks.HookResult{Status: runtimehooks.HookResultPass}
	}, nil
}

// sanitizeHTTPObserveMetadata 对外发 payload 前剥离敏感预览字段，避免意外外传。
func sanitizeHTTPObserveMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	sanitized := make(map[string]any, len(metadata))
	for key, value := range metadata {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := httpObserveSensitiveMetadataKeys[normalized]; blocked {
			continue
		}
		sanitized[key] = value
	}
	return sanitized
}

// drainAndCloseHTTPResponseBody 在关闭前执行有上限 drain，兼顾连接复用与响应体放大风险控制。
func drainAndCloseHTTPResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, httpObserveDrainLimitBytes))
	_ = resp.Body.Close()
}

// validateHTTPObserveEndpoint 校验 observe 回调地址，仅允许本机回环地址。
func validateHTTPObserveEndpoint(endpoint string) error {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed == nil || !parsed.IsAbs() {
		return fmt.Errorf("kind http requires absolute params.url")
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("kind http only supports http/https params.url")
	}
	if !isHTTPObserveLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("kind http params.url host %q is not allowed (loopback only)", parsed.Hostname())
	}
	return nil
}

// isHTTPObserveLoopbackHost 判断目标地址是否为回环主机，避免误配为公网外发。
func isHTTPObserveLoopbackHost(host string) bool {
	normalized := strings.TrimSpace(strings.ToLower(host))
	if normalized == "" {
		return false
	}
	if normalized == "localhost" {
		return true
	}
	ip := net.ParseIP(normalized)
	return ip != nil && ip.IsLoopback()
}

// buildHTTPObserveHeaders 从 params.headers 读取并归一化 HTTP 头配置。
func buildHTTPObserveHeaders(params map[string]any) (map[string]string, error) {
	if len(params) == 0 {
		return nil, nil
	}
	rawHeaders, ok := params["headers"]
	if !ok || rawHeaders == nil {
		return nil, nil
	}
	typed, ok := rawHeaders.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("kind http params.headers must be a map")
	}
	headers := make(map[string]string, len(typed))
	for key, value := range typed {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			return nil, fmt.Errorf("kind http params.headers contains empty header name")
		}
		normalizedValue := strings.TrimSpace(fmt.Sprintf("%v", value))
		if normalizedValue == "" {
			return nil, fmt.Errorf("kind http params.headers[%q] is empty", normalizedKey)
		}
		headers[normalizedKey] = normalizedValue
	}
	return headers, nil
}

// readHookParamBool 按默认值读取布尔参数，兼容 bool/string/number 输入。
func readHookParamBool(params map[string]any, key string, defaultValue bool) bool {
	if len(params) == 0 {
		return defaultValue
	}
	value, ok := params[key]
	if !ok || value == nil {
		return defaultValue
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "on":
			return true
		case "false", "0", "no", "off":
			return false
		default:
			return defaultValue
		}
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return defaultValue
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
