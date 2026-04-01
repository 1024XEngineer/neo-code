package context

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	ruleFileName      = "AGENTS.md"
	maxRuleFileRunes  = 4000
	maxTotalRuleRunes = 12000
)

type ruleDocument struct {
	Path      string
	Content   string
	Truncated bool
}

func loadProjectRules(ctx context.Context, workdir string) ([]ruleDocument, error) {
	paths, err := discoverRuleFiles(ctx, workdir)
	if err != nil {
		return nil, err
	}

	return loadRuleDocuments(ctx, paths, os.ReadFile)
}

func loadRuleDocuments(ctx context.Context, paths []string, readFile func(string) ([]byte, error)) ([]ruleDocument, error) {
	documents := make([]ruleDocument, 0, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		data, err := readFile(path)
		if err != nil {
			return nil, fmt.Errorf("context: read %s: %w", path, err)
		}

		content, truncated := truncateRunes(strings.TrimSpace(string(data)), maxRuleFileRunes)
		documents = append(documents, ruleDocument{
			Path:      path,
			Content:   content,
			Truncated: truncated,
		})
	}

	return documents, nil
}

func discoverRuleFiles(ctx context.Context, workdir string) ([]string, error) {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return nil, nil
	}

	dir := filepath.Clean(workdir)
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	paths := make([]string, 0, 4)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		match, err := findExactRuleFile(dir)
		if err != nil {
			return nil, err
		}
		if match != "" {
			paths = append(paths, match)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}

	return paths, nil
}

func findExactRuleFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("context: read dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if entry.Name() == ruleFileName {
			return filepath.Join(dir, entry.Name()), nil
		}
	}

	return "", nil
}

func renderProjectRulesSection(documents []ruleDocument) string {
	if len(documents) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("## Project Rules\n")

	remaining := maxTotalRuleRunes
	totalTruncated := false
	for _, document := range documents {
		if remaining <= 0 {
			totalTruncated = true
			break
		}

		content := document.Content
		if runeCount(content) > remaining {
			content, _ = truncateRunes(content, remaining)
			totalTruncated = true
		}
		remaining -= runeCount(content)

		builder.WriteString("\n### ")
		builder.WriteString(document.Path)
		builder.WriteString("\n")
		if content != "" {
			builder.WriteString("\n")
			builder.WriteString(content)
			builder.WriteString("\n")
		}
		if document.Truncated {
			builder.WriteString("\n[truncated to fit per-file limit]\n")
		}

		if content != document.Content {
			totalTruncated = true
			break
		}
	}

	if totalTruncated {
		builder.WriteString("\n[additional project rules truncated to fit total limit]\n")
	}

	return strings.TrimSpace(builder.String())
}

func truncateRunes(input string, max int) (string, bool) {
	if max <= 0 {
		return "", input != ""
	}
	if runeCount(input) <= max {
		return input, false
	}

	runes := []rune(input)
	return string(runes[:max]), true
}

func runeCount(input string) int {
	return utf8.RuneCountInString(input)
}
