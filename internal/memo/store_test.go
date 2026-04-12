package memo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentsession "neo-code/internal/session"
)

func TestNewFileStore(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")
	if store == nil {
		t.Fatal("NewFileStore returned nil")
	}
	if store.memoDir == "" {
		t.Error("memoDir is empty")
	}
	if store.topicsDir == "" {
		t.Error("topicsDir is empty")
	}
}

func TestFileStoreLoadIndexNotExist(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")

	idx, err := store.LoadIndex(context.Background())
	if err != nil {
		t.Fatalf("LoadIndex on nonexistent dir error: %v", err)
	}
	if idx == nil {
		t.Fatal("LoadIndex returned nil index")
	}
	if len(idx.Entries) != 0 {
		t.Errorf("Entries = %d, want 0", len(idx.Entries))
	}
}

func TestFileStoreSaveAndLoadIndex(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")

	original := &Index{
		Entries: []Entry{
			{
				ID:        "user_001",
				Type:      TypeUser,
				Title:     "偏好 tab 缩进",
				Content:   "详细内容",
				Keywords:  []string{"tabs"},
				Source:    SourceUserManual,
				TopicFile: "user_profile.md",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		UpdatedAt: time.Now(),
	}

	ctx := context.Background()
	if err := store.SaveIndex(ctx, original); err != nil {
		t.Fatalf("SaveIndex error: %v", err)
	}

	loaded, err := store.LoadIndex(ctx)
	if err != nil {
		t.Fatalf("LoadIndex error: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("loaded entries = %d, want 1", len(loaded.Entries))
	}
	if loaded.Entries[0].Title != "偏好 tab 缩进" {
		t.Errorf("Title = %q, want %q", loaded.Entries[0].Title, "偏好 tab 缩进")
	}
	if loaded.Entries[0].TopicFile != "user_profile.md" {
		t.Errorf("TopicFile = %q, want %q", loaded.Entries[0].TopicFile, "user_profile.md")
	}
}

func TestFileStoreSaveIndexNil(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")
	err := store.SaveIndex(context.Background(), nil)
	if err == nil {
		t.Error("SaveIndex(nil) should return error")
	}
}

func TestFileStoreSaveAndLoadTopic(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")
	ctx := context.Background()

	content := "---\ntype: user\n---\n\n这是详细内容\n"
	if err := store.SaveTopic(ctx, "user_profile.md", content); err != nil {
		t.Fatalf("SaveTopic error: %v", err)
	}

	loaded, err := store.LoadTopic(ctx, "user_profile.md")
	if err != nil {
		t.Fatalf("LoadTopic error: %v", err)
	}
	if loaded != content {
		t.Errorf("LoadTopic = %q, want %q", loaded, content)
	}
}

func TestFileStoreLoadTopicNotExist(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")
	ctx := context.Background()

	_, err := store.LoadTopic(ctx, "nonexistent.md")
	if err == nil {
		t.Error("LoadTopic on nonexistent file should return error")
	}
}

func TestFileStoreDeleteTopic(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")
	ctx := context.Background()

	if err := store.SaveTopic(ctx, "to_delete.md", "content"); err != nil {
		t.Fatalf("SaveTopic error: %v", err)
	}
	if err := store.DeleteTopic(ctx, "to_delete.md"); err != nil {
		t.Fatalf("DeleteTopic error: %v", err)
	}
	if _, err := store.LoadTopic(ctx, "to_delete.md"); err == nil {
		t.Error("LoadTopic after delete should return error")
	}
}

func TestFileStoreDeleteTopicNotExist(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")
	ctx := context.Background()

	err := store.DeleteTopic(ctx, "nonexistent.md")
	if err != nil {
		t.Errorf("DeleteTopic on nonexistent file should not error: %v", err)
	}
}

func TestFileStoreListTopics(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")
	ctx := context.Background()

	// 空目录应返回空列表
	topics, err := store.ListTopics(ctx)
	if err != nil {
		t.Fatalf("ListTopics on empty dir error: %v", err)
	}
	if len(topics) != 0 {
		t.Errorf("ListTopics empty = %d, want 0", len(topics))
	}

	// 写入几个 topic
	for _, name := range []string{"a.md", "b.md", "c.txt"} {
		if strings.HasSuffix(name, ".md") {
			_ = store.SaveTopic(ctx, name, "content")
		}
	}

	topics, err = store.ListTopics(ctx)
	if err != nil {
		t.Fatalf("ListTopics error: %v", err)
	}
	if len(topics) != 2 {
		t.Errorf("ListTopics = %d, want 2 (only .md files)", len(topics))
	}
}

func TestFileStoreCancelContext(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.LoadIndex(ctx); err == nil {
		t.Error("LoadIndex with cancelled context should return error")
	}
	if err := store.SaveIndex(ctx, &Index{}); err == nil {
		t.Error("SaveIndex with cancelled context should return error")
	}
	if _, err := store.LoadTopic(ctx, "f.md"); err == nil {
		t.Error("LoadTopic with cancelled context should return error")
	}
	if err := store.SaveTopic(ctx, "f.md", "c"); err == nil {
		t.Error("SaveTopic with cancelled context should return error")
	}
	if err := store.DeleteTopic(ctx, "f.md"); err == nil {
		t.Error("DeleteTopic with cancelled context should return error")
	}
	if _, err := store.ListTopics(ctx); err == nil {
		t.Error("ListTopics with cancelled context should return error")
	}
}

func TestFileStoreWorkspaceIsolation(t *testing.T) {
	tmp := t.TempDir()
	store1 := NewFileStore(tmp, "/workspace/a")
	store2 := NewFileStore(tmp, "/workspace/b")
	ctx := context.Background()

	idx1 := &Index{Entries: []Entry{{Type: TypeUser, Title: "Project A"}}}
	if err := store1.SaveIndex(ctx, idx1); err != nil {
		t.Fatalf("SaveIndex store1 error: %v", err)
	}

	idx2, err := store2.LoadIndex(ctx)
	if err != nil {
		t.Fatalf("LoadIndex store2 error: %v", err)
	}
	if len(idx2.Entries) != 0 {
		t.Errorf("store2 should have no entries (workspace isolation), got %d", len(idx2.Entries))
	}
}

func TestFileStoreAtomicWrite(t *testing.T) {
	tmp := t.TempDir()
	store := NewFileStore(tmp, "/workspace/project")
	ctx := context.Background()

	// 写入索引后不应存在临时文件
	_ = store.SaveIndex(ctx, &Index{Entries: []Entry{{Type: TypeUser, Title: "test"}}})

	memoDir := store.memoDir
	entries, _ := os.ReadDir(memoDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file should not exist after atomic write: %s", e.Name())
		}
	}
}

func TestMemoDirectory(t *testing.T) {
	dir := memoDirectory("/base", "/workspace")
	expected := filepath.Join("/base", "projects", agentsession.HashWorkspaceRoot("/workspace"), "memo")
	if dir != expected {
		t.Errorf("memoDirectory = %q, want %q", dir, expected)
	}
}

func TestHashWorkspaceRootStable(t *testing.T) {
	h1 := agentsession.HashWorkspaceRoot("/workspace/project")
	h2 := agentsession.HashWorkspaceRoot("/workspace/project")
	if h1 != h2 {
		t.Errorf("hash not stable: %q != %q", h1, h2)
	}
}

func TestHashWorkspaceRootDifferent(t *testing.T) {
	h1 := agentsession.HashWorkspaceRoot("/workspace/a")
	h2 := agentsession.HashWorkspaceRoot("/workspace/b")
	if h1 == h2 {
		t.Errorf("different paths should produce different hashes")
	}
}

func TestHashWorkspaceRootEmpty(t *testing.T) {
	h := agentsession.HashWorkspaceRoot("")
	// 空路径回退到 "unknown" 的哈希，应产生稳定的非空结果
	if h == "" {
		t.Error("hash of empty workspace root should not be empty")
	}
	if len(h) != 16 {
		t.Errorf("hash length = %d, want 16 (8 bytes hex)", len(h))
	}
}
