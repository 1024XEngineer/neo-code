package filesystem

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	readFileToolName  = "filesystem_read_file"
	writeFileToolName = "filesystem_write_file"
	grepToolName      = "filesystem_grep"
	globToolName      = "filesystem_glob"
	editToolName      = "filesystem_edit"
)

func effectiveRoot(defaultRoot string, workdir string) string {
	base := strings.TrimSpace(workdir)
	if base == "" {
		base = defaultRoot
	}
	return base
}

func toRelativePath(root string, target string) string {
	base, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(target)
	}

	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return filepath.Clean(target)
	}

	rel, err := filepath.Rel(base, absoluteTarget)
	if err != nil {
		return filepath.Clean(target)
	}

	return filepath.Clean(rel)
}

func skipDirEntry(path string, entry os.DirEntry) bool {
	if !entry.IsDir() {
		return false
	}

	name := strings.ToLower(strings.TrimSpace(entry.Name()))
	switch name {
	case ".git", ".idea", ".vscode", "node_modules":
		return true
	}

	return false
}
