package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

// textAssetStubStore 是为 inlineTextSessionAssets 设计的最小 AssetStore 桩。
// 预置的 mimeToBytes / mimeToFileName 决定 Open 返回的内容；openErr 可注入 IO 失败。
type textAssetStubStore struct {
	mu          sync.Mutex
	payloads    map[string]map[string][]byte // sessionID -> assetID -> bytes
	mimes       map[string]map[string]string // sessionID -> assetID -> mime
	fileNames   map[string]map[string]string // sessionID -> assetID -> fileName
	openErr     error
	invalidUTF8 bool
}

func newTextAssetStubStore() *textAssetStubStore {
	return &textAssetStubStore{
		payloads:  map[string]map[string][]byte{},
		mimes:     map[string]map[string]string{},
		fileNames: map[string]map[string]string{},
	}
}

func (s *textAssetStubStore) SaveAsset(_ context.Context, sessionID string, r io.Reader, mimeType string) (agentsession.AssetMeta, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return agentsession.AssetMeta{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.payloads[sessionID] == nil {
		s.payloads[sessionID] = map[string][]byte{}
	}
	if s.mimes[sessionID] == nil {
		s.mimes[sessionID] = map[string]string{}
	}
	if s.fileNames[sessionID] == nil {
		s.fileNames[sessionID] = map[string]string{}
	}
	id := "asset-" + mimeType
	s.payloads[sessionID][id] = data
	s.mimes[sessionID][id] = mimeType
	return agentsession.AssetMeta{ID: id, MimeType: mimeType, Size: int64(len(data))}, nil
}

func (s *textAssetStubStore) Open(_ context.Context, sessionID, assetID string) (io.ReadCloser, agentsession.AssetMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openErr != nil {
		return nil, agentsession.AssetMeta{}, s.openErr
	}
	payloads, ok := s.payloads[sessionID]
	if !ok {
		return nil, agentsession.AssetMeta{}, errors.New("session not found")
	}
	data, ok := payloads[assetID]
	if !ok {
		return nil, agentsession.AssetMeta{}, errors.New("asset not found")
	}
	mime := s.mimes[sessionID][assetID]
	if s.invalidUTF8 {
		// 注入一个非 UTF-8 字节序列用于错误路径测试。
		data = []byte{0xC3, 0x28, 0xFF, 0xFE}
	}
	return io.NopCloser(bytes.NewReader(data)), agentsession.AssetMeta{ID: assetID, MimeType: mime, Size: int64(len(data))}, nil
}

func (s *textAssetStubStore) Stat(_ context.Context, sessionID, assetID string) (agentsession.AssetMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mime := s.mimes[sessionID][assetID]
	return agentsession.AssetMeta{ID: assetID, MimeType: mime, Size: int64(len(s.payloads[sessionID][assetID]))}, nil
}

// 工具方法：把 inlineTextSessionAssets 返回的结果"扁平化"为 (image parts, text parts) 数量。
func countPartsByKind(parts []providertypes.ContentPart) (images, texts int) {
	for _, p := range parts {
		switch p.Kind {
		case providertypes.ContentPartImage:
			images++
		case providertypes.ContentPartText:
			texts++
		}
	}
	return
}

func TestInlineTextSessionAssetsReplacesTextAsset(t *testing.T) {
	t.Parallel()

	store := newTextAssetStubStore()
	ctx := context.Background()
	meta, err := store.SaveAsset(ctx, "s1", strings.NewReader("# README\nhello world"), "text/markdown")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}

	parts := []providertypes.ContentPart{
		providertypes.NewTextPart("user query"),
		providertypes.NewSessionAssetImagePart(meta.ID, meta.MimeType),
	}

	out, result := inlineTextSessionAssets(ctx, store, "s1", parts, agentsession.DefaultTextAssetPolicy(), nil, nil)
	if result.Failed != 0 {
		t.Fatalf("Failed = %d, want 0", result.Failed)
	}
	if result.Inlined != 1 {
		t.Fatalf("Inlined = %d, want 1", result.Inlined)
	}
	if result.Truncated != 0 {
		t.Fatalf("Truncated = %d, want 0", result.Truncated)
	}
	images, texts := countPartsByKind(out)
	if images != 0 {
		t.Errorf("images = %d, want 0 (text asset should be replaced)", images)
	}
	if texts != 2 {
		t.Errorf("texts = %d, want 2 (user query + inlined text asset)", texts)
	}
	// 内联后的内容应包含原始 markdown 文本。
	found := false
	for _, p := range out {
		if p.Kind == providertypes.ContentPartText && strings.Contains(p.Text, "# README") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected inlined text part to contain # README, got %+v", out)
	}
}

