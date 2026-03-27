package filesystem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxReadBytes      = 64 * 1024
	maxSearchFileSize = 256 * 1024
	defaultListLimit  = 200
	defaultSearchHits = 100
	maxWriteBytes     = 256 * 1024
)

func resolvePath(workdir, target string) (string, error) {
	base, err := filepath.Abs(workdir)
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}

	candidate := strings.TrimSpace(target)
	if candidate == "" {
		candidate = "."
	}

	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, candidate)
	}

	candidate = filepath.Clean(candidate)

	rel, err := filepath.Rel(base, candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", target, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes workdir", target)
	}

	return candidate, nil
}

func wrapPathError(op, target string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("path %q does not exist", target)
	}
	return fmt.Errorf("%s path %q: %w", op, target, err)
}

func truncateString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}

	return value[:limit] + "\n\n[truncated]"
}
