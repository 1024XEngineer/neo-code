package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dust/neo-code/internal/tools"
)

const defaultGrepResultLimit = 200

var errGrepResultLimitReached = errors.New("filesystem_grep: result limit reached")

type GrepTool struct {
	root string
}

type grepInput struct {
	Pattern  string `json:"pattern"`
	Dir      string `json:"dir"`
	UseRegex bool   `json:"use_regex"`
}

func NewGrep(root string) *GrepTool {
	return &GrepTool{root: root}
}

func (t *GrepTool) Name() string {
	return grepToolName
}

func (t *GrepTool) Description() string {
	return "Search the workspace for matching text and return file paths, line numbers, and matching snippets."
}

func (t *GrepTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Literal text or a regular expression to search for.",
			},
			"dir": map[string]any{
				"type":        "string",
				"description": "Optional directory relative to the workspace root to scope the search.",
			},
			"use_regex": map[string]any{
				"type":        "boolean",
				"description": "When true, treat pattern as a regular expression. When false, perform a literal substring search.",
			},
		},
		"required": []string{"pattern"},
	}
}

func (t *GrepTool) Execute(ctx context.Context, input tools.ToolCallInput) (tools.ToolResult, error) {
	var args grepInput
	if err := json.Unmarshal(input.Arguments, &args); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	pattern := strings.TrimSpace(args.Pattern)
	if pattern == "" {
		return tools.ToolResult{Name: t.Name()}, errors.New(grepToolName + ": pattern is required")
	}
	if err := ctx.Err(); err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	root := effectiveRoot(t.root, input.Workdir)
	searchRoot, err := resolveSearchDir(root, args.Dir)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	matcher, err := buildGrepMatcher(pattern, args.UseRegex)
	if err != nil {
		return tools.ToolResult{Name: t.Name()}, err
	}

	var (
		results      []string
		matchedFiles int
	)
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

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		fileMatched := false
		for idx, line := range lines {
			if !matcher(line) {
				continue
			}
			fileMatched = true
			results = append(results, fmt.Sprintf("%s:%d: %s", toRelativePath(root, path), idx+1, strings.TrimRight(line, "\r")))
			if len(results) >= defaultGrepResultLimit {
				return errGrepResultLimitReached
			}
		}
		if fileMatched {
			matchedFiles++
		}
		return nil
	})
	if err != nil && !errors.Is(err, errGrepResultLimitReached) {
		return tools.ToolResult{Name: t.Name()}, err
	}

	if len(results) == 0 {
		return tools.ToolResult{
			Name:    t.Name(),
			Content: "no matches",
			Metadata: map[string]any{
				"root":          searchRoot,
				"matched_files": 0,
				"matched_lines": 0,
			},
		}, nil
	}

	return tools.ToolResult{
		Name:    t.Name(),
		Content: strings.Join(results, "\n"),
		Metadata: map[string]any{
			"root":          searchRoot,
			"matched_files": matchedFiles,
			"matched_lines": len(results),
		},
	}, nil
}

func buildGrepMatcher(pattern string, useRegex bool) (func(string) bool, error) {
	if !useRegex {
		return func(line string) bool {
			return strings.Contains(line, pattern)
		}, nil
	}

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid regex: %w", grepToolName, err)
	}
	return compiled.MatchString, nil
}

func resolveSearchDir(root string, dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return resolvePath(root, ".")
	}
	return resolvePath(root, dir)
}
