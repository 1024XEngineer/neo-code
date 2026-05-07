package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"neo-code/internal/security"
	"neo-code/internal/tools"
)

type WriteFileTool struct {
	root string
}

type writeFileInput struct {
	Path              string `json:"path"`
	Content           string `json:"content"`
	VerifyAfterWrite  bool   `json:"verify_after_write,omitempty"`
	VerificationScope string `json:"verification_scope,omitempty"`
}

func NewWrite(root string) *WriteFileTool {
	return &WriteFileTool{root: root}
}

func (t *WriteFileTool) Name() string {
	return writeFileToolName
}

func (t *WriteFileTool) Description() string {
	return "Write a file inside the current workspace, creating parent directories when needed. Set verify_after_write=true to emit verification facts in the same call."
}

func (t *WriteFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to the workspace root, or an absolute path inside the workspace.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full file content to write.",
			},
			"verify_after_write": map[string]any{
				"type":        "boolean",
				"description": "When true, emit verification facts from write/read-back comparison in this same call.",
			},
			"verification_scope": map[string]any{
				"type":        "string",
				"description": "Optional verification scope label when verify_after_write is true. Defaults to artifact:<path>.",
			},
		},
		"required": []string{"path", "content"},
	}
}

// MicroCompactPolicy 声明写文件工具的历史结果默认参与 micro compact 清理。
func (t *WriteFileTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *WriteFileTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	var args writeFileInput
	if err := json.Unmarshal(input.Arguments, &args); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if strings.TrimSpace(args.Path) == "" {
		err := errors.New(writeFileToolName + ": path is required")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if err := ctx.Err(); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	base, err := tools.ResolveEffectiveRoot(t.root, input.Workdir)
	if err != nil {
		return tools.NewErrorResult(t.Name(), "invalid workdir", err.Error(), nil), err
	}

	_, target, err := tools.ResolveWorkspaceTarget(
		input,
		security.TargetTypePath,
		base,
		args.Path,
		resolvePath,
	)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	existing, readErr := os.ReadFile(target)
	if readErr == nil && string(existing) == args.Content {
		result := tools.ToolResult{
			Name:    t.Name(),
			Content: "ok",
			Metadata: map[string]any{
				"path":              target,
				"bytes":             len(args.Content),
				"noop_write":        true,
				"content_unchanged": true,
			},
		}
		if token, ok := compactWriteVerificationToken(args.Content); ok {
			result.Metadata["written_content"] = token
		}
		if args.VerifyAfterWrite {
			scope := resolveWriteVerificationScope(args.VerificationScope, target)
			result.Facts.VerificationPerformed = true
			result.Facts.VerificationPassed = true
			result.Facts.VerificationScope = scope
			result.Metadata["verification_reason"] = "write_content_match_noop"
			result.Metadata["verification_scope"] = scope
			if token, ok := compactWriteVerificationToken(args.Content); ok {
				result.Metadata["verification_expected"] = []string{token}
			}
		}
		return result, nil
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), readErr), "", nil), readErr
	}
	if err := os.WriteFile(target, []byte(args.Content), 0o644); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	result := tools.ToolResult{
		Name:    t.Name(),
		Content: "ok",
		Metadata: map[string]any{
			"path":              target,
			"bytes":             len(args.Content),
			"noop_write":        false,
			"content_unchanged": false,
		},
		Facts: tools.ToolExecutionFacts{WorkspaceWrite: true},
	}
	if token, ok := compactWriteVerificationToken(args.Content); ok {
		result.Metadata["written_content"] = token
	}
	if args.VerifyAfterWrite {
		scope := resolveWriteVerificationScope(args.VerificationScope, target)
		result.Facts.VerificationPerformed = true
		result.Facts.VerificationScope = scope
		result.Metadata["verification_scope"] = scope
		readBack, readBackErr := os.ReadFile(target)
		if readBackErr != nil {
			result.Facts.VerificationPassed = false
			result.Metadata["verification_reason"] = "write_readback_error"
			result.Metadata["verification_readback_error"] = readBackErr.Error()
		} else if string(readBack) == args.Content {
			result.Facts.VerificationPassed = true
			result.Metadata["verification_reason"] = "write_readback_match"
			if token, ok := compactWriteVerificationToken(args.Content); ok {
				result.Metadata["verification_expected"] = []string{token}
			}
		} else {
			result.Facts.VerificationPassed = false
			result.Metadata["verification_reason"] = "write_readback_mismatch"
		}
	}
	return result, nil
}

// resolveWriteVerificationScope 统一计算 write_file 写后验证 scope，避免上层缺省值不稳定。
func resolveWriteVerificationScope(raw string, path string) string {
	scope := strings.TrimSpace(raw)
	if scope != "" {
		return scope
	}
	return "artifact:" + strings.TrimSpace(path)
}

// compactWriteVerificationToken 生成可执行的内容校验 token，避免将超长文本塞进决策动作建议。
func compactWriteVerificationToken(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", false
	}
	if utf8.RuneCountInString(trimmed) > 256 {
		return "", false
	}
	return trimmed, true
}
