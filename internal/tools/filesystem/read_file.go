package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"neo-code/internal/security"
	"neo-code/internal/tools"
)

const emitChunkSize = 4 * 1024

type ReadFileTool struct {
	root string
}

type readFileInput struct {
	Path string `json:"path"`
}

func New(root string) *ReadFileTool {
	return &ReadFileTool{root: root}
}

func (t *ReadFileTool) Name() string {
	return readFileToolName
}

func (t *ReadFileTool) Description() string {
	return "Read a file from the current workspace and return its contents."
}

func (t *ReadFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to the workspace root, or an absolute path inside the workspace.",
			},
		},
		"required": []string{"path"},
	}
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

	base := effectiveRoot(t.root, input.Workdir)

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
	result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)

	if input.EmitChunk != nil {
		content := []byte(result.Content)
		for start := 0; start < len(content); start += emitChunkSize {
			end := start + emitChunkSize
			if end > len(content) {
				end = len(content)
			}
			input.EmitChunk(content[start:end])
		}
	}

	return result, nil
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
