package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
)

type blockingAssetReader struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	read    bool
}

func newBlockingAssetReader() *blockingAssetReader {
	return &blockingAssetReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (r *blockingAssetReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	return copy(p, []byte("slow-image")), nil
}

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
	// text/html 仍不属于会话文本 asset 白名单（避免任意 HTML/可执行脚本被内联为模型上下文）。
	if _, err := store.SaveAsset(ctx, "session_assets_invalid", strings.NewReader("x"), "text/html"); err == nil {
		t.Fatalf("expected unsupported mime type error")
	}
	if _, err := store.SaveAsset(ctx, "missing", strings.NewReader("x"), "image/png"); err == nil {
		t.Fatalf("expected missing session error")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
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

func TestSQLiteStoreSaveAssetDoesNotHoldWriteLockDuringFileCopy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_slow_copy", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	reader := newBlockingAssetReader()
	saveDone := make(chan error, 1)
	go func() {
		_, err := store.SaveAsset(ctx, session.ID, reader, "image/png")
		saveDone <- err
	}()

	select {
	case <-reader.started:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for SaveAsset copy to start")
	}

	appendDone := make(chan error, 1)
	go func() {
		appendDone <- store.AppendMessages(ctx, AppendMessagesInput{
			SessionID: session.ID,
			Messages: []providertypes.Message{{
				Role:  providertypes.RoleUser,
				Parts: []providertypes.ContentPart{providertypes.NewTextPart("message while asset copy is blocked")},
			}},
		})
	}()

	select {
	case err := <-appendDone:
		if err != nil {
			close(reader.release)
			t.Fatalf("AppendMessages() while SaveAsset copying error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		close(reader.release)
		t.Fatalf("AppendMessages() blocked while SaveAsset was copying")
	}

	close(reader.release)
	if err := <-saveDone; err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}
}

func TestSQLiteStoreDeleteAssetRemovesMetadataAndFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_delete", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	meta, err := store.SaveAsset(ctx, session.ID, strings.NewReader("image"), "image/png")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}
	target := filepath.Join(store.assetsDir, session.ID, meta.ID+".bin")

	if err := store.DeleteAsset(ctx, session.ID, meta.ID); err != nil {
		t.Fatalf("DeleteAsset() error = %v", err)
	}
	if _, err := store.Stat(ctx, session.ID, meta.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat(deleted asset) error = %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("asset file stat error = %v, want os.ErrNotExist", err)
	}
	if err := store.DeleteAsset(ctx, session.ID, meta.ID); err != nil {
		t.Fatalf("DeleteAsset() should be idempotent, got %v", err)
	}
}

func TestSQLiteStoreDeleteSessionRemovesAssetDirectory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_delete_assets", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := store.SaveAsset(ctx, session.ID, strings.NewReader("image"), "image/png"); err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}
	assetDir := filepath.Join(store.assetsDir, session.ID)

	if err := store.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := store.LoadSession(ctx, session.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadSession(deleted) error = %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(assetDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("asset dir stat error = %v, want os.ErrNotExist", err)
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
	store := NewSQLiteStore(baseDir, workspaceRoot)
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
	if err := store.DeleteAsset(ctx, "session_assets_ctx", "asset_x"); err == nil {
		t.Fatalf("expected canceled DeleteAsset error")
	}
	if err := store.DeleteSession(ctx, "session_assets_ctx"); err == nil {
		t.Fatalf("expected canceled DeleteSession error")
	}
	if _, err := store.CleanupExpiredSessions(ctx, time.Hour); err == nil {
		t.Fatalf("expected canceled CleanupExpiredSessions error")
	}
}

func TestSQLiteStoreSaveAssetRespectsConfiguredAssetPolicy(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	store.SetAssetPolicy(AssetPolicy{
		MaxSessionAssetBytes: 1,
	})
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_limit", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := store.SaveAsset(ctx, session.ID, strings.NewReader("xx"), "image/png"); err == nil ||
		!strings.Contains(err.Error(), "asset size exceeds 1 bytes") {
		t.Fatalf("expected configured asset size limit error, got %v", err)
	}
}

func MaxSessionAssetBytesForTest() int64 {
	return MaxSessionAssetBytes
}

func TestSQLiteStoreSaveAssetAcceptsWhitelistedTextMime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_text_ok", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	cases := []struct {
		mime     string
		payload  string
		wantMime string
	}{
		{mime: "text/plain", payload: "hello", wantMime: "text/plain"},
		{mime: "text/markdown", payload: "# title", wantMime: "text/markdown"},
		{mime: "application/json", payload: "{\"k\":1}", wantMime: "application/json"},
		{mime: "text/yaml", payload: "k: v", wantMime: "text/yaml"},
		{mime: "application/x-yaml", payload: "k: v", wantMime: "application/x-yaml"},
		{mime: "text/csv", payload: "a,b\n1,2", wantMime: "text/csv"},
	}
	for _, tc := range cases {
		meta, err := store.SaveAsset(ctx, session.ID, strings.NewReader(tc.payload), tc.mime)
		if err != nil {
			t.Fatalf("SaveAsset(mime=%q) error = %v", tc.mime, err)
		}
		if meta.MimeType != tc.wantMime {
			t.Errorf("SaveAsset(mime=%q) returned mime=%q", tc.mime, meta.MimeType)
		}
		if meta.Size != int64(len(tc.payload)) {
			t.Errorf("SaveAsset(mime=%q) size=%d, want %d", tc.mime, meta.Size, len(tc.payload))
		}
		// Open 必须能取回相同字节流。
		rc, openMeta, err := store.Open(ctx, session.ID, meta.ID)
		if err != nil {
			t.Fatalf("Open(asset=%s) error = %v", meta.ID, err)
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			t.Fatalf("ReadAll(asset=%s) error = %v", meta.ID, readErr)
		}
		if string(data) != tc.payload {
			t.Errorf("Open(asset=%s) payload mismatch", meta.ID)
		}
		if openMeta.MimeType != tc.wantMime {
			t.Errorf("Open(asset=%s) mime=%q, want %q", meta.ID, openMeta.MimeType, tc.wantMime)
		}
	}
}

