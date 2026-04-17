package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	agentsession "neo-code/internal/session"
)

type stubAssetStore struct {
	openFunc func(ctx context.Context, sessionID string, assetID string) (io.ReadCloser, agentsession.AssetMeta, error)
}

func (s stubAssetStore) SaveAsset(ctx context.Context, sessionID string, r io.Reader, mimeType string) (agentsession.AssetMeta, error) {
	_ = ctx
	_ = sessionID
	_ = r
	_ = mimeType
	return agentsession.AssetMeta{}, nil
}

func (s stubAssetStore) Open(ctx context.Context, sessionID string, assetID string) (io.ReadCloser, agentsession.AssetMeta, error) {
	return s.openFunc(ctx, sessionID, assetID)
}

func (s stubAssetStore) Stat(ctx context.Context, sessionID string, assetID string) (agentsession.AssetMeta, error) {
	_ = ctx
	_ = sessionID
	_ = assetID
	return agentsession.AssetMeta{}, nil
}

func TestBuildSessionAssetReaderReturnsNilWhenUnavailable(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	if reader := svc.buildSessionAssetReader(context.Background(), "session-1"); reader != nil {
		t.Fatalf("expected nil reader without asset store")
	}
	svc.SetSessionAssetStore(stubAssetStore{})
	if reader := svc.buildSessionAssetReader(context.Background(), "   "); reader != nil {
		t.Fatalf("expected nil reader for empty session id")
	}
}

func TestBuildSessionAssetReaderOpen(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	svc.SetSessionAssetStore(stubAssetStore{
		openFunc: func(ctx context.Context, sessionID string, assetID string) (io.ReadCloser, agentsession.AssetMeta, error) {
			if sessionID != "session-asset" || assetID != "asset-1" {
				t.Fatalf("unexpected open args session=%q asset=%q", sessionID, assetID)
			}
			return io.NopCloser(bytes.NewReader([]byte("img"))), agentsession.AssetMeta{
				ID:       "asset-1",
				MimeType: "image/png",
				Size:     3,
			}, nil
		},
	})

	reader := svc.buildSessionAssetReader(context.Background(), "session-asset")
	if reader == nil {
		t.Fatalf("expected non-nil reader")
	}

	rc, mime, err := reader.Open(context.Background(), "asset-1")
	if err != nil {
		t.Fatalf("reader.Open() error = %v", err)
	}
	defer func() { _ = rc.Close() }()
	content, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if mime != "image/png" || string(content) != "img" {
		t.Fatalf("unexpected open result mime=%q content=%q", mime, string(content))
	}
}

func TestBuildSessionAssetReaderOpenErrorWrapped(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	svc.SetSessionAssetStore(stubAssetStore{
		openFunc: func(ctx context.Context, sessionID string, assetID string) (io.ReadCloser, agentsession.AssetMeta, error) {
			_ = ctx
			_ = sessionID
			_ = assetID
			return nil, agentsession.AssetMeta{}, io.EOF
		},
	})
	reader := svc.buildSessionAssetReader(context.Background(), "session-asset")
	if reader == nil {
		t.Fatalf("expected non-nil reader")
	}
	if _, _, err := reader.Open(context.Background(), "asset-1"); err == nil || !strings.Contains(err.Error(), "runtime: open session asset") {
		t.Fatalf("expected wrapped open error, got %v", err)
	}
}

func TestBuildSessionAssetReaderOpenForwardsContext(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	svc.SetSessionAssetStore(stubAssetStore{
		openFunc: func(ctx context.Context, sessionID string, assetID string) (io.ReadCloser, agentsession.AssetMeta, error) {
			_ = sessionID
			_ = assetID
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatalf("expected canceled context, got %v", ctx.Err())
			}
			return nil, agentsession.AssetMeta{}, ctx.Err()
		},
	})

	runCtx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := svc.buildSessionAssetReader(runCtx, "session-asset")
	if reader == nil {
		t.Fatalf("expected non-nil reader")
	}
	if _, _, err := reader.Open(nil, "asset-ctx"); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestBuildSessionAssetReaderOpenFallsBackToBackgroundWhenNoContext(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	svc.SetSessionAssetStore(stubAssetStore{
		openFunc: func(ctx context.Context, sessionID string, assetID string) (io.ReadCloser, agentsession.AssetMeta, error) {
			if ctx == nil {
				t.Fatalf("expected non-nil context")
			}
			if err := ctx.Err(); err != nil {
				t.Fatalf("expected active context, got err=%v", err)
			}
			if sessionID != "session-bg" || assetID != "asset-bg" {
				t.Fatalf("unexpected open args session=%q asset=%q", sessionID, assetID)
			}
			return io.NopCloser(bytes.NewReader([]byte("ok"))), agentsession.AssetMeta{
				ID:       "asset-bg",
				MimeType: "image/png",
				Size:     2,
			}, nil
		},
	})

	reader := svc.buildSessionAssetReader(nil, "session-bg")
	if reader == nil {
		t.Fatalf("expected non-nil reader")
	}
	rc, mime, err := reader.Open(nil, "asset-bg")
	if err != nil {
		t.Fatalf("reader.Open() error = %v", err)
	}
	defer func() { _ = rc.Close() }()
	if mime != "image/png" {
		t.Fatalf("expected mime image/png, got %q", mime)
	}
}
