package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"neo-code/internal/tools"
)

type GlobTool struct {
	root string
}

type globInput struct {
	Pattern string `json:"pattern"`
	Dir     string `json:"dir"`
}

func NewGlob(root string) *GlobTool {
	return &GlobTool{root: root}
}

func (t *GlobTool) Name() string {
	return globToolName
}

func (t *GlobTool) Description() string {
	return "List workspace file paths that match a glob pattern such as **/*.go."
}

func (t *GlobTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern to match relative file paths, for example **/*.go or internal/**/*.md.",
			},
			"dir": map[string]any{
				"type":        "string",
				"description": "Optional directory relative to the workspace root to scope the search.",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *GlobTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	var args globInput
	if err := json.Unmarshal(input.Arguments, &args); err != nil {
		return tools.NewErrorResult(t.Name(), "invalid arguments", err.Error(), nil), err
	}

	rawPattern := strings.TrimSpace(args.Pattern)
	if rawPattern == "" {
		err := errors.New(globToolName + ": pattern is required")
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	pattern := normalizeSlashPath(rawPattern)
	if err := ctx.Err(); err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	root := effectiveRoot(t.root, input.Workdir)
	searchRoot, err := resolveSearchDir(root, args.Dir)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}
	filter, err := newResultPathFilter(root)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	matcher, err := buildGlobMatcher(pattern)
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	matches := make([]string, 0, 32)
	matchedCount := 0
	filteredCount := 0
	filteredReasons := map[string]int{}
	err = filepath.WalkDir(searchRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if skipDirEntry(path, entry) {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}

		relativeToSearch, err := filepath.Rel(searchRoot, path)
		if err != nil {
			return nil
		}
		if matcher.MatchString(normalizeSlashPath(relativeToSearch)) {
			matchedCount++
			relativePath, reason, allowed := filter.evaluate(path)
			if !allowed {
				filteredCount++
				filteredReasons[reason]++
				return nil
			}
			matches = append(matches, relativePath)
		}
		return nil
	})
	if err != nil {
		return tools.NewErrorResult(t.Name(), tools.NormalizeErrorReason(t.Name(), err), "", nil), err
	}

	sort.Strings(matches)
	if len(matches) == 0 {
		return tools.ToolResult{
			Name:    t.Name(),
			Content: "no matches",
			Metadata: map[string]any{
				"root":             searchRoot,
				"count":            0,
				"matched_count":    matchedCount,
				"filtered_count":   filteredCount,
				"returned_count":   0,
				"filtered_reasons": filteredReasons,
			},
		}, nil
	}

	result := tools.ToolResult{
		Name:    t.Name(),
		Content: strings.Join(matches, "\n"),
		Metadata: map[string]any{
			"root":             searchRoot,
			"count":            len(matches),
			"matched_count":    matchedCount,
			"filtered_count":   filteredCount,
			"returned_count":   len(matches),
			"filtered_reasons": filteredReasons,
		},
	}
	result = tools.ApplyOutputLimit(result, tools.DefaultOutputLimitBytes)
	return result, nil
}

func buildGlobMatcher(pattern string) (*regexp.Regexp, error) {
	var builder strings.Builder
	builder.WriteString("^")
	for idx := 0; idx < len(pattern); idx++ {
		ch := pattern[idx]
		switch ch {
		case '*':
			if idx+1 < len(pattern) && pattern[idx+1] == '*' {
				if idx+2 < len(pattern) && pattern[idx+2] == '/' {
					builder.WriteString(`(?:.*/)?`)
					idx += 2
					continue
				}
				builder.WriteString(".*")
				idx++
				continue
			}
			builder.WriteString(`[^/]*`)
		case '?':
			builder.WriteString(`[^/]`)
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			builder.WriteByte('\\')
			builder.WriteByte(ch)
		default:
			builder.WriteByte(ch)
		}
	}
	builder.WriteString("$")
	return regexp.Compile(builder.String())
}

func normalizeSlashPath(value string) string {
	return strings.ReplaceAll(filepath.Clean(value), `\`, "/")
}
