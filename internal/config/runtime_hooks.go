package config

import (
	"fmt"
	"strings"
)

const (
	// DefaultRuntimeHooksEnabled 定义 runtime hooks 全局开关默认值。
	DefaultRuntimeHooksEnabled = true
	// DefaultRuntimeUserHooksEnabled 定义 user hooks 开关默认值。
	DefaultRuntimeUserHooksEnabled = true
	// DefaultRuntimeHookTimeoutSec 定义 hook 默认超时秒数。
	DefaultRuntimeHookTimeoutSec = 2
)

const (
	runtimeHookFailurePolicyWarnOnly  = "warn_only"
	runtimeHookFailurePolicyFailOpen  = "fail_open"
	runtimeHookFailurePolicyFailClose = "fail_closed"
)

const (
	runtimeHookScopeUser   = "user"
	runtimeHookKindBuiltIn = "builtin"
	runtimeHookModeSync    = "sync"
)

const (
	runtimeHookPointBeforeToolCall           = "before_tool_call"
	runtimeHookPointAfterToolResult          = "after_tool_result"
	runtimeHookPointBeforeCompletionDecision = "before_completion_decision"
	runtimeHookPointBeforePermissionDecision = "before_permission_decision"
	runtimeHookPointAfterToolFailure         = "after_tool_failure"
	runtimeHookPointSessionStart             = "session_start"
	runtimeHookPointSessionEnd               = "session_end"
	runtimeHookPointUserPromptSubmit         = "user_prompt_submit"
	runtimeHookPointPreCompact               = "pre_compact"
	runtimeHookPointPostCompact              = "post_compact"
	runtimeHookPointSubAgentStart            = "subagent_start"
	runtimeHookPointSubAgentStop             = "subagent_stop"
)

const (
	runtimeHookHandlerRequireFileExists = "require_file_exists"
	runtimeHookHandlerWarnOnToolCall    = "warn_on_tool_call"
	runtimeHookHandlerAddContextNote    = "add_context_note"
)

// RuntimeHooksConfig 描述 runtime hooks 的全局开关与 user hooks 配置。
type RuntimeHooksConfig struct {
	Enabled              *bool                   `yaml:"enabled,omitempty"`
	UserHooksEnabled     *bool                   `yaml:"user_hooks_enabled,omitempty"`
	DefaultTimeoutSec    int                     `yaml:"default_timeout_sec,omitempty"`
	DefaultFailurePolicy string                  `yaml:"default_failure_policy,omitempty"`
	Items                []RuntimeHookItemConfig `yaml:"items,omitempty"`
}

// RuntimeHookItemConfig 描述单个 user builtin hook 配置项。
type RuntimeHookItemConfig struct {
	ID            string         `yaml:"id,omitempty"`
	Enabled       *bool          `yaml:"enabled,omitempty"`
	Point         string         `yaml:"point,omitempty"`
	Scope         string         `yaml:"scope,omitempty"`
	Kind          string         `yaml:"kind,omitempty"`
	Mode          string         `yaml:"mode,omitempty"`
	Handler       string         `yaml:"handler,omitempty"`
	Priority      int            `yaml:"priority,omitempty"`
	TimeoutSec    int            `yaml:"timeout_sec,omitempty"`
	FailurePolicy string         `yaml:"failure_policy,omitempty"`
	Params        map[string]any `yaml:"params,omitempty"`
}

// defaultRuntimeHooksConfig 返回 runtime hooks 默认配置。
func defaultRuntimeHooksConfig() RuntimeHooksConfig {
	return RuntimeHooksConfig{
		Enabled:              boolPtr(DefaultRuntimeHooksEnabled),
		UserHooksEnabled:     boolPtr(DefaultRuntimeUserHooksEnabled),
		DefaultTimeoutSec:    DefaultRuntimeHookTimeoutSec,
		DefaultFailurePolicy: runtimeHookFailurePolicyWarnOnly,
		Items:                nil,
	}
}

// Clone 复制 runtime hooks 配置，避免切片/map 底层共享。
func (c RuntimeHooksConfig) Clone() RuntimeHooksConfig {
	cloned := RuntimeHooksConfig{
		DefaultTimeoutSec:    c.DefaultTimeoutSec,
		DefaultFailurePolicy: c.DefaultFailurePolicy,
	}
	if c.Enabled != nil {
		cloned.Enabled = boolPtr(*c.Enabled)
	}
	if c.UserHooksEnabled != nil {
		cloned.UserHooksEnabled = boolPtr(*c.UserHooksEnabled)
	}
	if len(c.Items) > 0 {
		cloned.Items = make([]RuntimeHookItemConfig, 0, len(c.Items))
		for _, item := range c.Items {
			cloned.Items = append(cloned.Items, item.Clone())
		}
	}
	return cloned
}

