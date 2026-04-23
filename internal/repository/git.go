package repository

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	gitCommandTimeout               = 5 * time.Second
	representativeChangedFilesLimit = 10
	defaultChangedFilesLimit        = 50
	maxChangedFilesLimit            = 200
	maxChangedSnippetLinesPerFile   = 20
	maxChangedSnippetTotalLines     = 200
)

type gitCommandRunner func(ctx context.Context, workdir string, args ...string) (string, error)

type gitSnapshot struct {
	InGitRepo bool
	Branch    string
	Ahead     int
	Behind    int
	Entries   []gitChangedEntry
}

type gitChangedEntry struct {
	Path    string
	OldPath string
	Status  ChangedFileStatus
}

// loadGitSnapshot 统一读取一次 git 状态快照，供摘要与变更上下文复用。
func (s *Service) loadGitSnapshot(ctx context.Context, workdir string) (gitSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return gitSnapshot{}, err
	}
	if strings.TrimSpace(workdir) == "" || s == nil || s.gitRunner == nil {
		return gitSnapshot{}, nil
	}

	output, err := s.gitRunner(ctx, workdir, "status", "--porcelain=v1", "--branch", "--untracked-files=normal")
	if err != nil {
		if isContextError(err) {
			return gitSnapshot{}, err
		}
		if isNotGitRepository(output, err) {
			return gitSnapshot{}, nil
		}
		return gitSnapshot{}, err
	}

	return parseGitSnapshot(output), nil
}

// changedFileSnippet 按固定语义为单个变更条目生成受限片段。
func (s *Service) changedFileSnippet(ctx context.Context, workdir string, entry gitChangedEntry) (snippetResult, error) {
	switch entry.Status {
	case StatusDeleted, StatusConflicted:
		return snippetResult{}, nil
	case StatusModified, StatusRenamed:
		return s.readDiffSnippet(ctx, workdir, entry.Path)
	case StatusAdded:
		snippet, err := s.readDiffSnippet(ctx, workdir, entry.Path)
		if err != nil {
			return snippetResult{}, err
		}
		if snippet.text != "" {
			return snippet, nil
		}
		return s.readFileHeadSnippet(workdir, entry.Path)
	case StatusUntracked:
		return s.readFileHeadSnippet(workdir, entry.Path)
	default:
		return snippetResult{}, nil
	}
}

// readDiffSnippet 读取单文件 patch 并裁剪为受限片段。
func (s *Service) readDiffSnippet(ctx context.Context, workdir string, path string) (snippetResult, error) {
	if s == nil || s.gitRunner == nil {
		return snippetResult{}, nil
	}
	_, target, err := resolveWorkspacePath(workdir, path)
	if err != nil {
		return snippetResult{}, err
	}
	allowed, err := allowRepositorySnippetByPath(target)
	if err != nil {
		return snippetResult{}, err
	}
	if !allowed {
		return snippetResult{}, nil
	}
	output, err := s.gitRunner(ctx, workdir, "diff", "--unified=3", "HEAD", "--", filepath.ToSlash(path))
	if err != nil {
		if isContextError(err) {
			return snippetResult{}, err
		}
		return snippetResult{}, err
	}
	return trimSnippetText(output, maxChangedSnippetLinesPerFile), nil
}

// readFileHeadSnippet 读取工作树文件头部片段，供新增或未跟踪文件回退使用。
func (s *Service) readFileHeadSnippet(workdir string, relativePath string) (snippetResult, error) {
	if s == nil || s.readFile == nil {
		return snippetResult{}, nil
	}
	_, target, err := resolveWorkspacePath(workdir, relativePath)
	if err != nil {
		return snippetResult{}, err
	}
	allowed, err := allowRepositorySnippetByPath(target)
	if err != nil {
		return snippetResult{}, err
	}
	if !allowed {
		return snippetResult{}, nil
	}
	content, err := s.readFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return snippetResult{}, nil
		}
		return snippetResult{}, err
	}
	if isBinaryContent(content) {
		return snippetResult{}, nil
	}
	return trimSnippetText(string(content), maxChangedSnippetLinesPerFile), nil
}

