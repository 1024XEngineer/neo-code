package memo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	agentsession "neo-code/internal/session"
)

const (
	memoDirName   = "memo"
	topicsDirName = "topics"
	memoFileName  = "MEMO.md"
)

// FileStore 基于文件系统实现 Store 接口，采用工作区隔离的目录布局。
type FileStore struct {
	mu        sync.RWMutex
	memoDir   string
	topicsDir string
}

// NewFileStore 创建 FileStore 实例，目录基于 baseDir 和 workspaceRoot 计算工作区隔离路径。
func NewFileStore(baseDir string, workspaceRoot string) *FileStore {
	dir := memoDirectory(baseDir, workspaceRoot)
	return &FileStore{
		memoDir:   dir,
		topicsDir: filepath.Join(dir, topicsDirName),
	}
}

// LoadIndex 加载 MEMO.md 索引文件并解析为 Index 结构。
func (s *FileStore) LoadIndex(ctx context.Context) (*Index, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.loadIndexUnlocked()
}

// SaveIndex 将索引写入 MEMO.md 文件，采用临时文件 + 原子替换策略。
func (s *FileStore) SaveIndex(ctx context.Context, index *Index) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if index == nil {
		return errors.New("memo: index is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.memoDir, 0o755); err != nil {
		return fmt.Errorf("memo: create memo dir: %w", err)
	}

	content := RenderIndex(index)
	target := filepath.Join(s.memoDir, memoFileName)
	temp := target + ".tmp"

	if err := os.WriteFile(temp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("memo: write temp index: %w", err)
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("memo: remove old index: %w", err)
	}
	if err := os.Rename(temp, target); err != nil {
		return fmt.Errorf("memo: commit index: %w", err)
	}

	return nil
}

// LoadTopic 读取指定 topic 文件的完整内容。
func (s *FileStore) LoadTopic(ctx context.Context, filename string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.topicPath(filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("memo: read topic %s: %w", filename, err)
	}
	return string(data), nil
}

// SaveTopic 将内容写入指定 topic 文件，采用临时文件 + 原子替换策略。
func (s *FileStore) SaveTopic(ctx context.Context, filename string, content string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.topicsDir, 0o755); err != nil {
		return fmt.Errorf("memo: create topics dir: %w", err)
	}

	path := s.topicPath(filename)
	temp := path + ".tmp"

	if err := os.WriteFile(temp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("memo: write temp topic: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("memo: remove old topic: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("memo: commit topic: %w", err)
	}

	return nil
}

// DeleteTopic 删除指定 topic 文件。
func (s *FileStore) DeleteTopic(ctx context.Context, filename string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.topicPath(filename)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("memo: delete topic %s: %w", filename, err)
	}
	return nil
}

// ListTopics 列出 topics 目录下所有 .md 文件名。
func (s *FileStore) ListTopics(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.topicsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("memo: list topics: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// loadIndexUnlocked 在无锁状态下读取并解析 MEMO.md。
func (s *FileStore) loadIndexUnlocked() (*Index, error) {
	path := filepath.Join(s.memoDir, memoFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Index{}, nil
		}
		return nil, fmt.Errorf("memo: read index: %w", err)
	}
	return ParseIndex(string(data))
}

// topicPath 生成 topic 文件的安全路径，防止目录穿越。
func (s *FileStore) topicPath(filename string) string {
	safe := filepath.Base(filename)
	return filepath.Join(s.topicsDir, safe)
}

// memoDirectory 根据工作区根目录计算记忆分桶目录，复用 session 包的工作区哈希。
func memoDirectory(baseDir string, workspaceRoot string) string {
	return filepath.Join(baseDir, "projects", agentsession.HashWorkspaceRoot(workspaceRoot), memoDirName)
}
