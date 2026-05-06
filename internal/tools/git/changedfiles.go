package git

import (
	"context"
	"encoding/json"

	"neo-code/internal/repository"
	"neo-code/internal/tools"
)

// ChangedFilesTool implements the git_changed_files tool.
type ChangedFilesTool struct {
	root string
}

// NewChangedFiles creates a new git_changed_files tool.
func NewChangedFiles(root string) *ChangedFilesTool {
	return &ChangedFilesTool{root: root}
}

func (t *ChangedFilesTool) Name() string {
	return tools.ToolNameGitChangedFiles
}

func (t *ChangedFilesTool) Description() string {
	return "List changed files in the current Git working tree with their status (modified, added, deleted, renamed, etc.)."
}

func (t *ChangedFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"workdir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of files to return (default 50, max 200).",
			},
		},
	}
}

func (t *ChangedFilesTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *ChangedFilesTool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var in struct {
		Workdir string `json:"workdir,omitempty"`
		Limit   int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(call.Arguments, &in); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}

	root := effectiveRoot(t.root, in.Workdir)
	svc := repository.NewService()
	opts := repository.ChangedFilesOptions{
		Limit: in.Limit,
	}
	result, err := svc.ChangedFiles(ctx, root, opts)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	files := make([]fileEntry, 0, len(result.Files))
	for _, f := range result.Files {
		files = append(files, fileEntry{
			Status:  string(f.Status),
			Path:    f.Path,
			OldPath: f.OldPath,
		})
	}

	content := formatFileList(files, result.TotalCount, result.Truncated)
	return tools.ToolResult{
		Name:    t.Name(),
		Content: content,
		Metadata: map[string]any{
			"returned_count": result.ReturnedCount,
			"total_count":    result.TotalCount,
			"truncated":      result.Truncated,
		},
	}, nil
}
