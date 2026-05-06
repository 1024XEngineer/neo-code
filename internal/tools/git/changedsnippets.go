package git

import (
	"context"
	"encoding/json"

	"neo-code/internal/repository"
	"neo-code/internal/tools"
)

// ChangedSnippetsTool implements the git_changed_snippets tool.
type ChangedSnippetsTool struct {
	root string
}

// NewChangedSnippets creates a new git_changed_snippets tool.
func NewChangedSnippets(root string) *ChangedSnippetsTool {
	return &ChangedSnippetsTool{root: root}
}

func (t *ChangedSnippetsTool) Name() string {
	return tools.ToolNameGitChangedSnippets
}

func (t *ChangedSnippetsTool) Description() string {
	return "List changed files with diff snippets. Useful when you need to see the actual content of modifications, not just file names."
}

func (t *ChangedSnippetsTool) Schema() map[string]any {
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
			"snippet_file_count_limit": map[string]any{
				"type":        "integer",
				"description": "If the total number of changed files exceeds this limit, snippets are omitted to avoid token bloat.",
			},
		},
	}
}

func (t *ChangedSnippetsTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *ChangedSnippetsTool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var in struct {
		Workdir                string `json:"workdir,omitempty"`
		Limit                  int    `json:"limit,omitempty"`
		SnippetFileCountLimit  int    `json:"snippet_file_count_limit,omitempty"`
	}
	if err := json.Unmarshal(call.Arguments, &in); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}

	root := effectiveRoot(t.root, in.Workdir)
	svc := repository.NewService()
	opts := repository.ChangedFilesOptions{
		Limit:                 in.Limit,
		IncludeSnippets:       true,
		SnippetFileCountLimit: in.SnippetFileCountLimit,
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
			Snippet: f.Snippet,
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