func TestSQLiteStoreSaveAssetRejectsNonWhitelistedTextMime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_text_reject", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	cases := []string{
		"text/html",
		"application/javascript",
		"application/octet-stream",
		"application/xml",
		"text/css",
		"application/pdf",
	}
	for _, mime := range cases {
		if _, err := store.SaveAsset(ctx, session.ID, strings.NewReader("x"), mime); err == nil {
			t.Errorf("SaveAsset(mime=%q) should be rejected", mime)
		} else if !strings.Contains(err.Error(), "unsupported asset mime type") {
			t.Errorf("SaveAsset(mime=%q) unexpected error = %v", mime, err)
		}
	}
}

func TestSQLiteStoreSaveAssetAppliesTextPolicySizeLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	store.SetTextAssetPolicy(TextAssetPolicy{
		Whitelist:         DefaultTextAssetWhitelist(),
		MaxTextAssetBytes: 4,
		MaxTextAssetChars: 16,
	})
	session, err := store.CreateSession(ctx, CreateSessionInput{ID: "session_assets_text_limit", Title: "assets"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// 1 字节 OK。
	if _, err := store.SaveAsset(ctx, session.ID, strings.NewReader("abcd"), "text/plain"); err != nil {
		t.Fatalf("SaveAsset(within text limit) error = %v", err)
	}
	// 超 4 字节应被拒（错误信息中应出现 4 bytes 截断值）。
	oversized := bytes.NewReader([]byte("abcde"))
	if _, err := store.SaveAsset(ctx, session.ID, oversized, "text/markdown"); err == nil ||
		!strings.Contains(err.Error(), "asset size exceeds 4 bytes") {
		t.Fatalf("SaveAsset(exceed text limit) error = %v, want size limit error", err)
	}
	// 图片路径仍走 image 上限（默认 20 MiB），4 字节图片 OK。
	if _, err := store.SaveAsset(ctx, session.ID, strings.NewReader("abcd"), "image/png"); err != nil {
		t.Fatalf("SaveAsset(image within image limit) error = %v", err)
	}
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
