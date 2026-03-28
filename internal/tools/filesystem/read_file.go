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
		return tools.ToolResult{Name: t.Name()}, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return tools.ToolResult{Name: t.Name()}, errors.New(readFileToolName + ": path is required")
	}

	base := effectiveRoot(t.root, input.Workdir)

	target, err := resolvePath(base, args.Path)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	if input.EmitChunk != nil && len(data) > emitChunkSize {
		for start := 0; start < len(data); start += emitChunkSize {
			end := start + emitChunkSize
			if end > len(data) {
				end = len(data)
			}
			input.EmitChunk(data[start:end])
		}
	}

	return tools.ToolResult{
		Name:    t.Name(),
		Content: string(data),
		Metadata: map[string]any{
			"path": target,
		},
	}, nil
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
