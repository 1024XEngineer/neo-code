package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// stubAssetStore 是用于单元测试 LoadTextAsset 的最小 AssetStore 桩。
// SaveAsset 直接写内存字节流；Open/Stat 返回同样内容。
type stubAssetStore struct {
	payloads map[string]map[string][]byte // sessionID -> assetID -> bytes
	mimes    map[string]map[string]string
}

func newStubAssetStore() *stubAssetStore {
	return &stubAssetStore{
		payloads: map[string]map[string][]byte{},
		mimes:    map[string]map[string]string{},
	}
}

func (s *stubAssetStore) SaveAsset(_ context.Context, sessionID string, r io.Reader, mimeType string) (AssetMeta, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return AssetMeta{}, err
	}
	if s.payloads[sessionID] == nil {
		s.payloads[sessionID] = map[string][]byte{}
	}
	if s.mimes[sessionID] == nil {
		s.mimes[sessionID] = map[string]string{}
	}
	id := "asset-" + mimeType + "-" + itoa(len(s.payloads[sessionID]))
	s.payloads[sessionID][id] = data
	s.mimes[sessionID][id] = mimeType
	return AssetMeta{ID: id, MimeType: mimeType, Size: int64(len(data))}, nil
}

func (s *stubAssetStore) Open(_ context.Context, sessionID, assetID string) (io.ReadCloser, AssetMeta, error) {
	payloads, ok := s.payloads[sessionID]
	if !ok {
		return nil, AssetMeta{}, errors.New("session not found")
	}
	data, ok := payloads[assetID]
	if !ok {
		return nil, AssetMeta{}, errors.New("asset not found")
	}
	mime := s.mimes[sessionID][assetID]
	return io.NopCloser(bytes.NewReader(data)), AssetMeta{ID: assetID, MimeType: mime, Size: int64(len(data))}, nil
}

func (s *stubAssetStore) Stat(_ context.Context, sessionID, assetID string) (AssetMeta, error) {
	payloads, ok := s.payloads[sessionID]
	if !ok {
		return AssetMeta{}, errors.New("session not found")
	}
	data, ok := payloads[assetID]
	if !ok {
		return AssetMeta{}, errors.New("asset not found")
	}
	mime := s.mimes[sessionID][assetID]
	return AssetMeta{ID: assetID, MimeType: mime, Size: int64(len(data))}, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789"
	out := ""
	for n > 0 {
		out = string(digits[n%10]) + out
		n /= 10
	}
	return out
}

func TestLoadTextAsset_Success(t *testing.T) {
	t.Parallel()

	store := newStubAssetStore()
	ctx := context.Background()
	meta, err := store.SaveAsset(ctx, "s1", strings.NewReader("hello world"), "text/plain")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}

	result, err := LoadTextAsset(ctx, store, "s1", meta.ID, DefaultTextAssetPolicy(), TextAssetLoadOptions{
		FileName: "notes.md",
	})
	if err != nil {
		t.Fatalf("LoadTextAsset() error = %v", err)
	}
	if result.Content != "hello world" {
		t.Errorf("content = %q, want %q", result.Content, "hello world")
	}
	if result.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	if result.KeptChars != 11 {
		t.Errorf("KeptChars = %d, want 11", result.KeptChars)
	}
}

func TestLoadTextAsset_EmptyPayload(t *testing.T) {
	t.Parallel()

	store := newStubAssetStore()
	ctx := context.Background()
	meta, err := store.SaveAsset(ctx, "s1", strings.NewReader(""), "text/plain")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}

	_, err = LoadTextAsset(ctx, store, "s1", meta.ID, DefaultTextAssetPolicy(), TextAssetLoadOptions{})
	var loadErr *AssetTextLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected AssetTextLoadError, got %v", err)
	}
	if loadErr.Reason != "empty" {
		t.Errorf("Reason = %q, want %q", loadErr.Reason, "empty")
	}
}

