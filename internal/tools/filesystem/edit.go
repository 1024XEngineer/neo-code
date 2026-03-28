package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"neo-code/internal/tools"
)

type EditTool struct {
	root string
}

type editInput struct {
	Path          string `json:"path"`
	SearchString  string `json:"search_string"`
	ReplaceString string `json:"replace_string"`
}

func NewEdit(root string) *EditTool {
	return &EditTool{root: root}
}

func (t *EditTool) Name() string {
	return editToolName
}

func (t *EditTool) Description() string {
	return "Replace exactly one matching code block in a file and write the updated content back to disk."
}

func (t *EditTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Target file path relative to the workspace root, or an absolute path inside the workspace.",
			},
			"search_string": map[string]any{
				"type":        "string",
				"description": "Exact string to find in the file. It must match exactly once.",
			},
			"replace_string": map[string]any{
				"type":        "string",
				"description": "Replacement content for the matched string.",
			},
		},
		"required": []string{"path", "search_string", "replace_string"},
	}
}

func (t *EditTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	var args editInput
	if err := json.Unmarshal(input.Arguments, &args); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}
	if strings.TrimSpace(args.Path) == "" {
		return tools.ToolResult{Name: t.Name()}, errors.New(editToolName + ": path is required")
	}
	if args.SearchString == "" {
		return tools.ToolResult{Name: t.Name()}, errors.New(editToolName + ": search_string is required")
	}
	if err := ctx.Err(); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	root := effectiveRoot(t.root, input.Workdir)
	target, err := resolvePath(root, args.Path)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	content := string(data)
	matches := strings.Count(content, args.SearchString)
	switch {
	case matches == 0:
		return tools.ToolResult{Name: t.Name()}, fmt.Errorf("%s: search_string not found in %s", editToolName, toRelativePath(root, target))
	case matches > 1:
		return tools.ToolResult{Name: t.Name()}, fmt.Errorf("%s: search_string matched %d locations in %s; refine it to a unique block", editToolName, matches, toRelativePath(root, target))
	}

	updated := strings.Replace(content, args.SearchString, args.ReplaceString, 1)
	if updated == content {
		return tools.ToolResult{Name: t.Name()}, fmt.Errorf("%s: replacement produced no changes", editToolName)
	}
	if err := ctx.Err(); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	if err := os.WriteFile(target, []byte(updated), 0o644); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	return tools.ToolResult{
		Name:    t.Name(),
		Content: "ok",
		Metadata: map[string]any{
			"path":               target,
			"relative_path":      normalizeSlashPath(toRelativePath(root, target)),
			"search_length":      len(args.SearchString),
			"replacement_length": len(args.ReplaceString),
		},
	}, nil
}
