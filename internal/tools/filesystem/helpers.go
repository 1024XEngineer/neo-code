package filesystem

import (
	"os"
	"path/filepath"
	"strings"

	"neo-code/internal/tools"
)

const (
	readFileToolName   = tools.ToolNameFilesystemReadFile
	writeFileToolName  = tools.ToolNameFilesystemWriteFile
	grepToolName       = tools.ToolNameFilesystemGrep
	globToolName       = tools.ToolNameFilesystemGlob
	editToolName       = tools.ToolNameFilesystemEdit
	moveFileToolName   = tools.ToolNameFilesystemMoveFile
	copyFileToolName   = tools.ToolNameFilesystemCopyFile
	deleteFileToolName = tools.ToolNameFilesystemDeleteFile
	createDirToolName  = tools.ToolNameFilesystemCreateDir
	removeDirToolName  = tools.ToolNameFilesystemRemoveDir
)

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
