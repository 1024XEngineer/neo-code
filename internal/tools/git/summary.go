package git

import (
	"context"
	"encoding/json"
	"strings"

	"neo-code/internal/repository"
	"neo-code/internal/tools"
)

// SummaryTool implements the git_summary tool.
type SummaryTool struct {
	root string
}

// NewSummary creates a new git_summary tool.
func NewSummary(root string) *SummaryTool {
	return &SummaryTool{root: root}
}

func (t *SummaryTool) Name() string {
	return tools.ToolNameGitSummary
}

func (t *SummaryTool) Description() string {
	return "Return a structured summary of the current Git repository state: branch, dirty status, ahead/behind counts, and changed file count."
}

func (t *SummaryTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"workdir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root.",
			},
		},
	}
}

func (t *SummaryTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *SummaryTool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var in struct {
		Workdir string `json:"workdir,omitempty"`
	}
	if err := json.Unmarshal(call.Arguments, &in); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}

	root := effectiveRoot(t.root, in.Workdir)
	svc := repository.NewService()
	summary, err := svc.Summary(ctx, root)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	content := formatSummary(summary)
	return tools.ToolResult{
		Name:    t.Name(),
		Content: content,
		Metadata: map[string]any{
			"in_git_repo": summary.InGitRepo,
			"branch":      summary.Branch,
			"dirty":       summary.Dirty,
			"ahead":       summary.Ahead,
			"behind":      summary.Behind,
		},
	}, nil
}

func formatSummary(s repository.Summary) string {
	var b strings.Builder
	b.WriteString("in_git_repo: ")
	b.WriteString(boolToString(s.InGitRepo))
	if !s.InGitRepo {
		return b.String()
	}
	b.WriteString("\nbranch: ")
	b.WriteString(s.Branch)
	b.WriteString("\ndirty: ")
	b.WriteString(boolToString(s.Dirty))
	b.WriteString("\nahead: ")
	b.WriteString(itoa(s.Ahead))
	b.WriteString("\nbehind: ")
	b.WriteString(itoa(s.Behind))
	b.WriteString("\nchanged_file_count: ")
	b.WriteString(itoa(s.ChangedFileCount))
	if len(s.RepresentativeChangedFiles) > 0 {
		b.WriteString("\nrepresentative_changed_files:")
		for _, path := range s.RepresentativeChangedFiles {
			b.WriteString("\n- ")
			b.WriteString(path)
		}
	}
	return b.String()
}
