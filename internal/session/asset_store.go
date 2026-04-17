package session

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// AssetMeta 描述会话附件的最小元数据。
type AssetMeta struct {
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

// AssetStore 定义会话级附件读写契约。
type AssetStore interface {
	SaveAsset(ctx context.Context, sessionID string, r io.Reader, mimeType string) (AssetMeta, error)
	Open(ctx context.Context, sessionID string, assetID string) (io.ReadCloser, AssetMeta, error)
	Stat(ctx context.Context, sessionID string, assetID string) (AssetMeta, error)
}

// newAssetMeta 生成新的会话附件元数据，并校验 MIME 约束。
func newAssetMeta(mimeType string) (AssetMeta, error) {
	normalized := strings.ToLower(strings.TrimSpace(mimeType))
	if normalized == "" {
		return AssetMeta{}, fmt.Errorf("session: asset mime type is empty")
	}
	if !strings.HasPrefix(normalized, "image/") {
		return AssetMeta{}, fmt.Errorf("session: unsupported asset mime type %q", mimeType)
	}
	return AssetMeta{
		ID:       NewID("asset"),
		MimeType: normalized,
	}, nil
}
