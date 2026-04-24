package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
	verificationTaskPolicyUnknown = "unknown"
)

const (
	verifierTodoConvergence = "todo_convergence"
	verifierFileExists      = "file_exists"
	verifierContentMatch    = "content_match"
	verifierCommandSuccess  = "command_success"
	verifierGitDiff         = "git_diff"
	verifierBuild           = "build"
	verifierTest            = "test"
	verifierLint            = "lint"
	verifierTypecheck       = "typecheck"
)

const (
	verificationHookBeforeVerification       = "before_verification"
	verificationHookAfterVerification        = "after_verification"
	verificationHookBeforeCompletionDecision = "before_completion_decision"
)

const (
	verificationHookFailurePolicyFailClosed = "fail_closed"
	verificationHookFailurePolicyFailOpen   = "fail_open"
)

const (
	verificationScopeProject = "project"
)

const (
	verificationExecModeNonInteractive = "non_interactive"
)

var defaultVerificationAllowedCommands = []string{
	"go",
	"git",
	"npm",
	"pnpm",
	"yarn",
	"make",
	"cargo",
	"python",
	"pytest",
	"ruff",
	"eslint",
	"tsc",
	"golangci-lint",
}

var defaultVerificationDeniedCommands = []string{
	"rm",
	"sudo",
	"curl",
	"wget",
}

// VerificationConfig 定义 runtime final 验收阶段的 verifier 编排配置。
type VerificationConfig struct {
	Enabled           *bool                             `yaml:"enabled,omitempty"`
	DefaultTaskPolicy string                            `yaml:"default_task_policy,omitempty"`
	FinalIntercept    *bool                             `yaml:"final_intercept,omitempty"`
	MaxNoProgress     int                               `yaml:"max_no_progress,omitempty"`
	MaxRetries        int                               `yaml:"max_retries,omitempty"`
	Verifiers         map[string]VerifierConfig         `yaml:"verifiers,omitempty"`
	ExecutionPolicy   VerificationExecutionPolicyConfig `yaml:"execution_policy,omitempty"`
	Hooks             VerificationHookConfig            `yaml:"hooks,omitempty"`
}

// VerifierConfig 定义单个 verifier 的启停、命令与失败策略。
type VerifierConfig struct {
	Enabled        bool   `yaml:"enabled,omitempty"`
	Required       bool   `yaml:"required,omitempty"`
	Command        string `yaml:"command,omitempty"`
	TimeoutSec     int    `yaml:"timeout_sec,omitempty"`
	OutputCapBytes int    `yaml:"output_cap_bytes,omitempty"`
	Scope          string `yaml:"scope,omitempty"`
	FailOpen       bool   `yaml:"fail_open,omitempty"`
	FailClosed     bool   `yaml:"fail_closed,omitempty"`
}

// VerificationHookConfig 定义验证前后内置 hook 的执行策略。
type VerificationHookConfig struct {
	BeforeVerification       HookSpec `yaml:"before_verification,omitempty"`
	AfterVerification        HookSpec `yaml:"after_verification,omitempty"`
	BeforeCompletionDecision HookSpec `yaml:"before_completion_decision,omitempty"`
}

// HookSpec 定义单个 hook 的开关、超时、优先级与失败策略。
type HookSpec struct {
	Enabled       bool   `yaml:"enabled,omitempty"`
	TimeoutSec    int    `yaml:"timeout_sec,omitempty"`
	FailurePolicy string `yaml:"failure_policy,omitempty"`
	Priority      int    `yaml:"priority,omitempty"`
}

// VerificationExecutionPolicyConfig 定义 verifier 命令的非交互执行策略。
type VerificationExecutionPolicyConfig struct {
	Mode             string   `yaml:"mode,omitempty"`
	NonInteractive   bool     `yaml:"non_interactive,omitempty"`
	AllowedCommands  []string `yaml:"allowed_commands,omitempty"`
	DeniedCommands   []string `yaml:"denied_commands,omitempty"`
	DefaultTimeout   int      `yaml:"default_timeout_sec,omitempty"`
	DefaultOutputCap int      `yaml:"default_output_cap_bytes,omitempty"`
}