func TestInlineTextSessionAssetsKeepsImageAsset(t *testing.T) {
	t.Parallel()

	store := newTextAssetStubStore()
	ctx := context.Background()
	meta, err := store.SaveAsset(ctx, "s1", strings.NewReader("image-bytes"), "image/png")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}

	parts := []providertypes.ContentPart{
		providertypes.NewSessionAssetImagePart(meta.ID, meta.MimeType),
	}

	out, result := inlineTextSessionAssets(ctx, store, "s1", parts, agentsession.DefaultTextAssetPolicy(), nil, nil)
	if result.Inlined != 0 {
		t.Errorf("Inlined = %d, want 0 (image asset should not be inlined)", result.Inlined)
	}
	if result.Failed != 0 {
		t.Errorf("Failed = %d, want 0", result.Failed)
	}
	images, _ := countPartsByKind(out)
	if images != 1 {
		t.Errorf("images = %d, want 1 (image asset kept)", images)
	}
}

func TestInlineTextSessionAssetsKeepsRemoteImage(t *testing.T) {
	t.Parallel()

	store := newTextAssetStubStore()
	parts := []providertypes.ContentPart{
		providertypes.NewRemoteImagePart("https://example.com/cat.png"),
	}
	out, result := inlineTextSessionAssets(context.Background(), store, "s1", parts, agentsession.DefaultTextAssetPolicy(), nil, nil)
	if result.Inlined != 0 {
		t.Errorf("Inlined = %d, want 0", result.Inlined)
	}
	images, _ := countPartsByKind(out)
	if images != 1 {
		t.Errorf("images = %d, want 1 (remote image kept)", images)
	}
}

func TestInlineTextSessionAssetsDropsOnUTF8Error(t *testing.T) {
	t.Parallel()

	store := newTextAssetStubStore()
	store.invalidUTF8 = true
	ctx := context.Background()
	meta, err := store.SaveAsset(ctx, "s1", strings.NewReader("placeholder"), "text/plain")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}

	parts := []providertypes.ContentPart{
		providertypes.NewTextPart("user text"),
		providertypes.NewSessionAssetImagePart(meta.ID, meta.MimeType),
	}

	var onErrorCalls int
	var onErrorID string
	var onErrorErr error
	out, result := inlineTextSessionAssets(ctx, store, "s1", parts, agentsession.DefaultTextAssetPolicy(), nil,
		func(assetID string, err error) {
			onErrorCalls++
			onErrorID = assetID
			onErrorErr = err
		},
	)
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
	if result.Inlined != 0 {
		t.Errorf("Inlined = %d, want 0", result.Inlined)
	}
	if onErrorCalls != 1 {
		t.Errorf("onError called %d times, want 1", onErrorCalls)
	}
	if onErrorID != meta.ID {
		t.Errorf("onError assetID = %q, want %q", onErrorID, meta.ID)
	}
	var loadErr *agentsession.AssetTextLoadError
	if !errors.As(onErrorErr, &loadErr) {
		t.Errorf("onError err type = %T, want *AssetTextLoadError", onErrorErr)
	}
	images, texts := countPartsByKind(out)
	if images != 0 {
		t.Errorf("images = %d, want 0 (failed part dropped)", images)
	}
	if texts != 1 {
		t.Errorf("texts = %d, want 1 (user text kept)", texts)
	}
}