// parseGitSnapshot 将 porcelain=v1 --branch 输出归一化为内部快照。
func parseGitSnapshot(output string) gitSnapshot {
	lines := splitNonEmptyLines(output)
	if len(lines) == 0 {
		return gitSnapshot{}
	}

	snapshot := gitSnapshot{InGitRepo: true}
	if strings.HasPrefix(lines[0], "## ") {
		snapshot.Branch, snapshot.Ahead, snapshot.Behind = parseBranchLine(strings.TrimPrefix(lines[0], "## "))
		lines = lines[1:]
	}

	snapshot.Entries = make([]gitChangedEntry, 0, len(lines))
	for _, line := range lines {
		entry, ok := parseChangedEntry(line)
		if ok {
			snapshot.Entries = append(snapshot.Entries, entry)
		}
	}
	return snapshot
}

// parseBranchLine 解析分支头信息中的分支名与 ahead/behind 计数。
func parseBranchLine(line string) (string, int, int) {
	line = strings.TrimSpace(line)
	switch {
	case line == "":
		return "", 0, 0
	case strings.HasPrefix(line, "No commits yet on "):
		return strings.TrimSpace(strings.TrimPrefix(line, "No commits yet on ")), 0, 0
	case strings.HasPrefix(line, "HEAD "):
		return "detached", 0, 0
	default:
		ahead, behind := parseTrackingCounters(line)
		if index := strings.Index(line, "..."); index >= 0 {
			line = line[:index]
		}
		if index := strings.Index(line, " ["); index >= 0 {
			line = line[:index]
		}
		return strings.TrimSpace(line), ahead, behind
	}
}

// parseTrackingCounters 提取 branch header 中的 ahead/behind 数值。
func parseTrackingCounters(line string) (int, int) {
	start := strings.Index(line, "[")
	end := strings.LastIndex(line, "]")
	if start < 0 || end <= start {
		return 0, 0
	}
	segment := strings.TrimSpace(line[start+1 : end])
	if segment == "" {
		return 0, 0
	}

	ahead := 0
	behind := 0
	for _, part := range strings.Split(segment, ",") {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) != 2 {
			continue
		}
		value, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "ahead":
			ahead = value
		case "behind":
			behind = value
		}
	}
	return ahead, behind
}

// parseChangedEntry 将 porcelain 行归一化为单个变更条目。
func parseChangedEntry(line string) (gitChangedEntry, bool) {
	if len(line) < 3 {
		return gitChangedEntry{}, false
	}
	x := line[0]
	y := line[1]
	pathPart := strings.TrimSpace(line[3:])
	if x == '?' && y == '?' {
		if pathPart == "" {
			return gitChangedEntry{}, false
		}
		return gitChangedEntry{Path: filepathSlashClean(pathPart), Status: StatusUntracked}, true
	}

	status := normalizeStatus(x, y)
	if status == "" {
		return gitChangedEntry{}, false
	}

	entry := gitChangedEntry{Status: status}
	if status == StatusRenamed && strings.Contains(pathPart, " -> ") {
		parts := strings.SplitN(pathPart, " -> ", 2)
		entry.OldPath = filepathSlashClean(strings.TrimSpace(parts[0]))
		entry.Path = filepathSlashClean(strings.TrimSpace(parts[1]))
	} else {
		entry.Path = filepathSlashClean(pathPart)
	}
	if entry.Path == "" {
		return gitChangedEntry{}, false
	}
	return entry, true
}

// normalizeStatus 将 porcelain 的 XY 状态对映射为稳定的归一化状态。
func normalizeStatus(x byte, y byte) ChangedFileStatus {
	pair := string([]byte{x, y})
	if strings.ContainsAny(pair, "U") || pair == "AA" || pair == "DD" {
		return StatusConflicted
	}
	if x == 'R' || y == 'R' {
		return StatusRenamed
	}
	if x == 'D' || y == 'D' {
		return StatusDeleted
	}
	if x == 'A' || y == 'A' {
		return StatusAdded
	}
	if x == 'M' || y == 'M' || x == 'T' || y == 'T' || x == 'C' || y == 'C' {
		return StatusModified
	}
	return ""
}

// runGitCommand 统一执行 git 子命令，并在超时后主动取消。
func runGitCommand(ctx context.Context, workdir string, args ...string) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	command := exec.CommandContext(timeoutCtx, "git", append([]string{"-C", workdir}, args...)...)
	output, err := command.CombinedOutput()
	return string(output), err
}

// isNotGitRepository 判断命令失败是否只是因为当前目录不是 git 仓库。
func isNotGitRepository(output string, err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(output))
	if strings.Contains(message, "not a git repository") {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "not a git repository")
}

// isContextError 用于保留上下文取消与超时等主链路错误语义。
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
