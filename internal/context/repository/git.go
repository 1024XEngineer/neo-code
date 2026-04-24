package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	maxChangedDiffBytes             = 64 * 1024
)

type gitCommandOptions struct {
	MaxOutputBytes int
}

type gitCommandOutput struct {
	text      string
	truncated bool
}

type gitCommandRunner func(ctx context.Context, workdir string, opts gitCommandOptions, args ...string) (gitCommandOutput, error)

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

	output, err := s.gitRunner(ctx, workdir, gitCommandOptions{}, "status", "--porcelain=v1", "-z", "--branch", "--untracked-files=normal")
	if err != nil {
		if isContextError(err) {
			return gitSnapshot{}, err
		}
		if isNotGitRepository(output.text, err) || isAmbiguousGitStatusOutsideRepo(workdir, output.text, err) {
			return gitSnapshot{}, nil
		}
		return gitSnapshot{}, err
	}

	return parseGitSnapshot(output.text), nil
}

// changedFileSnippet 按固定语义为单个变更条目生成受限片段。
func (s *Service) changedFileSnippet(ctx context.Context, workdir string, entry gitChangedEntry) (snippetResult, error) {
	switch entry.Status {
	case StatusDeleted, StatusConflicted:
		return snippetResult{}, nil
	case StatusModified, StatusRenamed, StatusCopied:
		return s.readDiffSnippet(ctx, workdir, entry.Path)
	case StatusAdded:
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
	_, _, allowed, err := resolveRepositorySnippetFile(workdir, path)
	if err != nil {
		return snippetResult{}, err
	}
	if !allowed {
		return snippetResult{}, nil
	}
	output, err := s.gitRunner(ctx, workdir, gitCommandOptions{MaxOutputBytes: maxChangedDiffBytes}, "diff", "--unified=3", "HEAD", "--", filepath.ToSlash(path))
	if err != nil {
		if isContextError(err) {
			return snippetResult{}, err
		}
		return snippetResult{}, err
	}
	snippet := trimSnippetText(output.text, maxChangedSnippetLinesPerFile)
	if output.truncated {
		snippet.truncated = true
	}
	return snippet, nil
}

// readFileHeadSnippet 读取工作树文件头部片段，供新增或未跟踪文件回退使用。
func (s *Service) readFileHeadSnippet(workdir string, relativePath string) (snippetResult, error) {
	if s == nil || s.readFile == nil {
		return snippetResult{}, nil
	}
	target, _, allowed, err := resolveRepositorySnippetFile(workdir, relativePath)
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

// parseGitSnapshot 将 porcelain v1 -z 输出归一化为内部快照。
func parseGitSnapshot(output string) gitSnapshot {
	records := splitNulRecords(output)
	if len(records) == 0 {
		return gitSnapshot{}
	}

	snapshot := gitSnapshot{InGitRepo: true}
	if strings.HasPrefix(records[0], "## ") {
		snapshot.Branch, snapshot.Ahead, snapshot.Behind = parseBranchLine(strings.TrimPrefix(records[0], "## "))
		records = records[1:]
	}

	snapshot.Entries = make([]gitChangedEntry, 0, len(records))
	for index := 0; index < len(records); index++ {
		entry, consumed, ok := parseChangedRecord(records[index:])
		if ok {
			snapshot.Entries = append(snapshot.Entries, entry)
			index += consumed - 1
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

// splitNulRecords 按 NUL record 拆分 -z 输出，并忽略尾部空 record。
func splitNulRecords(output string) []string {
	records := strings.Split(output, "\x00")
	for len(records) > 0 && records[len(records)-1] == "" {
		records = records[:len(records)-1]
	}
	return records
}

// parseChangedRecord 将单条或双条 -z record 归一化为结构化变更条目。
func parseChangedRecord(records []string) (gitChangedEntry, int, bool) {
	if len(records) == 0 || len(records[0]) < 4 {
		return gitChangedEntry{}, 1, false
	}
	record := records[0]
	x := record[0]
	y := record[1]
	pathPart := filepathSlashClean(record[3:])
	if x == '?' && y == '?' {
		if pathPart == "" {
			return gitChangedEntry{}, 1, false
		}
		return gitChangedEntry{Path: pathPart, Status: StatusUntracked}, 1, true
	}

	status := normalizeStatus(x, y)
	if status == "" {
		return gitChangedEntry{}, 1, false
	}

	entry := gitChangedEntry{Status: status}
	if status == StatusRenamed || status == StatusCopied {
		if len(records) < 2 {
			return gitChangedEntry{}, 1, false
		}
		entry.Path = pathPart
		entry.OldPath = filepathSlashClean(records[1])
		if entry.Path == "" || entry.OldPath == "" {
			return gitChangedEntry{}, 2, false
		}
		return entry, 2, true
	}
	if pathPart == "" {
		return gitChangedEntry{}, 1, false
	}
	entry.Path = pathPart
	return entry, 1, true
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
	if x == 'C' || y == 'C' {
		return StatusCopied
	}
	if x == 'D' || y == 'D' {
		return StatusDeleted
	}
	if x == 'A' || y == 'A' {
		return StatusAdded
	}
	if x == 'M' || y == 'M' || x == 'T' || y == 'T' {
		return StatusModified
	}
	return ""
}

// runGitCommand 统一执行 git 子命令，并在超时后主动取消。
func runGitCommand(ctx context.Context, workdir string, opts gitCommandOptions, args ...string) (gitCommandOutput, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	command := exec.CommandContext(timeoutCtx, "git", append([]string{"-C", workdir}, args...)...)
	buffer := &gitOutputBuffer{maxBytes: opts.MaxOutputBytes}
	command.Stdout = buffer
	command.Stderr = io.MultiWriter(buffer)
	err := command.Run()
	return gitCommandOutput{text: buffer.String(), truncated: buffer.truncated}, err
}

type gitOutputBuffer struct {
	bytes.Buffer
	maxBytes  int
	truncated bool
}

func (b *gitOutputBuffer) Write(p []byte) (int, error) {
	if b.maxBytes > 0 {
		remaining := b.maxBytes - b.Len()
		if remaining > 0 {
			if len(p) > remaining {
				_, _ = b.Buffer.Write(p[:remaining])
				b.truncated = true
			} else {
				_, _ = b.Buffer.Write(p)
			}
		} else {
			b.truncated = true
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func (b *gitOutputBuffer) String() string {
	return string(bytes.ToValidUTF8(b.Buffer.Bytes(), nil))
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

// isAmbiguousGitStatusOutsideRepo 处理 git status 在非仓库目录中仅返回 exit status 128 的模糊场景。
func isAmbiguousGitStatusOutsideRepo(workdir string, output string, err error) bool {
	if err == nil || strings.TrimSpace(output) != "" || !strings.Contains(strings.ToLower(err.Error()), "exit status 128") {
		return false
	}
	info, statErr := os.Stat(workdir)
	if statErr != nil || !info.IsDir() {
		return false
	}
	return !hasGitMetadataAncestor(workdir)
}

// hasGitMetadataAncestor 向上查找 .git 元数据入口，用于区分真实仓库与空目录。
func hasGitMetadataAncestor(workdir string) bool {
	current := filepath.Clean(workdir)
	for {
		gitPath := filepath.Join(current, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}

// isContextError 用于保留上下文取消与超时等主链路错误语义。
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
