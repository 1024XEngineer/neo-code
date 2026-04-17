package session

import (
	"fmt"
	"os"
	"testing"
)

func buildIndexedSuffix(index int) string {
	return fmt.Sprintf("-%02d", index)
}

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	baseDir, err := os.MkdirTemp("", "session-base-")
	if err != nil {
		t.Fatalf("MkdirTemp() baseDir error = %v", err)
	}
	workspaceRoot, err := os.MkdirTemp("", "session-workspace-")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	store := NewStore(baseDir, workspaceRoot)
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(baseDir)
		_ = os.RemoveAll(workspaceRoot)
	})
	return store
}