// defaultVerificationConfig 返回验证引擎默认策略。
func defaultVerificationConfig() VerificationConfig {
	return VerificationConfig{
		Enabled:           boolPtrConfig(true),
		DefaultTaskPolicy: verificationTaskPolicyUnknown,
		FinalIntercept:    boolPtrConfig(true),
		MaxNoProgress:     3,
		MaxRetries:        2,
		Verifiers: map[string]VerifierConfig{
			verifierTodoConvergence: {
				Enabled:        true,
				Required:       true,
				TimeoutSec:     5,
				OutputCapBytes: 32 * 1024,
				Scope:          verificationScopeProject,
				FailClosed:     true,
			},
			verifierFileExists: {
				Enabled:        true,
				Required:       false,
				TimeoutSec:     5,
				OutputCapBytes: 32 * 1024,
				Scope:          verificationScopeProject,
				FailClosed:     true,
			},
			verifierContentMatch: {
				Enabled:        true,
				Required:       false,
				TimeoutSec:     5,
				OutputCapBytes: 32 * 1024,
				Scope:          verificationScopeProject,
				FailClosed:     true,
			},
			verifierCommandSuccess: {
				Enabled:        false,
				Required:       false,
				TimeoutSec:     120,
				OutputCapBytes: 128 * 1024,
				Scope:          verificationScopeProject,
				FailClosed:     true,
			},
			verifierGitDiff: {
				Enabled:        true,
				Required:       false,
				Command:        "git diff --name-only",
				TimeoutSec:     15,
				OutputCapBytes: 64 * 1024,
				Scope:          verificationScopeProject,
				FailClosed:     true,
			},
			verifierBuild: {
				Enabled:        false,
				Required:       false,
				TimeoutSec:     300,
				OutputCapBytes: 256 * 1024,
				Scope:          verificationScopeProject,
				FailClosed:     true,
			},
			verifierTest: {
				Enabled:        false,
				Required:       false,
				TimeoutSec:     300,
				OutputCapBytes: 256 * 1024,
				Scope:          verificationScopeProject,
				FailClosed:     true,
			},
			verifierLint: {
				Enabled:        false,
				Required:       false,
				TimeoutSec:     180,
				OutputCapBytes: 256 * 1024,
				Scope:          verificationScopeProject,
				FailClosed:     true,
			},
			verifierTypecheck: {
				Enabled:        false,
				Required:       false,
				TimeoutSec:     180,
				OutputCapBytes: 256 * 1024,
				Scope:          verificationScopeProject,
				FailClosed:     true,
			},
		},
		ExecutionPolicy: defaultVerificationExecutionPolicyConfig(),
		Hooks: VerificationHookConfig{
			BeforeVerification: HookSpec{
				Enabled:       true,
				TimeoutSec:    2,
				FailurePolicy: verificationHookFailurePolicyFailClosed,
				Priority:      10,
			},
			AfterVerification: HookSpec{
				Enabled:       true,
				TimeoutSec:    2,
				FailurePolicy: verificationHookFailurePolicyFailOpen,
				Priority:      20,
			},
			BeforeCompletionDecision: HookSpec{
				Enabled:       true,
				TimeoutSec:    2,
				FailurePolicy: verificationHookFailurePolicyFailClosed,
				Priority:      30,
			},
		},
	}
}

// defaultVerificationExecutionPolicyConfig 返回 verifier 默认执行策略。
func defaultVerificationExecutionPolicyConfig() VerificationExecutionPolicyConfig {
	return VerificationExecutionPolicyConfig{
		Mode:             verificationExecModeNonInteractive,
		NonInteractive:   true,
		AllowedCommands:  append([]string(nil), defaultVerificationAllowedCommands...),
		DeniedCommands:   append([]string(nil), defaultVerificationDeniedCommands...),
		DefaultTimeout:   120,
		DefaultOutputCap: 128 * 1024,
	}
}

// Clone 复制 verification 配置，避免 map/slice 共享底层数据。
func (c VerificationConfig) Clone() VerificationConfig {
	cloned := VerificationConfig{
		Enabled:           cloneBoolPtr(c.Enabled),
		DefaultTaskPolicy: c.DefaultTaskPolicy,
		FinalIntercept:    cloneBoolPtr(c.FinalIntercept),
		MaxNoProgress:     c.MaxNoProgress,
		MaxRetries:        c.MaxRetries,
		ExecutionPolicy:   c.ExecutionPolicy.Clone(),
		Hooks:             c.Hooks.Clone(),
	}
	if len(c.Verifiers) > 0 {
		cloned.Verifiers = make(map[string]VerifierConfig, len(c.Verifiers))
		for name, cfg := range c.Verifiers {
			cloned.Verifiers[name] = cfg.Clone()
		}
	}
	return cloned
}