func TestInlineTextSessionAssetsTruncatesAndAddsMarker(t *testing.T) {
	t.Parallel()

	store := newTextAssetStubStore()
	ctx := context.Background()
	policy := agentsession.TextAssetPolicy{
		Whitelist:         agentsession.DefaultTextAssetWhitelist(),
		MaxTextAssetBytes: 4,
		MaxTextAssetChars: 1000,
	}
	original := strings.Repeat("a", 16)
	meta, err := store.SaveAsset(ctx, "s1", strings.NewReader(original), "text/plain")
	if err != nil {
		t.Fatalf("SaveAsset() error = %v", err)
	}

	parts := []providertypes.ContentPart{
		providertypes.NewSessionAssetImagePart(meta.ID, meta.MimeType),
	}

	out, result := inlineTextSessionAssets(ctx, store, "s1", parts, policy, map[string]string{
		meta.ID: "big.txt",
	}, nil)
	if result.Inlined != 1 {
		t.Errorf("Inlined = %d, want 1", result.Inlined)
	}
	if result.Truncated != 1 {
		t.Errorf("Truncated = %d, want 1", result.Truncated)
	}
	images, _ := countPartsByKind(out)
	if images != 0 {
		t.Errorf("images = %d, want 0 (truncated text asset still replaced)", images)
	}
	foundMarker := false
	for _, p := range out {
		if p.Kind == providertypes.ContentPartText && strings.Contains(p.Text, "[truncated:") && strings.Contains(p.Text, "filename=big.txt") {
			foundMarker = true
		}
	}
	if !foundMarker {
		t.Errorf("expected inlined part to contain truncation marker with sanitized filename, got %+v", out)
	}
}

func TestInlineTextSessionAssetsMixedTextAndImage(t *testing.T) {
	t.Parallel()

	store := newTextAssetStubStore()
	ctx := context.Background()
	imageMeta, err := store.SaveAsset(ctx, "s1", strings.NewReader("image"), "image/png")
	if err != nil {
		t.Fatalf("SaveAsset(image) error = %v", err)
	}
	textMeta, err := store.SaveAsset(ctx, "s1", strings.NewReader("json-body"), "application/json")
	if err != nil {
		t.Fatalf("SaveAsset(text) error = %v", err)
	}

	parts := []providertypes.ContentPart{
		providertypes.NewTextPart("user text"),
		providertypes.NewSessionAssetImagePart(imageMeta.ID, imageMeta.MimeType),
		providertypes.NewSessionAssetImagePart(textMeta.ID, textMeta.MimeType),
	}
	out, result := inlineTextSessionAssets(ctx, store, "s1", parts, agentsession.DefaultTextAssetPolicy(), nil, nil)
	if result.Inlined != 1 {
		t.Errorf("Inlined = %d, want 1 (only text asset should be inlined)", result.Inlined)
	}
	images, texts := countPartsByKind(out)
	if images != 1 {
		t.Errorf("images = %d, want 1 (image kept)", images)
	}
	if texts != 2 {
		t.Errorf("texts = %d, want 2 (user text + inlined text asset)", texts)
	}
}

