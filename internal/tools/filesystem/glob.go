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

	"github.com/dust/neo-code/internal/tools"
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
		return tools.ToolResult{Name: t.Name()}, err
	}

	rawPattern := strings.TrimSpace(args.Pattern)
	if rawPattern == "" {
		return tools.ToolResult{Name: t.Name()}, errors.New(globToolName + ": pattern is required")
	}
	pattern := normalizeSlashPath(rawPattern)
	if err := ctx.Err(); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	root := effectiveRoot(t.root, input.Workdir)
	searchRoot, err := resolveSearchDir(root, args.Dir)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	matcher, err := buildGlobMatcher(pattern)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	matches := make([]string, 0, 32)
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
			matches = append(matches, normalizeSlashPath(toRelativePath(root, path)))
		}
		return nil
	})
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	sort.Strings(matches)
	if len(matches) == 0 {
		return tools.ToolResult{
			Name:    t.Name(),
			Content: "no matches",
			Metadata: map[string]any{
				"root":  searchRoot,
				"count": 0,
			},
		}, nil
	}

	return tools.ToolResult{
		Name:    t.Name(),
		Content: strings.Join(matches, "\n"),
		Metadata: map[string]any{
			"root":  searchRoot,
			"count": len(matches),
		},
	}, nil
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
