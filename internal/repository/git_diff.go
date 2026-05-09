package repository

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"neo-code/internal/security"
)

const defaultGitDiffPreviewLimitBytes int64 = 512 * 1024

// GitBlobReader 定义从 Git 对象数据库读取 blob 内容的能力。
type GitBlobReader func(ctx context.Context, workdir string, spec string) ([]byte, error)

// GitBlobSizer 定义查询 Git blob 大小的能力。
type GitBlobSizer func(ctx context.Context, workdir string, spec string) (int64, error)

// ReadGitDiffFile 读取工作树相对于 HEAD 的单文件双文本差异预览。
func (s *Service) ReadGitDiffFile(ctx context.Context, workdir string, path string, maxBytes int64) (GitDiffFileResult, error) {
	if err := ctx.Err(); err != nil {
		return GitDiffFileResult{}, err
	}
	if s == nil {
		return GitDiffFileResult{}, fmt.Errorf("repository: service is nil")
	}
	root, _, err := security.ResolveWorkspacePath(workdir, ".")
	if err != nil {
		return GitDiffFileResult{}, err
	}
	snapshot, err := s.loadGitSnapshot(ctx, root)
	if err != nil {
		return GitDiffFileResult{}, err
	}
	if !snapshot.InGitRepo {
		return GitDiffFileResult{}, fmt.Errorf("repository: workdir is not a git repository")
	}

	normalizedPath := filepathSlashClean(path)
	entry, ok := findGitDiffEntry(snapshot.Entries, normalizedPath)
	if !ok {
		if isDirectoryGitDiffPath(root, normalizedPath) {
			return GitDiffFileResult{}, fmt.Errorf("repository: git diff path %q is a directory", normalizedPath)
		}
		return GitDiffFileResult{}, fmt.Errorf("repository: git diff file %q not found", normalizedPath)
	}
	limit := maxBytes
	if limit <= 0 {
		limit = defaultGitDiffPreviewLimitBytes
	}

	result := GitDiffFileResult{
		Path:     entry.Path,
		OldPath:  entry.OldPath,
		Status:   entry.Status,
		Encoding: "utf-8",
	}
	originalPath, modifiedPath := resolveGitDiffPaths(entry)

	if originalPath != "" {
		originalResult, readErr := s.readGitDiffOriginal(ctx, root, originalPath, limit)
		if readErr != nil {
			return GitDiffFileResult{}, readErr
		}
		result.OriginalContent = originalResult.content
		result.OriginalSize = originalResult.size
		result.IsBinary = result.IsBinary || originalResult.isBinary
		result.Truncated = result.Truncated || originalResult.truncated
	}
	if modifiedPath != "" {
		modifiedResult, readErr := s.readGitDiffModified(root, modifiedPath, limit)
		if readErr != nil {
			return GitDiffFileResult{}, readErr
		}
		result.ModifiedContent = modifiedResult.content
		result.ModifiedSize = modifiedResult.size
		result.IsBinary = result.IsBinary || modifiedResult.isBinary
		result.Truncated = result.Truncated || modifiedResult.truncated
	}
	if result.IsBinary {
		result.Encoding = "binary"
		result.OriginalContent = ""
		result.ModifiedContent = ""
	}
	if result.Truncated {
		result.OriginalContent = ""
		result.ModifiedContent = ""
	}
	return result, nil
}

type gitDiffReadResult struct {
	content   string
	size      int64
	isBinary  bool
	truncated bool
}

// findGitDiffEntry 从 git 状态快照中查找目标文件条目。
func findGitDiffEntry(entries []gitChangedEntry, path string) (gitChangedEntry, bool) {
	for _, entry := range entries {
		if filepathSlashClean(entry.Path) == path {
			return entry, true
		}
	}
	return gitChangedEntry{}, false
}

// resolveGitDiffPaths 根据变更状态推导原始文件与工作树文件的相对路径。
func resolveGitDiffPaths(entry gitChangedEntry) (string, string) {
	switch entry.Status {
	case StatusAdded, StatusUntracked:
		return "", entry.Path
	case StatusDeleted:
		return entry.Path, ""
	case StatusRenamed, StatusCopied:
		return entry.OldPath, entry.Path
	case StatusModified, StatusConflicted:
		return entry.Path, entry.Path
	default:
		return entry.Path, entry.Path
	}
}

// readGitDiffOriginal 读取 HEAD 中对应文件的原始文本。
func (s *Service) readGitDiffOriginal(ctx context.Context, workdir string, relativePath string, maxBytes int64) (gitDiffReadResult, error) {
	spec := buildGitHeadBlobSpec(relativePath)
	size, err := s.gitBlobSizer(ctx, workdir, spec)
	if err != nil {
		return gitDiffReadResult{}, err
	}
	result := gitDiffReadResult{size: size}
	if size > maxBytes {
		result.truncated = true
		return result, nil
	}
	data, err := s.gitBlobReader(ctx, workdir, spec)
	if err != nil {
		return gitDiffReadResult{}, err
	}
	if isBinaryContent(data) {
		result.isBinary = true
		return result, nil
	}
	result.content = string(data)
	return result, nil
}

// readGitDiffModified 读取工作树中的当前文本。
func (s *Service) readGitDiffModified(workdir string, relativePath string, maxBytes int64) (gitDiffReadResult, error) {
	target, err := resolveUntrackedEntryPath(workdir, relativePath)
	if err != nil {
		return gitDiffReadResult{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return gitDiffReadResult{}, err
	}
	if info.IsDir() {
		return gitDiffReadResult{}, fmt.Errorf("repository: %q is a directory", relativePath)
	}
	result := gitDiffReadResult{size: info.Size()}
	if info.Size() > maxBytes {
		result.truncated = true
		return result, nil
	}
	data, err := s.readFile(target)
	if err != nil {
		return gitDiffReadResult{}, err
	}
	if isBinaryContent(data) {
		result.isBinary = true
		return result, nil
	}
	result.content = string(data)
	return result, nil
}

// isDirectoryGitDiffPath 判断调用方请求的 Git Diff 路径当前是否是工作区目录，用于返回更明确的错误语义。
func isDirectoryGitDiffPath(workdir string, relativePath string) bool {
	target, err := resolveUntrackedEntryPath(workdir, relativePath)
	if err != nil {
		return false
	}
	info, err := os.Stat(target)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// buildGitHeadBlobSpec 构造 HEAD blob 读取使用的对象说明。
func buildGitHeadBlobSpec(path string) string {
	normalized := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(path)), "./")
	return "HEAD:" + normalized
}

// readGitBlob 读取 Git 对象数据库中的 blob 内容。
func readGitBlob(ctx context.Context, workdir string, spec string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", workdir, "show", "--no-textconv", spec)
	return cmd.Output()
}

// statGitBlob 查询 Git 对象数据库中 blob 的字节大小。
func statGitBlob(ctx context.Context, workdir string, spec string) (int64, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", workdir, "cat-file", "-s", spec)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	size, parseErr := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if parseErr != nil {
		return 0, parseErr
	}
	return size, nil
}