// ApplyDefaults 为 runtime hooks 配置补齐默认值。
func (c *RuntimeHooksConfig) ApplyDefaults(defaults RuntimeHooksConfig) {
	if c == nil {
		return
	}
	if c.Enabled == nil {
		if defaults.Enabled != nil {
			c.Enabled = boolPtr(*defaults.Enabled)
		} else {
			c.Enabled = boolPtr(DefaultRuntimeHooksEnabled)
		}
	}
	if c.UserHooksEnabled == nil {
		if defaults.UserHooksEnabled != nil {
			c.UserHooksEnabled = boolPtr(*defaults.UserHooksEnabled)
		} else {
			c.UserHooksEnabled = boolPtr(DefaultRuntimeUserHooksEnabled)
		}
	}
	if c.DefaultTimeoutSec <= 0 {
		c.DefaultTimeoutSec = defaults.DefaultTimeoutSec
	}
	if strings.TrimSpace(c.DefaultFailurePolicy) == "" {
		c.DefaultFailurePolicy = defaults.DefaultFailurePolicy
	}
	for index := range c.Items {
		c.Items[index].ApplyDefaults(*c)
	}
}

// Validate 校验 runtime hooks 配置合法性。
func (c RuntimeHooksConfig) Validate() error {
	if c.DefaultTimeoutSec <= 0 {
		return fmt.Errorf("runtime.hooks.default_timeout_sec must be greater than 0")
	}
	if err := validateRuntimeHookFailurePolicy(c.DefaultFailurePolicy); err != nil {
		return fmt.Errorf("runtime.hooks.default_failure_policy: %w", err)
	}
	seen := make(map[string]struct{}, len(c.Items))
	for index, item := range c.Items {
		normalizedID := strings.ToLower(strings.TrimSpace(item.ID))
		if normalizedID == "" {
			return fmt.Errorf("runtime.hooks.items[%d].id is required", index)
		}
		if _, exists := seen[normalizedID]; exists {
			return fmt.Errorf("runtime.hooks.items[%d].id duplicates %q", index, item.ID)
		}
		seen[normalizedID] = struct{}{}
		if err := item.Validate(c.DefaultFailurePolicy); err != nil {
			return fmt.Errorf("runtime.hooks.items[%d]: %w", index, err)
		}
	}
	return nil
}

// IsEnabled 返回 hooks 总开关是否开启。
func (c RuntimeHooksConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return DefaultRuntimeHooksEnabled
	}
	return *c.Enabled
}

// IsUserHooksEnabled 返回 user hooks 开关是否开启。
func (c RuntimeHooksConfig) IsUserHooksEnabled() bool {
	if c.UserHooksEnabled == nil {
		return DefaultRuntimeUserHooksEnabled
	}
	return *c.UserHooksEnabled
}

// Clone 复制单条 hook item 配置。
func (c RuntimeHookItemConfig) Clone() RuntimeHookItemConfig {
	cloned := RuntimeHookItemConfig{
		ID:            c.ID,
		Point:         c.Point,
		Scope:         c.Scope,
		Kind:          c.Kind,
		Mode:          c.Mode,
		Handler:       c.Handler,
		Priority:      c.Priority,
		TimeoutSec:    c.TimeoutSec,
		FailurePolicy: c.FailurePolicy,
	}
	if c.Enabled != nil {
		cloned.Enabled = boolPtr(*c.Enabled)
	}
	if len(c.Params) > 0 {
		cloned.Params = make(map[string]any, len(c.Params))
		for key, value := range c.Params {
			cloned.Params[key] = cloneRuntimeHookParamValue(value)
		}
	}
	return cloned
}

// ApplyDefaults 为单条 hook item 配置补齐默认值。
func (c *RuntimeHookItemConfig) ApplyDefaults(defaults RuntimeHooksConfig) {
	if c == nil {
		return
	}
	if c.Enabled == nil {
		c.Enabled = boolPtr(true)
	}
	if strings.TrimSpace(c.Scope) == "" {
		c.Scope = runtimeHookScopeUser
	}
	if strings.TrimSpace(c.Kind) == "" {
		c.Kind = runtimeHookKindBuiltIn
	}
	if strings.TrimSpace(c.Mode) == "" {
		c.Mode = runtimeHookModeSync
	}
	if c.TimeoutSec <= 0 {
		c.TimeoutSec = defaults.DefaultTimeoutSec
	}
	if strings.TrimSpace(c.FailurePolicy) == "" {
		c.FailurePolicy = defaults.DefaultFailurePolicy
	}
}

