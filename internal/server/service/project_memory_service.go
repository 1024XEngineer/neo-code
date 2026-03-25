package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-llm-demo/internal/server/domain"
)

type projectMemoryServiceImpl struct {
	workspaceRoot  string
	files          []string
	maxPromptChars int
}

// NewProjectMemoryService creates a service for loading explicit workspace memory files.
func NewProjectMemoryService(workspaceRoot string, files []string, maxPromptChars int) domain.ProjectMemoryService {
	cleaned := make([]string, 0, len(files))
	seen := map[string]struct{}{}
	for _, item := range files {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(item))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, item)
	}
	if maxPromptChars <= 0 {
		maxPromptChars = 2400
	}
	return &projectMemoryServiceImpl{
		workspaceRoot:  strings.TrimSpace(workspaceRoot),
		files:          cleaned,
		maxPromptChars: maxPromptChars,
	}
}

// BuildContext formats explicit project memory files into a prompt block.
func (s *projectMemoryServiceImpl) BuildContext(ctx context.Context) (string, error) {
	sources, err := s.ListSources(ctx)
	if err != nil {
		return "", err
	}
	if len(sources) == 0 {
		return "", nil
	}

	header := "Use the following explicit project memory files as authoritative project instructions and conventions. Prefer them over inferred memory when they conflict.\n"
	var builder strings.Builder
	builder.WriteString(header)

	for _, source := range sources {
		block := fmt.Sprintf("Project memory file: %s\n%s\n", source.Path, strings.TrimSpace(source.Content))
		if s.maxPromptChars > 0 && builder.Len()+len(block) > s.maxPromptChars {
			remaining := s.maxPromptChars - builder.Len()
			if remaining <= 0 {
				break
			}
			builder.WriteString(domain.SummarizeText(block, remaining))
			break
		}
		builder.WriteString(block)
		builder.WriteString("\n")
	}

	return strings.TrimSpace(builder.String()), nil
}

// ListSources returns the workspace memory files that currently exist.
func (s *projectMemoryServiceImpl) ListSources(ctx context.Context) ([]domain.ProjectMemorySource, error) {
	_ = ctx
	if strings.TrimSpace(s.workspaceRoot) == "" || len(s.files) == 0 {
		return nil, nil
	}

	sources := make([]domain.ProjectMemorySource, 0, len(s.files))
	for _, item := range s.files {
		fullPath, ok := s.resolveProjectFile(item)
		if !ok {
			continue
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}

		relPath, err := filepath.Rel(s.workspaceRoot, fullPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			relPath = fullPath
		}
		relPath = filepath.ToSlash(relPath)
		sources = append(sources, domain.ProjectMemorySource{
			Path:    relPath,
			Content: content,
		})
	}

	return sources, nil
}

func (s *projectMemoryServiceImpl) resolveProjectFile(item string) (string, bool) {
	if strings.TrimSpace(s.workspaceRoot) == "" {
		return "", false
	}

	root := filepath.Clean(s.workspaceRoot)
	if filepath.IsAbs(item) {
		cleaned := filepath.Clean(item)
		if cleaned == root || strings.HasPrefix(strings.ToLower(cleaned), strings.ToLower(root+string(filepath.Separator))) {
			return cleaned, true
		}
		return "", false
	}

	fullPath := filepath.Clean(filepath.Join(root, item))
	if fullPath == root || strings.HasPrefix(strings.ToLower(fullPath), strings.ToLower(root+string(filepath.Separator))) {
		return fullPath, true
	}
	return "", false
}
