package provider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

type stubSessionAssetReader struct {
	open func(ctx context.Context, assetID string) (io.ReadCloser, string, error)
}

func (s stubSessionAssetReader) Open(ctx context.Context, assetID string) (io.ReadCloser, string, error) {
	return s.open(ctx, assetID)
}

func TestReadSessionAssetImage(t *testing.T) {
	t.Parallel()

	newReader := func(data string, mime string, err error) providertypes.SessionAssetReader {
		return stubSessionAssetReader{open: func(_ context.Context, _ string) (io.ReadCloser, string, error) {
			if err != nil {
				return nil, "", err
			}
			return io.NopCloser(strings.NewReader(data)), mime, nil
		}}
	}

	tests := []struct {
		name                 string
		ctx                  context.Context
		assetReader          providertypes.SessionAssetReader
		asset                *providertypes.AssetRef
		remainingBudget      int64
		maxSessionAssetBytes int64
		requestBudget        RequestAssetBudget
		wantErr              string
		wantMime             string
		wantSize             int64
	}{
		{
			name:                 "success with mime from asset metadata",
			ctx:                  context.Background(),
			assetReader:          newReader("png-data", "", nil),
			asset:                &providertypes.AssetRef{ID: "a1", MimeType: "IMAGE/PNG"},
			remainingBudget:      100,
			maxSessionAssetBytes: 100,
			requestBudget:        DefaultRequestAssetBudget(),
			wantMime:             "image/png",
			wantSize:             int64(len("png-data")),
		},
		{
			name:                 "context canceled",
			ctx:                  func() context.Context { ctx, cancel := context.WithCancel(context.Background()); cancel(); return ctx }(),
			assetReader:          newReader("x", "image/png", nil),
			asset:                &providertypes.AssetRef{ID: "a1"},
			remainingBudget:      10,
			maxSessionAssetBytes: 10,
			requestBudget:        DefaultRequestAssetBudget(),
			wantErr:              "context canceled",
		},
		{
			name:                 "missing reader",
			ctx:                  context.Background(),
			assetReader:          nil,
			asset:                &providertypes.AssetRef{ID: "a1"},
			remainingBudget:      10,
			maxSessionAssetBytes: 10,
			requestBudget:        DefaultRequestAssetBudget(),
			wantErr:              "reader is not configured",
		},
		{
			name:                 "missing asset id",
			ctx:                  context.Background(),
			assetReader:          newReader("x", "image/png", nil),
			asset:                &providertypes.AssetRef{ID: ""},
			remainingBudget:      10,
			maxSessionAssetBytes: 10,
			requestBudget:        DefaultRequestAssetBudget(),
			wantErr:              "missing asset id",
		},
		{
			name:                 "remaining budget exceeded",
			ctx:                  context.Background(),
			assetReader:          newReader("x", "image/png", nil),
			asset:                &providertypes.AssetRef{ID: "a1"},
			remainingBudget:      0,
			maxSessionAssetBytes: 10,
			requestBudget:        RequestAssetBudget{MaxSessionAssetsTotalBytes: 10},
			wantErr:              "total exceeds 10 bytes",
		},
		{
			name:                 "open error",
			ctx:                  context.Background(),
			assetReader:          newReader("", "", errors.New("boom")),
			asset:                &providertypes.AssetRef{ID: "a1"},
			remainingBudget:      10,
			maxSessionAssetBytes: 10,
			requestBudget:        DefaultRequestAssetBudget(),
			wantErr:              "open session_asset \"a1\": boom",
		},
		{
			name: "single asset exceeds max bytes",
			ctx:  context.Background(),
			assetReader: stubSessionAssetReader{open: func(_ context.Context, _ string) (io.ReadCloser, string, error) {
				return io.NopCloser(bytes.NewReader([]byte("123456"))), "image/png", nil
			}},
			asset:                &providertypes.AssetRef{ID: "a1"},
			remainingBudget:      10,
			maxSessionAssetBytes: 5,
			requestBudget:        DefaultRequestAssetBudget(),
			wantErr:              "exceeds 5 bytes",
		},
		{
			name: "total budget exceeded before single-file max",
			ctx:  context.Background(),
			assetReader: stubSessionAssetReader{open: func(_ context.Context, _ string) (io.ReadCloser, string, error) {
				return io.NopCloser(bytes.NewReader([]byte("123456"))), "image/png", nil
			}},
			asset:                &providertypes.AssetRef{ID: "a1"},
			remainingBudget:      5,
			maxSessionAssetBytes: 10,
			requestBudget:        RequestAssetBudget{MaxSessionAssetsTotalBytes: 5},
			wantErr:              "total exceeds 10 bytes",
		},
		{
			name:                 "empty asset",
			ctx:                  context.Background(),
			assetReader:          newReader("", "image/png", nil),
			asset:                &providertypes.AssetRef{ID: "a1"},
			remainingBudget:      10,
			maxSessionAssetBytes: 10,
			requestBudget:        DefaultRequestAssetBudget(),
			wantErr:              "is empty",
		},
		{
			name:                 "unsupported mime",
			ctx:                  context.Background(),
			assetReader:          newReader("abc", "application/json", nil),
			asset:                &providertypes.AssetRef{ID: "a1"},
			remainingBudget:      10,
			maxSessionAssetBytes: 10,
			requestBudget:        DefaultRequestAssetBudget(),
			wantErr:              "unsupported mime type",
		},
		{
			name:                 "missing mime",
			ctx:                  context.Background(),
			assetReader:          newReader("abc", "", nil),
			asset:                &providertypes.AssetRef{ID: "a1", MimeType: "   "},
			remainingBudget:      10,
			maxSessionAssetBytes: 10,
			requestBudget:        DefaultRequestAssetBudget(),
			wantErr:              "missing mime type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mimeType, data, readBytes, err := ReadSessionAssetImage(
				tt.ctx,
				tt.assetReader,
				tt.asset,
				tt.remainingBudget,
				tt.maxSessionAssetBytes,
				tt.requestBudget,
			)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("ReadSessionAssetImage() unexpected error = %v", err)
			}
			if mimeType != tt.wantMime {
				t.Fatalf("mimeType = %q, want %q", mimeType, tt.wantMime)
			}
			if readBytes != tt.wantSize {
				t.Fatalf("readBytes = %d, want %d", readBytes, tt.wantSize)
			}
			if string(data) != "png-data" {
				t.Fatalf("unexpected data %q", string(data))
			}
		})
	}
}
