package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const workspaceIndexFileName = "workspaces.json"

// WorkspaceRecord 描述一个工作区在索引中的登记信息。
type WorkspaceRecord struct {
	Hash      string    `json:"hash"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WorkspaceIndex 管理工作区索引的读写与生命周期。
type WorkspaceIndex struct {
	baseDir string
	mu      sync.RWMutex
	records []WorkspaceRecord
}

// NewWorkspaceIndex 创建一个新的工作区索引管理器。
func NewWorkspaceIndex(baseDir string) *WorkspaceIndex {
	return &WorkspaceIndex{
		baseDir: baseDir,
		records: make([]WorkspaceRecord, 0),
	}
}

// Load 从磁盘加载工作区索引。若文件不存在则返回空索引（无错误）。
func (idx *WorkspaceIndex) Load() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	path := idx.filePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			idx.records = make([]WorkspaceRecord, 0)
			return nil
		}
		return fmt.Errorf("workspace index load: %w", err)
	}

	var loaded []WorkspaceRecord
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("workspace index parse: %w", err)
	}
	idx.records = loaded
	return nil
}

// Save 将当前索引持久化到磁盘。
func (idx *WorkspaceIndex) Save() error {
	idx.mu.RLock()
	data, err := json.MarshalIndent(idx.records, "", "  ")
	idx.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("workspace index marshal: %w", err)
	}

	path := idx.filePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("workspace index mkdir: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("workspace index write: %w", err)
	}
	return nil
}

// Register 将一个工作区注册到索引。若已存在则更新 name 和 updated_at。
func (idx *WorkspaceIndex) Register(path string, name string) (WorkspaceRecord, error) {
	trimmedPath := NormalizeWorkspaceRoot(path)
	if trimmedPath == "" {
		return WorkspaceRecord{}, fmt.Errorf("workspace path is empty")
	}

	hash := HashWorkspaceRoot(trimmedPath)
	if name == "" {
		name = filepath.Base(trimmedPath)
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	now := time.Now()
	for i, r := range idx.records {
		if r.Hash == hash {
			idx.records[i].Name = name
			idx.records[i].UpdatedAt = now
			return idx.records[i], nil
		}
	}

	record := WorkspaceRecord{
		Hash:      hash,
		Path:      trimmedPath,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	idx.records = append(idx.records, record)
	return record, nil
}

// List 返回所有工作区记录，按 UpdatedAt 降序排列（最近活跃在前）。
func (idx *WorkspaceIndex) List() []WorkspaceRecord {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]WorkspaceRecord, len(idx.records))
	copy(result, idx.records)
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result
}

// Get 根据哈希查找工作区记录。
func (idx *WorkspaceIndex) Get(hash string) (WorkspaceRecord, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	for _, r := range idx.records {
		if r.Hash == hash {
			return r, true
		}
	}
	return WorkspaceRecord{}, false
}

// Rename 修改指定工作区的显示名称。
func (idx *WorkspaceIndex) Rename(hash string, name string) (WorkspaceRecord, error) {
	if name == "" {
		return WorkspaceRecord{}, fmt.Errorf("workspace name is empty")
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	for i, r := range idx.records {
		if r.Hash == hash {
			idx.records[i].Name = name
			idx.records[i].UpdatedAt = time.Now()
			return idx.records[i], nil
		}
	}
	return WorkspaceRecord{}, fmt.Errorf("workspace %s not found", hash)
}

// Delete 从索引中移除指定工作区，并可选删除其数据目录。
func (idx *WorkspaceIndex) Delete(hash string, removeData bool) (WorkspaceRecord, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	var target WorkspaceRecord
	var found bool
	for i, r := range idx.records {
		if r.Hash == hash {
			target = r
			found = true
			idx.records = append(idx.records[:i], idx.records[i+1:]...)
			break
		}
	}
	if !found {
		return WorkspaceRecord{}, fmt.Errorf("workspace %s not found", hash)
	}

	if removeData {
		_ = os.RemoveAll(filepath.Join(idx.baseDir, projectsDirName, hash))
	}
	return target, nil
}

// filePath 返回索引文件的绝对路径。
func (idx *WorkspaceIndex) filePath() string {
	return filepath.Join(idx.baseDir, workspaceIndexFileName)
}
