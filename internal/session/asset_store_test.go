package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteStoreSaveAssetOpenAndStat(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	payload := []byte("image-bytes")
	meta, err := store.SaveAsset(ctx, session.ID, bytes.NewReader(payload), "image/png")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}
	if meta.ID == "" || meta.Size != int64(len(payload)) {
		t.Fatalf("unexpected asset meta: %+v", meta)
	}

	statMeta, err := store.Stat(ctx, session.ID, meta.ID)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if statMeta != meta {
		t.Fatalf("Stat() = %+v, want %+v", statMeta, meta)
	}

	rc, openMeta, err := store.Open(ctx, session.ID, meta.ID)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("unexpected open payload %q, want %q", string(data), string(payload))
	}
	if openMeta != meta {
		t.Fatalf("Open() meta = %+v, want %+v", openMeta, meta)
	}
}

func TestSQLiteStoreSaveAssetRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if _, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_invalid", Title: "assets"}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := store.SaveAsset(ctx, "session_assets_invalid", nil, "image/png"); err == nil {
		t.Fatalf("expected nil reader error")
	}
	if _, err := store.SaveAsset(ctx, "session_assets_invalid", strings.NewReader("x"), ""); err == nil {
		t.Fatalf("expected empty mime type error")
	}
	if _, err := store.SaveAsset(ctx, "session_assets_invalid", strings.NewReader("x"), "text/plain"); err == nil {
		t.Fatalf("expected unsupported mime type error")
	}
	if _, err := store.SaveAsset(ctx, "missing", strings.NewReader("x"), "image/png"); err == nil {
		t.Fatalf("expected missing session error")
	}
	if _, _, err := store.Open(ctx, "bad/session", "asset_ok"); err == nil {
		t.Fatalf("expected invalid session id error")
	}
	if _, err := store.Stat(ctx, "session_assets_invalid", "../bad"); err == nil {
		t.Fatalf("expected invalid asset id error")
	}
}

func TestSQLiteStoreSaveAssetRejectsOversizedPayload(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_big", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	oversized := bytes.NewReader(bytes.Repeat([]byte("x"), int(1+MaxSessionAssetBytesForTest())))
	if _, err := store.SaveAsset(ctx, session.ID, oversized, "image/png"); err == nil {
		t.Fatalf("expected oversize error")
	}
}

func TestSQLiteStoreOpenReturnsFileErrorWhenPayloadMissing(t *testing.T) {
	ctx := context.Background()
	baseDir, err := os.MkdirTemp("", "session-base-")
	if err != nil {
		t.Fatalf("MkdirTemp() baseDir error = %v", err)
	}
	workspaceRoot, err := os.MkdirTemp("", "session-workspace-")
	if err != nil {
		t.Fatalf("MkdirTemp() workspaceRoot error = %v", err)
	}
	store := NewStore(baseDir, workspaceRoot)
	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(baseDir)
		_ = os.RemoveAll(workspaceRoot)
	})
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_missing_file", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	meta, err := store.SaveAsset(ctx, session.ID, strings.NewReader("img"), "image/png")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}
	target := filepath.Join(assetsDirectory(baseDir, workspaceRoot), session.ID, meta.ID+".bin")
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove target asset: %v", err)
	}

	if _, _, err := store.Open(ctx, session.ID, meta.ID); err == nil {
		t.Fatalf("expected missing payload file error")
	}
}

func TestSQLiteStoreAssetMethodsRespectContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	store := newTestStore(t)
	if _, err := store.SaveAsset(ctx, "session_assets_ctx", strings.NewReader("x"), "image/png"); err == nil {
		t.Fatalf("expected canceled SaveAsset error")
	}
	if _, _, err := store.Open(ctx, "session_assets_ctx", "asset_x"); err == nil {
		t.Fatalf("expected canceled Open error")
	}
	if _, err := store.Stat(ctx, "session_assets_ctx", "asset_x"); err == nil {
		t.Fatalf("expected canceled Stat error")
	}
}

func MaxSessionAssetBytesForTest() int64 {
	return 20 * 1024 * 1024
}

func TestSQLiteStoreOpenMissingAssetReturnsNotExist(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if _, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_missing_meta", Title: "assets"}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	_, _, err := store.Open(ctx, "session_assets_missing_meta", "asset_missing")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestSQLiteStoreAssetMetaRejectsEscapedRelativePath(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_escape", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	db, err := store.ensureDB(ctx)
	if err != nil {
		t.Fatalf("ensureDB() error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO session_assets (id, session_id, mime_type, size_bytes, relative_path, created_at_ms)
VALUES ('asset_escape', ?, 'image/png', 4, '../escape.bin', 0)
`, session.ID); err != nil {
		t.Fatalf("insert escaped asset meta: %v", err)
	}

	if _, err := store.Stat(ctx, session.ID, "asset_escape"); err == nil || !strings.Contains(err.Error(), "escapes base dir") {
		t.Fatalf("expected escaped relative path error, got %v", err)
	}
}