func TestLoadTextAsset_NonUTF8Payload(t *testing.T) {
	t.Parallel()

	store := newStubAssetStore()
	ctx := context.Background()
	// 0xC3 0x28 是非法 UTF-8 序列（缺第二字节）。
	bad := []byte{0xC3, 0x28, 0xA0, 0xA1}
	meta, err := store.SaveAsset(ctx, "s1", bytes.NewReader(bad), "text/plain")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}

	_, err = LoadTextAsset(ctx, store, "s1", meta.ID, DefaultTextAssetPolicy(), TextAssetLoadOptions{})
	var loadErr *AssetTextLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected AssetTextLoadError, got %v", err)
	}
	if loadErr.Reason != "utf8" {
		t.Errorf("Reason = %q, want %q", loadErr.Reason, "utf8")
	}
}

func TestLoadTextAsset_TruncatesByBytes(t *testing.T) {
	t.Parallel()

	store := newStubAssetStore()
	ctx := context.Background()
	policy := TextAssetPolicy{
		Whitelist:         DefaultTextAssetWhitelist(),
		MaxTextAssetBytes: 8,
		MaxTextAssetChars: 1000,
	}
	original := strings.Repeat("a", 16) // 16 字节，> 8 字节上限
	meta, err := store.SaveAsset(ctx, "s1", strings.NewReader(original), "text/plain")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}

	result, err := LoadTextAsset(ctx, store, "s1", meta.ID, policy, TextAssetLoadOptions{FileName: "long.txt"})
	if err != nil {
		t.Fatalf("LoadTextAsset() error = %v", err)
	}
	if !result.Truncated {
		t.Errorf("Truncated = false, want true")
	}
	if !strings.Contains(result.Content, "[truncated:") {
		t.Errorf("content missing truncation marker, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "filename=long.txt") {
		t.Errorf("content missing sanitized filename, got %q", result.Content)
	}
	if result.OriginalBytes != 8 {
		t.Errorf("OriginalBytes = %d, want 8 (post-byte-cap)", result.OriginalBytes)
	}
}

func TestLoadTextAsset_TruncatesByChars(t *testing.T) {
	t.Parallel()

	store := newStubAssetStore()
	ctx := context.Background()
	policy := TextAssetPolicy{
		Whitelist:         DefaultTextAssetWhitelist(),
		MaxTextAssetBytes: 1024,
		MaxTextAssetChars: 4,
	}
	meta, err := store.SaveAsset(ctx, "s1", strings.NewReader("abcdefghij"), "text/plain")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}
	result, err := LoadTextAsset(ctx, store, "s1", meta.ID, policy, TextAssetLoadOptions{})
	if err != nil {
		t.Fatalf("LoadTextAsset() error = %v", err)
	}
	if !result.Truncated {
		t.Errorf("Truncated = false, want true")
	}
	// 截断提示里应包含 kept=4 chars。
	if !strings.Contains(result.Content, "kept=4 chars") {
		t.Errorf("content missing char count marker, got %q", result.Content)
	}
}

func TestLoadTextAsset_PreservesMultibyteBoundary(t *testing.T) {
	t.Parallel()

	store := newStubAssetStore()
	ctx := context.Background()
	// 5 个中文字符 = 15 字节。字符上限设为 3 → 截到 9 字节且不切碎字符。
	policy := TextAssetPolicy{
		Whitelist:         DefaultTextAssetWhitelist(),
		MaxTextAssetBytes: 1024,
		MaxTextAssetChars: 3,
	}
	meta, err := store.SaveAsset(ctx, "s1", strings.NewReader("一二三四五"), "text/plain")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}
	result, err := LoadTextAsset(ctx, store, "s1", meta.ID, policy, TextAssetLoadOptions{})
	if err != nil {
		t.Fatalf("LoadTextAsset() error = %v", err)
	}
	if result.KeptChars != 3 {
		t.Errorf("KeptChars = %d, want 3", result.KeptChars)
	}
	// 截断后剩余内容（不含截断提示）必须是 3 个完整中文字符。
	// 截断提示一定包含 newline + "[truncated:"，所以按最后一段截取前段。
	body := result.Content
	if idx := strings.Index(body, "\n\n[truncated:"); idx >= 0 {
		body = body[:idx]
	}
	if body != "一二三" {
		t.Errorf("truncated body = %q, want %q", body, "一二三")
	}
}

