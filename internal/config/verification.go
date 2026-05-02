package config

import (
	"errors"
	"fmt"
	"strings"
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

// VerificationConfig 定义 runtime final 验收阶段的 verifier 执行配置。
type VerificationConfig struct {
	MaxNoProgress   int                               `yaml:"max_no_progress,omitempty"`
	Verifiers       map[string]VerifierConfig         `yaml:"verifiers,omitempty"`
	ExecutionPolicy VerificationExecutionPolicyConfig `yaml:"execution_policy,omitempty"`
}

// VerifierConfig 定义单个 verifier 的命令与超时限制。
type VerifierConfig struct {
	Command        []string `yaml:"command,omitempty"`
	TimeoutSec     int      `yaml:"timeout_sec,omitempty"`
	OutputCapBytes int      `yaml:"output_cap_bytes,omitempty"`
	Scope          string   `yaml:"scope,omitempty"`
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
		MaxNoProgress: 2,
		Verifiers: map[string]VerifierConfig{
			verifierTodoConvergence: {
				TimeoutSec:     5,
				OutputCapBytes: 32 * 1024,
				Scope:          verificationScopeProject,
			},
			verifierFileExists: {
				TimeoutSec:     5,
				OutputCapBytes: 32 * 1024,
				Scope:          verificationScopeProject,
			},
			verifierContentMatch: {
				TimeoutSec:     5,
				OutputCapBytes: 32 * 1024,
				Scope:          verificationScopeProject,
			},
			verifierCommandSuccess: {
				TimeoutSec:     120,
				OutputCapBytes: 128 * 1024,
				Scope:          verificationScopeProject,
			},
			verifierGitDiff: {
				Command:        []string{"git", "status", "--porcelain", "--untracked-files=normal"},
				TimeoutSec:     15,
				OutputCapBytes: 64 * 1024,
				Scope:          verificationScopeProject,
			},
			verifierBuild: {
				TimeoutSec:     300,
				OutputCapBytes: 256 * 1024,
				Scope:          verificationScopeProject,
			},
			verifierTest: {
				TimeoutSec:     300,
				OutputCapBytes: 256 * 1024,
				Scope:          verificationScopeProject,
			},
			verifierLint: {
				TimeoutSec:     180,
				OutputCapBytes: 256 * 1024,
				Scope:          verificationScopeProject,
			},
			verifierTypecheck: {
				TimeoutSec:     180,
				OutputCapBytes: 256 * 1024,
				Scope:          verificationScopeProject,
			},
		},
		ExecutionPolicy: defaultVerificationExecutionPolicyConfig(),
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
		MaxNoProgress:   c.MaxNoProgress,
		ExecutionPolicy: c.ExecutionPolicy.Clone(),
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
	if c.MaxNoProgress <= 0 {
		c.MaxNoProgress = defaults.MaxNoProgress
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
}

// Validate 校验 verification 配置合法性。
func (c VerificationConfig) Validate() error {
	if c.MaxNoProgress <= 0 {
		return errors.New("runtime.verification.max_no_progress must be greater than 0")
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
	return nil
}

// Clone 复制 verifier 配置。
func (c VerifierConfig) Clone() VerifierConfig {
	return VerifierConfig{
		Command:        append([]string(nil), c.Command...),
		TimeoutSec:     c.TimeoutSec,
		OutputCapBytes: c.OutputCapBytes,
		Scope:          c.Scope,
	}
}

// ApplyDefaults 在 verifier 配置缺失时补齐默认值。
func (c *VerifierConfig) ApplyDefaults(defaults VerifierConfig) {
	if c == nil {
		return
	}
	if len(c.Command) == 0 {
		c.Command = append([]string(nil), defaults.Command...)
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
}

// Validate 校验单个 verifier 配置合法性。
func (c VerifierConfig) Validate() error {
	if c.TimeoutSec < 0 {
		return errors.New("timeout_sec must be greater than or equal to 0")
	}
	if c.OutputCapBytes < 0 {
		return errors.New("output_cap_bytes must be greater than or equal to 0")
	}
	return nil
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
