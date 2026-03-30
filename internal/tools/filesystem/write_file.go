package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"neo-code/internal/tools"
)

type WriteFileTool struct {
	root string
}

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func NewWrite(root string) *WriteFileTool {
	return &WriteFileTool{root: root}
}

func (t *WriteFileTool) Name() string {
	return writeFileToolName
}

func (t *WriteFileTool) Description() string {
	return "Write a file inside the current workspace, creating parent directories when needed."
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
		},
		"required": []string{"path", "content"},
	}
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

	base := effectiveRoot(t.root, input.Workdir)

	target, err := resolvePath(base, args.Path)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	if err := os.WriteFile(target, []byte(args.Content), 0o644); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	return tools.ToolResult{
		Name:    t.Name(),
		Content: "ok",
		Metadata: map[string]any{
			"path":  target,
			"bytes": len(args.Content),
		},
	}, nil
}