func TestLoadTextAsset_SanitizesFileNameInMarker(t *testing.T) {
	t.Parallel()

	store := newStubAssetStore()
	ctx := context.Background()
	policy := TextAssetPolicy{
		Whitelist:         DefaultTextAssetWhitelist(),
		MaxTextAssetBytes: 4,
		MaxTextAssetChars: 1000,
	}
	original := strings.Repeat("x", 16)
	meta, err := store.SaveAsset(ctx, "s1", strings.NewReader(original), "text/plain")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}
	result, err := LoadTextAsset(ctx, store, "s1", meta.ID, policy, TextAssetLoadOptions{
		FileName: "../etc/passwd",
	})
	if err != nil {
		t.Fatalf("LoadTextAsset() error = %v", err)
	}
	if strings.Contains(result.Content, "../") {
		t.Errorf("content still contains path separator, got %q", result.Content)
	}
	if !strings.Contains(result.Content, "filename=passwd") {
		t.Errorf("content missing sanitized basename, got %q", result.Content)
	}
}

func TestLoadTextAsset_RejectsOpenError(t *testing.T) {
	t.Parallel()

	store := newStubAssetStore()
	_, err := LoadTextAsset(context.Background(), store, "s1", "missing", DefaultTextAssetPolicy(), TextAssetLoadOptions{})
	var loadErr *AssetTextLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected AssetTextLoadError, got %v", err)
	}
	if loadErr.Reason != "open" {
		t.Errorf("Reason = %q, want %q", loadErr.Reason, "open")
	}
}

func TestLoadTextAsset_RejectsEmptyWhitelist(t *testing.T) {
	t.Parallel()

	store := newStubAssetStore()
	_, err := LoadTextAsset(
		context.Background(),
		store,
		"s1",
		"missing",
		TextAssetPolicy{Whitelist: NewTextAssetWhitelist(nil)},
		TextAssetLoadOptions{},
	)
	var loadErr *AssetTextLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected AssetTextLoadError, got %v", err)
	}
	if loadErr.Reason != "whitelist-empty" {
		t.Errorf("Reason = %q, want %q", loadErr.Reason, "whitelist-empty")
	}
}

func TestLoadTextAsset_RejectsEmptyAssetID(t *testing.T) {
	t.Parallel()

	store := newStubAssetStore()
	_, err := LoadTextAsset(context.Background(), store, "s1", "", DefaultTextAssetPolicy(), TextAssetLoadOptions{})
	var loadErr *AssetTextLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected AssetTextLoadError, got %v", err)
	}
	if loadErr.Reason != "missing-asset-id" {
		t.Errorf("Reason = %q, want %q", loadErr.Reason, "missing-asset-id")
	}
}

func TestLoadTextAsset_RejectsNilStore(t *testing.T) {
	t.Parallel()

	_, err := LoadTextAsset(context.Background(), nil, "s1", "a1", DefaultTextAssetPolicy(), TextAssetLoadOptions{})
	var loadErr *AssetTextLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("expected AssetTextLoadError, got %v", err)
	}
	if loadErr.Reason != "store-nil" {
		t.Errorf("Reason = %q, want %q", loadErr.Reason, "store-nil")
	}
}

func TestTruncateByRuneCount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		in        string
		max       int
		want      string
		wantTrunc bool
	}{
		{name: "no truncate when within limit", in: "abc", max: 10, want: "abc", wantTrunc: false},
		{name: "exact limit no truncate", in: "abcde", max: 5, want: "abcde", wantTrunc: false},
		{name: "truncate at boundary", in: "abcdef", max: 4, want: "abcd", wantTrunc: true},
		{name: "multibyte preserved", in: "一二三", max: 2, want: "一二", wantTrunc: true},
		{name: "zero max forces truncate", in: "abc", max: 0, want: "", wantTrunc: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, trunc := truncateByRuneCount(tc.in, tc.max)
			if got != tc.want {
				t.Errorf("got = %q, want %q", got, tc.want)
			}
			if trunc != tc.wantTrunc {
				t.Errorf("trunc = %v, want %v", trunc, tc.wantTrunc)
			}
		})
	}
}
