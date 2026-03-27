package filesystem

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"neocode/internal/tools"
)

// SearchTool searches text files under the configured workdir.
type SearchTool struct{}

// NewSearchTool constructs the search tool.
func NewSearchTool() *SearchTool {
	return &SearchTool{}
}

// Name returns the stable tool name.
func (t *SearchTool) Name() string {
	return "fs_search"
}

// Description describes the tool for the model.
func (t *SearchTool) Description() string {
	return "Search for a regular expression inside text files within the current workdir."
}

// Schema returns the JSON schema for tool arguments.
func (t *SearchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative directory path under the workdir. Defaults to current directory.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression to search for.",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "Optional filepath glob filter, for example *.go.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of matches to return.",
			},
		},
		"required": []string{"pattern"},
	}
}

// Execute performs the search.
func (t *SearchTool) Execute(ctx context.Context, call tools.Invocation) (tools.Result, error) {
	_ = ctx

	var args struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
		Glob    string `json:"glob"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return tools.Result{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Pattern == "" {
		return tools.Result{}, fmt.Errorf("pattern is required")
	}

	root, err := resolvePath(call.Workdir, args.Path)
	if err != nil {
		return tools.Result{}, err
	}

	info, err := os.Stat(root)
	if err != nil {
		return tools.Result{}, wrapPathError("stat", args.Path, err)
	}
	if !info.IsDir() {
		return tools.Result{}, fmt.Errorf("path %q is not a directory", args.Path)
	}

	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return tools.Result{}, fmt.Errorf("invalid regexp: %w", err)
	}

	limit := args.Limit
	if limit <= 0 || limit > defaultSearchHits {
		limit = defaultSearchHits
	}

	results := make([]string, 0, limit)
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if len(results) >= limit {
			return fs.SkipAll
		}
		if entry.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if args.Glob != "" {
			matched, err := filepath.Match(args.Glob, filepath.Base(path))
			if err != nil {
				return err
			}
			if !matched {
				return nil
			}
		}

		payload, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if len(payload) > maxSearchFileSize || bytes.IndexByte(payload, 0) >= 0 {
			return nil
		}

		scanner := bufio.NewScanner(bytes.NewReader(payload))
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d: %s", rel, lineNo, strings.TrimSpace(line)))
				if len(results) >= limit {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != fs.SkipAll {
		return tools.Result{}, walkErr
	}

	if len(results) == 0 {
		results = append(results, "[no matches]")
	}

	return tools.Result{
		Content: fmt.Sprintf("search root: %s\npattern: %s\n\n%s", root, args.Pattern, strings.Join(results, "\n")),
		Metadata: map[string]any{
			"path":    root,
			"pattern": args.Pattern,
			"matches": len(results),
		},
	}, nil
}
