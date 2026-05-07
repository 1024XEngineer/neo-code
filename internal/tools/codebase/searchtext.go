package codebase

import (
	"context"
	"encoding/json"
	"strings"

	"neo-code/internal/repository"
	"neo-code/internal/tools"
)

// SearchTextTool implements the codebase_search_text tool.
type SearchTextTool struct {
	root string
	svc  *repository.Service
}

// NewSearchText creates a new codebase_search_text tool.
func NewSearchText(svc *repository.Service, root string) *SearchTextTool {
	return &SearchTextTool{root: root, svc: svc}
}

func (t *SearchTextTool) Name() string {
	return tools.ToolNameCodebaseSearchText
}

func (t *SearchTextTool) Description() string {
	return "Search for text occurrences across the workspace. Returns file paths, line hints, and match counts. Does NOT return code snippets; use codebase_read to view content."
}

func (t *SearchTextTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Text to search for.",
			},
			"scope_dir": map[string]any{
				"type":        "string",
				"description": "Optional subdirectory to limit the search scope.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of hits to return (default 20, max 50).",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Optional working directory relative to the workspace root.",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchTextTool) MicroCompactPolicy() tools.MicroCompactPolicy {
	return tools.MicroCompactPolicyCompact
}

func (t *SearchTextTool) Execute(ctx context.Context, call tools.ToolCallInput) (tools.ToolResult, error) {
	var in struct {
		Query    string `json:"query"`
		ScopeDir string `json:"scope_dir,omitempty"`
		Limit    int    `json:"limit,omitempty"`
		Workdir  string `json:"workdir,omitempty"`
	}
	if err := json.Unmarshal(call.Arguments, &in); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}
	if strings.TrimSpace(in.Query) == "" {
		return tools.NewErrorResult(t.Name(), "missing required argument: query", "", nil), nil
	}

	root, err := tools.ResolveEffectiveRoot(t.root, in.Workdir)
	if err != nil {
		return tools.NewErrorResult(t.Name(), "invalid workdir", err.Error(), nil), err
	}
	opts := repository.SearchOptions{
		ScopeDir: in.ScopeDir,
		Limit:    in.Limit,
	}
	result, err := t.svc.SearchText(ctx, root, in.Query, opts)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	content := formatTextSearchResult(result)
	return tools.ToolResult{
		Name:    t.Name(),
		Content: content,
		Metadata: map[string]any{
			"returned_count": len(result.Hits),
			"total_count":    result.TotalCount,
			"truncated":      result.Truncated,
		},
	}, nil
}

func formatTextSearchResult(r repository.TextSearchResult) string {
	var b strings.Builder
	b.WriteString("returned_count: ")
	b.WriteString(itoa(len(r.Hits)))
	b.WriteString("\ntotal_count: ")
	b.WriteString(itoa(r.TotalCount))
	b.WriteString("\ntruncated: ")
	b.WriteString(boolToString(r.Truncated))
	if len(r.Hits) > 0 {
		b.WriteString("\n")
	}
	for _, h := range r.Hits {
		b.WriteString("\n- path: ")
		b.WriteString(h.Path)
		b.WriteString("\n  line_hint: ")
		b.WriteString(itoa(h.LineHint))
		b.WriteString("\n  match_count: ")
		b.WriteString(itoa(h.MatchCount))
	}
	return b.String()
}