// ApplyDefaults 在配置缺失时补齐 verification 默认值。
func (c *VerificationConfig) ApplyDefaults(defaults VerificationConfig) {
	if c == nil {
		return
	}
	if c.Enabled == nil {
		c.Enabled = cloneBoolPtr(defaults.Enabled)
	}
	if c.FinalIntercept == nil {
		c.FinalIntercept = cloneBoolPtr(defaults.FinalIntercept)
	}
	if strings.TrimSpace(c.DefaultTaskPolicy) == "" {
		c.DefaultTaskPolicy = defaults.DefaultTaskPolicy
	}
	if c.MaxNoProgress <= 0 {
		c.MaxNoProgress = defaults.MaxNoProgress
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = defaults.MaxRetries
	}
	if c.Verifiers == nil {
		c.Verifiers = make(map[string]VerifierConfig, len(defaults.Verifiers))
	}
	for name, def := range defaults.Verifiers {
		current, exists := c.Verifiers[name]
		if !exists {
			c.Verifiers[name] = def
			continue
		}
		current.ApplyDefaults(def)
		c.Verifiers[name] = current
	}
	c.ExecutionPolicy.ApplyDefaults(defaults.ExecutionPolicy)
	c.Hooks.ApplyDefaults(defaults.Hooks)
}

// EnabledValue 返回 verification 总开关的最终布尔值；未设置时返回 false。
func (c VerificationConfig) EnabledValue() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}

// FinalInterceptValue 返回 final 拦截开关的最终布尔值；未设置时返回 false。
func (c VerificationConfig) FinalInterceptValue() bool {
	if c.FinalIntercept == nil {
		return false
	}
	return *c.FinalIntercept
}

// Validate 校验 verification 配置合法性。
func (c VerificationConfig) Validate() error {
	if strings.TrimSpace(c.DefaultTaskPolicy) == "" {
		return errors.New("runtime.verification.default_task_policy is required")
	}
	if c.MaxNoProgress <= 0 {
		return errors.New("runtime.verification.max_no_progress must be greater than 0")
	}
	if c.MaxRetries < 0 {
		return errors.New("runtime.verification.max_retries must be greater than or equal to 0")
	}
	for name, verifier := range c.Verifiers {
		if strings.TrimSpace(name) == "" {
			return errors.New("runtime.verification.verifiers has empty name")
		}
		if err := verifier.Validate(); err != nil {
			return fmt.Errorf("runtime.verification.verifiers.%s: %w", name, err)
		}
	}
	if err := c.ExecutionPolicy.Validate(); err != nil {
		return err
	}
	if err := c.Hooks.Validate(); err != nil {
		return err
	}
	return nil
}

// Clone 复制 verifier 配置。
func (c VerifierConfig) Clone() VerifierConfig {
	return c
}

// ApplyDefaults 在 verifier 配置缺失时补齐默认值。
func (c *VerifierConfig) ApplyDefaults(defaults VerifierConfig) {
	if c == nil {
		return
	}
	if c.TimeoutSec <= 0 {
		c.TimeoutSec = defaults.TimeoutSec
	}
	if c.OutputCapBytes <= 0 {
		c.OutputCapBytes = defaults.OutputCapBytes
	}
	if strings.TrimSpace(c.Scope) == "" {
		c.Scope = defaults.Scope
	}
	if !c.FailOpen && !c.FailClosed {
		c.FailOpen = defaults.FailOpen
		c.FailClosed = defaults.FailClosed
	}
}

// Validate 校验单个 verifier 配置合法性。
func (c VerifierConfig) Validate() error {
	if c.FailOpen && c.FailClosed {
		return errors.New("fail_open and fail_closed are mutually exclusive")
	}
	if c.TimeoutSec < 0 {
		return errors.New("timeout_sec must be greater than or equal to 0")
	}
	if c.OutputCapBytes < 0 {
		return errors.New("output_cap_bytes must be greater than or equal to 0")
	}
	return nil
}

// Clone 复制 hook 配置。
func (c VerificationHookConfig) Clone() VerificationHookConfig {
	return VerificationHookConfig{
		BeforeVerification:       c.BeforeVerification.Clone(),
		AfterVerification:        c.AfterVerification.Clone(),
		BeforeCompletionDecision: c.BeforeCompletionDecision.Clone(),
	}
}

