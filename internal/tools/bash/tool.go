package bash

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"neo-code/internal/tools"
)

type Tool struct {
	root     string
	shell    string
	timeout  time.Duration
	executor SecurityExecutor
}

type input struct {
	Command           string `json:"command"`
	Workdir           string `json:"workdir,omitempty"`
	Verification      bool   `json:"verification,omitempty"`
	VerificationScope string `json:"verification_scope,omitempty"`
}

func New(root string, shell string, timeout time.Duration) *Tool {
	executor := NewDefaultSecurityExecutor(root, shell, timeout)
	return &Tool{
		root:     root,
		shell:    shell,
		timeout:  timeout,
		executor: executor,
	}
}

// NewWithExecutor creates a bash tool using an injected security executor.
func NewWithExecutor(root string, shell string, timeout time.Duration, executor SecurityExecutor) *Tool {
	if executor == nil {
		executor = NewDefaultSecurityExecutor(root, shell, timeout)
	}
	return &Tool{
		root:     root,
		shell:    shell,
		timeout:  timeout,
		executor: executor,
	}
}

func (t *Tool) Name() string {
	return tools.ToolNameBash
}

func (t *Tool) Description() string {
	return "Execute a shell command inside the workspace with timeout and bounded output."
}

func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute.",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root.",
			},
			"verification": map[string]any{
				"type":        "boolean",
				"description": "Set true when this command is explicitly used for verification.",
			},
			"verification_scope": map[string]any{
				"type":        "string",
				"description": "Optional verification scope. Defaults to workspace when verification=true.",
			},
		},
		"required": []string{"command"},
	}
}

// MicroCompactPolicy 声明 bash 工具的历史结果默认参与 micro compact 清理。
func (t *Tool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *Tool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var in input
	if err := json.Unmarshal(call.Arguments, &in); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if t.executor == nil {
		err := errors.New("bash: security executor is nil")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	result, err := t.executor.Execute(ctx, call, in.Command, in.Workdir)
	result.Metadata = withVerificationMetadata(result.Metadata, in, err == nil && !result.IsError)
	result.Facts = withVerificationFacts(result.Facts, in, err == nil && !result.IsError)
	return result, err
}

// withVerificationMetadata 在 bash 调用显式声明验证意图时写入结构化验证元数据。
func withVerificationMetadata(metadata map[string]any, in input, succeeded bool) map[string]any {
	scope := in.VerificationScope
	if !in.Verification && scope == "" {
		return metadata
	}
	if metadata == nil {
		metadata = make(map[string]any, 3)
	}
	metadata["verification_performed"] = true
	metadata["verification_passed"] = succeeded
	if scope == "" {
		scope = "workspace"
	}
	metadata["verification_scope"] = scope
	return metadata
}

// withVerificationFacts 在 bash 调用显式声明验证意图时写入受信的结构化事实。
func withVerificationFacts(facts tools.ToolExecutionFacts, in input, succeeded bool) tools.ToolExecutionFacts {
	scope := strings.TrimSpace(in.VerificationScope)
	if !in.Verification && scope == "" {
		return facts
	}
	facts.VerificationPerformed = true
	facts.VerificationPassed = succeeded
	if scope == "" {
		scope = "workspace"
	}
	facts.VerificationScope = scope
	return facts
}
