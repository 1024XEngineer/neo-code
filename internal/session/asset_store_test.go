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

	providertypes "neo-code/internal/provider/types"
)

func TestJSONStoreSaveAssetOpenAndStat(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir(), t.TempDir())
	sessionID := "session_asset_round_trip"
	payload := []byte("fake-image-bytes")

	meta, err := store.SaveAsset(context.Background(), sessionID, bytes.NewReader(payload), "image/png")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}
	if meta.ID == "" || meta.MimeType != "image/png" || meta.Size != int64(len(payload)) {
		t.Fatalf("unexpected saved meta: %+v", meta)
	}

	statMeta, err := store.Stat(context.Background(), sessionID, meta.ID)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if statMeta != meta {
		t.Fatalf("expected stat meta %+v, got %+v", meta, statMeta)
	}

	rc, openMeta, err := store.Open(context.Background(), sessionID, meta.ID)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll(open) error = %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("unexpected open payload: got %q want %q", string(data), string(payload))
	}
	if openMeta != meta {
		t.Fatalf("expected open meta %+v, got %+v", meta, openMeta)
	}

	assetPath := store.assetPath(sessionID, meta.ID)
	if _, err := os.Stat(assetPath); err != nil {
		t.Fatalf("expected asset file %q exists, err=%v", assetPath, err)
	}
	if _, err := os.Stat(store.assetMetaPath(sessionID, meta.ID)); err != nil {
		t.Fatalf("expected asset meta file exists, err=%v", err)
	}
}

func TestJSONStoreSaveAssetRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir(), t.TempDir())
	sessionID := "session_asset_invalid"

	if _, err := store.SaveAsset(context.Background(), sessionID, nil, "image/png"); err == nil {
		t.Fatalf("expected nil reader error")
	}
	if _, err := store.SaveAsset(context.Background(), sessionID, strings.NewReader("x"), ""); err == nil {
		t.Fatalf("expected empty mime error")
	}
	if _, err := store.SaveAsset(context.Background(), sessionID, strings.NewReader("x"), "text/plain"); err == nil {
		t.Fatalf("expected unsupported mime error")
	}
	if _, err := store.SaveAsset(context.Background(), "../bad", strings.NewReader("x"), "image/png"); err == nil {
		t.Fatalf("expected invalid session id error")
	}
}

func TestJSONStoreAssetOpenAndStatRejectInvalidID(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir(), t.TempDir())

	if _, _, err := store.Open(context.Background(), "bad/session", "asset-ok"); err == nil {
		t.Fatalf("expected invalid session id on open")
	}
	if _, _, err := store.Open(context.Background(), "session_ok", "../bad"); err == nil {
		t.Fatalf("expected invalid asset id on open")
	}
	if _, err := store.Stat(context.Background(), "bad/session", "asset-ok"); err == nil {
		t.Fatalf("expected invalid session id on stat")
	}
	if _, err := store.Stat(context.Background(), "session_ok", "../bad"); err == nil {
		t.Fatalf("expected invalid asset id on stat")
	}
}

func TestJSONStoreAssetStoreRespectsCanceledContext(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir(), t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := store.SaveAsset(ctx, "session_ctx_cancel", strings.NewReader("x"), "image/png"); err == nil {
		t.Fatalf("expected canceled SaveAsset error")
	}
	if _, _, err := store.Open(ctx, "session_ctx_cancel", "asset_x"); err == nil {
		t.Fatalf("expected canceled Open error")
	}
	if _, err := store.Stat(ctx, "session_ctx_cancel", "asset_x"); err == nil {
		t.Fatalf("expected canceled Stat error")
	}
}

func TestJSONStoreSaveAssetRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir(), t.TempDir())
	oversized := bytes.NewReader(make([]byte, providertypes.MaxSessionAssetBytes+1))

	if _, err := store.SaveAsset(context.Background(), "session_oversize", oversized, "image/png"); err == nil ||
		!strings.Contains(err.Error(), "asset size exceeds") {
		t.Fatalf("expected oversized payload rejection, got %v", err)
	}
}

