package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/dust/neo-code/internal/tools"
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
		return tools.ToolResult{Name: t.Name()}, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return tools.ToolResult{Name: t.Name()}, errors.New(writeFileToolName + ": path is required")
	}
	if err := ctx.Err(); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	base := effectiveRoot(t.root, input.Workdir)

	target, err := resolvePath(base, args.Path)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}
	if err := os.WriteFile(target, []byte(args.Content), 0o644); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
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