// Validate 校验单条 hook item 配置合法性。
func (c RuntimeHookItemConfig) Validate(defaultFailurePolicy string) error {
	if strings.TrimSpace(c.ID) == "" {
		return fmt.Errorf("id is required")
	}
	point := strings.TrimSpace(c.Point)
	switch point {
	case runtimeHookPointBeforeToolCall,
		runtimeHookPointAfterToolResult,
		runtimeHookPointBeforeCompletionDecision,
		runtimeHookPointBeforePermissionDecision,
		runtimeHookPointAfterToolFailure,
		runtimeHookPointSessionStart,
		runtimeHookPointSessionEnd,
		runtimeHookPointUserPromptSubmit,
		runtimeHookPointPreCompact,
		runtimeHookPointPostCompact,
		runtimeHookPointSubAgentStart,
		runtimeHookPointSubAgentStop:
	default:
		return fmt.Errorf("point %q is not supported", c.Point)
	}
	if !runtimeHookPointUserAllowed(point) {
		return fmt.Errorf("point %q does not allow user hooks", c.Point)
	}

	if normalizedScope := strings.ToLower(strings.TrimSpace(c.Scope)); normalizedScope != runtimeHookScopeUser {
		return fmt.Errorf("scope %q is not supported", c.Scope)
	}
	if normalizedKind := strings.ToLower(strings.TrimSpace(c.Kind)); normalizedKind != runtimeHookKindBuiltIn {
		return fmt.Errorf("kind %q is not supported", c.Kind)
	}
	if normalizedMode := strings.ToLower(strings.TrimSpace(c.Mode)); normalizedMode != runtimeHookModeSync {
		return fmt.Errorf("mode %q is not supported", c.Mode)
	}
	if c.TimeoutSec <= 0 {
		return fmt.Errorf("timeout_sec must be greater than 0")
	}
	policy := c.FailurePolicy
	if strings.TrimSpace(policy) == "" {
		policy = defaultFailurePolicy
	}
	if err := validateRuntimeHookFailurePolicy(policy); err != nil {
		return fmt.Errorf("failure_policy: %w", err)
	}

	handler := strings.ToLower(strings.TrimSpace(c.Handler))
	switch handler {
	case runtimeHookHandlerRequireFileExists, runtimeHookHandlerWarnOnToolCall, runtimeHookHandlerAddContextNote:
	default:
		return fmt.Errorf("handler %q is not supported", c.Handler)
	}
	if handler == runtimeHookHandlerWarnOnToolCall && !hasWarnOnToolCallTargets(c.Params) {
		return fmt.Errorf("handler %q requires params.tool_name or params.tool_names", c.Handler)
	}
	return nil
}

// IsEnabled 返回单条 hook item 是否启用。
func (c RuntimeHookItemConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

func validateRuntimeHookFailurePolicy(policy string) error {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case runtimeHookFailurePolicyWarnOnly, runtimeHookFailurePolicyFailOpen, runtimeHookFailurePolicyFailClose:
		return nil
	default:
		return fmt.Errorf("invalid policy %q", policy)
	}
}

func cloneRuntimeHookParamValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneRuntimeHookParamValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneRuntimeHookParamValue(item)
		}
		return cloned
	default:
		return value
	}
}

func hasWarnOnToolCallTargets(params map[string]any) bool {
	if len(params) == 0 {
		return false
	}
	toolNameRaw, hasToolName := params["tool_name"]
	if hasToolName && strings.TrimSpace(fmt.Sprintf("%v", toolNameRaw)) != "" {
		return true
	}
	toolNamesRaw, hasToolNames := params["tool_names"]
	if !hasToolNames || toolNamesRaw == nil {
		return false
	}
	switch typed := toolNamesRaw.(type) {
	case []string:
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if strings.TrimSpace(fmt.Sprintf("%v", item)) != "" {
				return true
			}
		}
	}
	return false
}

func runtimeHookPointUserAllowed(point string) bool {
	switch strings.ToLower(strings.TrimSpace(point)) {
	case runtimeHookPointBeforePermissionDecision, runtimeHookPointPreCompact, runtimeHookPointSubAgentStart:
		return false
	default:
		return true
	}
}
