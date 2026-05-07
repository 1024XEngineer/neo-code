package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"neo-code/internal/security"
	"neo-code/internal/tools"
)

const emitChunkSize = 4 * 1024

var errReadFileEmitChunkFailed = errors.New(readFileToolName + ": emit chunk failed")

type ReadFileTool struct {
	root string
}

type readFileInput struct {
	Path              string   `json:"path"`
	ExpectContains    []string `json:"expect_contains,omitempty"`
	VerificationScope string   `json:"verification_scope,omitempty"`
}

func New(root string) *ReadFileTool {
	return &ReadFileTool{root: root}
}

func (t *ReadFileTool) Name() string {
	return readFileToolName
}

func (t *ReadFileTool) Description() string {
	return "Read a file from the current workspace and return its contents. Use expect_contains for explicit verification facts."
}

func (t *ReadFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to the workspace root, or an absolute path inside the workspace.",
			},
			"expect_contains": map[string]any{
				"type":        "array",
				"description": "Optional substrings that must all appear in file content to mark verification passed.",
				"items": map[string]any{
					"type": "string",
				},
			},
			"verification_scope": map[string]any{
				"type":        "string",
				"description": "Optional verification scope label to emit in ToolExecutionFacts when expect_contains is provided.",
			},
		},
		"required": []string{"path"},
	}
}

// MicroCompactPolicy 声明读文件工具的历史结果默认参与 micro compact 清理。
func (t *ReadFileTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *ReadFileTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	var args readFileInput
	if err := json.Unmarshal(input.Arguments, &args); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if strings.TrimSpace(args.Path) == "" {
		err := errors.New(readFileToolName + ": path is required")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	base, err := tools.ResolveEffectiveRoot(t.root, input.Workdir)
	if err != nil {
		return tools.NewErrorResult(t.Name(), "invalid workdir", err.Error(), nil), err
	}

	base, target, err := tools.ResolveWorkspaceTarget(
		input,
		security.TargetTypePath,
		base,
		args.Path,
		resolvePath,
	)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	filter, err := newResultPathFilter(base)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	_, reason, allowed := filter.evaluate(target)
	if !allowed {
		err := errors.New(readFileToolName + ": blocked by security policy (" + reason + ")")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	result := tools.ToolResult{
		Name:    t.Name(),
		Content: string(data),
		Metadata: map[string]any{
			"path": target,
		},
	}
	if len(args.ExpectContains) > 0 {
		result.Facts.VerificationPerformed = true
		result.Facts.VerificationScope = strings.TrimSpace(args.VerificationScope)
		missing := missingExpectedSubstrings(result.Content, args.ExpectContains)
		if len(missing) == 0 {
			result.Facts.VerificationPassed = true
			result.Metadata["verification_reason"] = "content_match"
			result.Metadata["verification_expected"] = append([]string(nil), args.ExpectContains...)
		} else {
			result.Facts.VerificationPassed = false
			result.Metadata["verification_reason"] = "content_mismatch"
			result.Metadata["verification_expected"] = append([]string(nil), args.ExpectContains...)
			result.Metadata["verification_missing"] = missing
		}
	}
	result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)

	if input.EmitChunk != nil {
		content := []byte(result.Content)
		emittedBytes := 0
		for start := 0; start < len(content); start += emitChunkSize {
			end := start + emitChunkSize
			if end > len(content) {
				end = len(content)
			}
			if emitErr := input.EmitChunk(content[start:end]); emitErr != nil {
				err := fmt.Errorf("%w: %w", errReadFileEmitChunkFailed, emitErr)
				return tools.NewErrorResult(
					t.Name(),
					tools.NormalizeErrorReason(t.Name(), err),
					"",
					map[string]any{
						"path":          target,
						"emitted_bytes": emittedBytes,
					},
				), err
			}
			emittedBytes += end - start
		}
	}

	return result, nil
}

func missingExpectedSubstrings(content string, expected []string) []string {
	if len(expected) == 0 {
		return nil
	}
	missing := make([]string, 0)
	for _, token := range expected {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}
		if !strings.Contains(content, trimmed) {
			missing = append(missing, trimmed)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return missing
}

func resolvePath(root string, requested string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}

	target := strings.TrimSpace(requested)
	if target == "" {
		return "", errors.New(readFileToolName + ": path is required")
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}

	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New(readFileToolName + ": path escapes workspace root")
	}

	return target, nil
}