func TestTextAssetFileNameMap(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		images  []UserImageInput
		wantNil bool
		want    map[string]string
	}{
		{
			name:    "empty images",
			images:  nil,
			wantNil: true,
		},
		{
			name: "skip empty asset id",
			images: []UserImageInput{
				{AssetID: "", Path: "/x.png"},
			},
			wantNil: true,
		},
		{
			name: "skip empty path",
			images: []UserImageInput{
				{AssetID: "a1", Path: "  "},
			},
			wantNil: true,
		},
		{
			name: "map populated",
			images: []UserImageInput{
				{AssetID: "a1", Path: "/tmp/notes.md"},
				{AssetID: "a2", Path: "data.csv"},
			},
			want: map[string]string{
				"a1": "/tmp/notes.md",
				"a2": "data.csv",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := textAssetFileNameMap(tc.images)
			if tc.wantNil {
				if got != nil {
					t.Errorf("got = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("got nil, want %+v", tc.want)
			}
			if len(got) != len(tc.want) {
				t.Errorf("len(got)=%d, want %d", len(got), len(tc.want))
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("got[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestInlineTextSessionAssetsGuards(t *testing.T) {
	t.Parallel()

	parts := []providertypes.ContentPart{providertypes.NewTextPart("keep")}
	if out, result := inlineTextSessionAssets(
		context.Background(), nil, "s1", parts, agentsession.DefaultTextAssetPolicy(), nil, nil,
	); len(out) != 1 || len(result.Parts) != 0 {
		t.Fatalf("nil store result = out:%+v result:%+v", out, result)
	}
	if out, result := inlineTextSessionAssets(
		context.Background(), newTextAssetStubStore(), "s1", nil, agentsession.DefaultTextAssetPolicy(), nil, nil,
	); out != nil || result.Inlined != 0 {
		t.Fatalf("empty parts result = out:%+v result:%+v", out, result)
	}

	malformed := []providertypes.ContentPart{
		{Kind: providertypes.ContentPartImage},
		{Kind: providertypes.ContentPartImage, Image: &providertypes.ImagePart{SourceType: providertypes.ImageSourceSessionAsset}},
		providertypes.NewSessionAssetImagePart(" ", "text/plain"),
		providertypes.NewSessionAssetImagePart("image-1", "image/png"),
	}
	out, result := inlineTextSessionAssets(
		context.Background(), newTextAssetStubStore(), "s1", malformed, agentsession.DefaultTextAssetPolicy(), nil, nil,
	)
	if len(out) != len(malformed) || result.Inlined != 0 || result.Failed != 0 {
		t.Fatalf("malformed parts result = out:%+v result:%+v", out, result)
	}
}

func TestInlineUserInputTextAssetsNilService(t *testing.T) {
	t.Parallel()

	parts := []providertypes.ContentPart{providertypes.NewTextPart("keep")}
	var service *Service
	result := service.inlineUserInputTextAssets(
		context.Background(), "s1", PrepareInput{}, parts, agentsession.DefaultTextAssetPolicy(),
	)
	if len(result.Parts) != 1 || result.Parts[0].Text != "keep" {
		t.Fatalf("nil service result = %+v", result)
	}
}

func TestDropTextAssetImageParts(t *testing.T) {
	t.Parallel()

	if got := dropTextAssetImageParts(nil, agentsession.DefaultTextAssetPolicy(), nil); got != nil {
		t.Fatalf("empty parts = %+v, want nil", got)
	}
	parts := []providertypes.ContentPart{
		providertypes.NewTextPart("keep"),
		providertypes.NewRemoteImagePart("https://example.com/image.png"),
		providertypes.NewSessionAssetImagePart("image-1", "image/png"),
		{Kind: providertypes.ContentPartImage},
		{Kind: providertypes.ContentPartImage, Image: &providertypes.ImagePart{SourceType: providertypes.ImageSourceSessionAsset}},
		providertypes.NewSessionAssetImagePart(" ", "text/plain"),
		providertypes.NewSessionAssetImagePart("text-1", "text/plain"),
	}
	var droppedID, droppedMIME string
	got := dropTextAssetImageParts(parts, agentsession.DefaultTextAssetPolicy(), func(assetID, mime string) {
		droppedID = assetID
		droppedMIME = mime
	})
	if len(got) != len(parts)-1 {
		t.Fatalf("drop result length = %d, want %d: %+v", len(got), len(parts)-1, got)
	}
	if droppedID != "text-1" || droppedMIME != "text/plain" {
		t.Fatalf("drop callback = (%q, %q)", droppedID, droppedMIME)
	}
	for _, part := range got {
		if part.Image != nil && part.Image.Asset != nil && part.Image.Asset.ID == "text-1" {
			t.Fatal("text asset was not dropped")
		}
	}
}
