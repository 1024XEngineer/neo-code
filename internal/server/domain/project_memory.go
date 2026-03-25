package domain

import "context"

// ProjectMemorySource represents an explicit memory file loaded from the workspace.
type ProjectMemorySource struct {
	Path    string
	Content string
}

// ProjectMemoryService loads explicit project memory files and formats them for prompt injection.
type ProjectMemoryService interface {
	BuildContext(ctx context.Context) (string, error)
	ListSources(ctx context.Context) ([]ProjectMemorySource, error)
}