func TestDecodeStoredAssetMetaRejectsNonImageMIME(t *testing.T) {
	t.Parallel()

	_, err := decodeStoredAssetMeta([]byte(`{"id":"asset_ok","mime_type":"text/plain","size":1}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported asset mime type") {
		t.Fatalf("expected non-image mime rejection, got %v", err)
	}
}

func TestDecodeAndEncodeStoredAssetMetaValidation(t *testing.T) {
	t.Parallel()

	if _, err := decodeStoredAssetMeta([]byte(`{`)); err == nil || !strings.Contains(err.Error(), "decode asset meta") {
		t.Fatalf("expected decode error, got %v", err)
	}
	if _, err := decodeStoredAssetMeta([]byte(`{"id":"bad/asset","mime_type":"image/png","size":1}`)); err == nil ||
		!strings.Contains(err.Error(), "unsupported characters") {
		t.Fatalf("expected invalid asset id error, got %v", err)
	}
	if _, err := decodeStoredAssetMeta([]byte(`{"id":"asset_ok","mime_type":"   ","size":1}`)); err == nil ||
		!strings.Contains(err.Error(), "mime_type is empty") {
		t.Fatalf("expected empty mime_type error, got %v", err)
	}

	if _, err := encodeStoredAssetMeta(AssetMeta{ID: "bad/asset", MimeType: "image/png", Size: 1}); err == nil ||
		!strings.Contains(err.Error(), "unsupported characters") {
		t.Fatalf("expected invalid asset id encode error, got %v", err)
	}
	if _, err := encodeStoredAssetMeta(AssetMeta{ID: "asset_ok", MimeType: "   ", Size: 1}); err == nil ||
		!strings.Contains(err.Error(), "asset mime type is empty") {
		t.Fatalf("expected empty mime encode error, got %v", err)
	}
}

func TestJSONStoreSaveAssetFailurePaths(t *testing.T) {
	t.Parallel()

	t.Run("create assets dir failed", func(t *testing.T) {
		t.Parallel()

		baseDir := t.TempDir()
		workspaceRoot := t.TempDir()
		store := NewJSONStore(baseDir, workspaceRoot)

		assetsDir := store.assetsDir("session_assets_dir_fail")
		if err := os.MkdirAll(filepath.Dir(assetsDir), 0o755); err != nil {
			t.Fatalf("mkdir parent: %v", err)
		}
		if err := os.WriteFile(assetsDir, []byte("blocked"), 0o644); err != nil {
			t.Fatalf("write blocker file: %v", err)
		}

		if _, err := store.SaveAsset(context.Background(), "session_assets_dir_fail", strings.NewReader("x"), "image/png"); err == nil ||
			!strings.Contains(err.Error(), "create assets dir") {
			t.Fatalf("expected create assets dir error, got %v", err)
		}
	})

	t.Run("copy temp asset failed", func(t *testing.T) {
		t.Parallel()

		store := NewJSONStore(t.TempDir(), t.TempDir())
		if _, err := store.SaveAsset(context.Background(), "session_copy_fail", failingReader{}, "image/png"); err == nil ||
			!strings.Contains(err.Error(), "write temp asset") {
			t.Fatalf("expected write temp asset error, got %v", err)
		}
	})
}

func TestJSONStoreOpenAndStatMissingStoredFiles(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(t.TempDir(), t.TempDir())
	sessionID := "session_missing_files"
	meta, err := store.SaveAsset(context.Background(), sessionID, strings.NewReader("img"), "image/png")
	if err != nil {
		t.Fatalf("save seed asset: %v", err)
	}

	if err := os.Remove(store.assetPath(sessionID, meta.ID)); err != nil {
		t.Fatalf("remove asset file: %v", err)
	}
	if _, _, err := store.Open(context.Background(), sessionID, meta.ID); err == nil {
		t.Fatalf("expected open failure when asset binary is missing")
	}

	if err := os.Remove(store.assetMetaPath(sessionID, meta.ID)); err != nil {
		t.Fatalf("remove asset meta file: %v", err)
	}
	if _, err := store.Stat(context.Background(), sessionID, meta.ID); err == nil {
		t.Fatalf("expected stat failure when asset meta is missing")
	}
}

type failingReader struct{}

func (failingReader) Read(_ []byte) (int, error) {
	return 0, errors.New("read failure")
}