// ApplyDefaults 在 hook 配置缺失时回填默认值。
func (c *VerificationHookConfig) ApplyDefaults(defaults VerificationHookConfig) {
	if c == nil {
		return
	}
	c.BeforeVerification.ApplyDefaults(defaults.BeforeVerification)
	c.AfterVerification.ApplyDefaults(defaults.AfterVerification)
	c.BeforeCompletionDecision.ApplyDefaults(defaults.BeforeCompletionDecision)
}

// Validate 校验 hook 配置是否合法。
func (c VerificationHookConfig) Validate() error {
	if err := c.BeforeVerification.Validate(); err != nil {
		return fmt.Errorf("%s: %w", verificationHookBeforeVerification, err)
	}
	if err := c.AfterVerification.Validate(); err != nil {
		return fmt.Errorf("%s: %w", verificationHookAfterVerification, err)
	}
	if err := c.BeforeCompletionDecision.Validate(); err != nil {
		return fmt.Errorf("%s: %w", verificationHookBeforeCompletionDecision, err)
	}
	return nil
}

// Clone 复制单个 hook 定义。
func (c HookSpec) Clone() HookSpec {
	return c
}

// ApplyDefaults 在 hook 缺失时补齐默认值。
func (c *HookSpec) ApplyDefaults(defaults HookSpec) {
	if c == nil {
		return
	}
	if c.TimeoutSec <= 0 {
		c.TimeoutSec = defaults.TimeoutSec
	}
	if strings.TrimSpace(c.FailurePolicy) == "" {
		c.FailurePolicy = defaults.FailurePolicy
	}
}

// Validate 校验 hook 配置是否合法。
func (c HookSpec) Validate() error {
	if c.TimeoutSec < 0 {
		return errors.New("timeout_sec must be greater than or equal to 0")
	}
	switch strings.TrimSpace(c.FailurePolicy) {
	case verificationHookFailurePolicyFailClosed, verificationHookFailurePolicyFailOpen:
		return nil
	default:
		return fmt.Errorf("unsupported failure_policy %q", c.FailurePolicy)
	}
}

// Clone 复制执行策略配置。
func (c VerificationExecutionPolicyConfig) Clone() VerificationExecutionPolicyConfig {
	return VerificationExecutionPolicyConfig{
		Mode:             c.Mode,
		NonInteractive:   c.NonInteractive,
		AllowedCommands:  append([]string(nil), c.AllowedCommands...),
		DeniedCommands:   append([]string(nil), c.DeniedCommands...),
		DefaultTimeout:   c.DefaultTimeout,
		DefaultOutputCap: c.DefaultOutputCap,
	}
}

// ApplyDefaults 在执行策略缺失时补齐默认值。
func (c *VerificationExecutionPolicyConfig) ApplyDefaults(defaults VerificationExecutionPolicyConfig) {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.Mode) == "" {
		c.Mode = defaults.Mode
	}
	if len(c.AllowedCommands) == 0 {
		c.AllowedCommands = append([]string(nil), defaults.AllowedCommands...)
	}
	if len(c.DeniedCommands) == 0 {
		c.DeniedCommands = append([]string(nil), defaults.DeniedCommands...)
	}
	if c.DefaultTimeout <= 0 {
		c.DefaultTimeout = defaults.DefaultTimeout
	}
	if c.DefaultOutputCap <= 0 {
		c.DefaultOutputCap = defaults.DefaultOutputCap
	}
}

// Validate 校验执行策略配置。
func (c VerificationExecutionPolicyConfig) Validate() error {
	if strings.TrimSpace(c.Mode) == "" {
		return errors.New("runtime.verification.execution_policy.mode is required")
	}
	if c.DefaultTimeout <= 0 {
		return errors.New("runtime.verification.execution_policy.default_timeout_sec must be greater than 0")
	}
	if c.DefaultOutputCap <= 0 {
		return errors.New("runtime.verification.execution_policy.default_output_cap_bytes must be greater than 0")
	}
	return nil
}

// boolPtr 构造 bool 指针，便于配置默认值与序列化结构复用。
func boolPtrConfig(value bool) *bool {
	v := value
	return &v
}

// cloneBoolPtr 复制 bool 指针，避免 Clone 后共享底层地址。
func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}
