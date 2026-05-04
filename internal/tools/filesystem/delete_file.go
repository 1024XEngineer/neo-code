package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"neo-code/internal/security"
	"neo-code/internal/tools"
)

type DeleteFileTool struct {
	root string
}

type deleteFileInput struct {
	Path string `json:"path"`
}

func NewDelete(root string) *DeleteFileTool {
	return &DeleteFileTool{root: root}
}

func (t *DeleteFileTool) Name() string {
	return deleteFileToolName
}

func (t *DeleteFileTool) Description() string {
	return "Delete a single file inside the workspace. Does not remove directories."
}

func (t *DeleteFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path relative to workspace root, or absolute inside the workspace.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *DeleteFileTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *DeleteFileTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	var args deleteFileInput
	if err := json.Unmarshal(input.Arguments, &args); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if strings.TrimSpace(args.Path) == "" {
		err := errors.New(deleteFileToolName + ": path is required")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if err := ctx.Err(); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	base := effectiveRoot(t.root, input.Workdir)

	_, target, err := tools.ResolveWorkspaceTarget(input, security.TargetTypePath, base, args.Path, resolvePath)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	info, statErr := os.Stat(target)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return tools.ToolResult{
				Name:    t.Name(),
				Content: "ok",
				Metadata: map[string]any{
					"path":       target,
					"deleted":    false,
					"noop_write": true,
				},
				Facts: tools.ToolExecutionFacts{WorkspaceWrite: true},
			}, nil
		}
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), statErr), "", nil), statErr
	}
	if info.IsDir() {
		err := errors.New(deleteFileToolName + ": path is a directory; use filesystem_remove_dir")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	if err := os.Remove(target); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	return tools.ToolResult{
		Name:    t.Name(),
		Content: "ok",
		Metadata: map[string]any{
			"path":    target,
			"deleted": true,
			"bytes":   info.Size(),
		},
		Facts: tools.ToolExecutionFacts{WorkspaceWrite: true},
	}, nil
}
