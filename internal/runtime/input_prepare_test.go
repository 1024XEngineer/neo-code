package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"neo-code/internal/config"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

func TestServicePrepareUserInputEmitsNormalizeAndAssetSaved(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	svc, _ := newPrepareTestService(t, workdir, true)

	imagePath := filepath.Join(workdir, "img.png")
	if err := os.WriteFile(imagePath, minimalPNGBytesForRuntimeTest(), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	input, err := svc.PrepareUserInput(context.Background(), PrepareInput{
		RunID:  "run-prepare-1",
		Text:   "hello",
		Images: []UserImageInput{{Path: imagePath, MimeType: "image/png"}},
	})
	if err != nil {
		t.Fatalf("PrepareUserInput() error = %v", err)
	}
	if input.SessionID == "" || input.RunID != "run-prepare-1" {
		t.Fatalf("unexpected prepared user input: %+v", input)
	}
	if len(input.Parts) != 2 || input.Parts[0].Kind != providertypes.ContentPartText || input.Parts[1].Kind != providertypes.ContentPartImage {
		t.Fatalf("unexpected prepared parts: %+v", input.Parts)
	}

	normalizedEvent := mustReadRuntimeEvent(t, svc.Events())
	if normalizedEvent.Type != EventInputNormalized {
		t.Fatalf("expected first event %q, got %q", EventInputNormalized, normalizedEvent.Type)
	}
	normalizedPayload, ok := normalizedEvent.Payload.(InputNormalizedPayload)
	if !ok || normalizedPayload.ImageCount != 1 {
		t.Fatalf("unexpected normalized payload: %#v", normalizedEvent.Payload)
	}

	assetSavedEvent := mustReadRuntimeEvent(t, svc.Events())
	if assetSavedEvent.Type != EventAssetSaved {
		t.Fatalf("expected second event %q, got %q", EventAssetSaved, assetSavedEvent.Type)
	}
	assetSavedPayload, ok := assetSavedEvent.Payload.(AssetSavedPayload)
	if !ok || assetSavedPayload.AssetID == "" || assetSavedPayload.MimeType != "image/png" {
		t.Fatalf("unexpected asset_saved payload: %#v", assetSavedEvent.Payload)
	}
}

func TestServicePrepareUserInputEmitsAssetSaveFailed(t *testing.T) {
	t.Parallel()

	svc, _ := newPrepareTestService(t, t.TempDir(), true)
	_, err := svc.PrepareUserInput(context.Background(), PrepareInput{
		RunID:  "run-prepare-2",
		Text:   "hello",
		Images: []UserImageInput{{Path: filepath.Join(t.TempDir(), "missing.png"), MimeType: "image/png"}},
	})
	if err == nil {
		t.Fatalf("expected PrepareUserInput() to fail")
	}

	failedEvent := mustReadRuntimeEvent(t, svc.Events())
	if failedEvent.Type != EventAssetSaveFailed {
		t.Fatalf("expected event %q, got %q", EventAssetSaveFailed, failedEvent.Type)
	}
	if failedEvent.SessionID == "" {
		t.Fatalf("expected asset_save_failed event to include session id")
	}
	payload, ok := failedEvent.Payload.(AssetSaveFailedPayload)
	if !ok || payload.Index != 0 {
		t.Fatalf("unexpected asset_save_failed payload: %#v", failedEvent.Payload)
	}
}

func TestServicePrepareUserInputWithoutPreparerEmitsErrorEvent(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	svc, _ := newPrepareTestService(t, workdir, false)

	_, err := svc.PrepareUserInput(context.Background(), PrepareInput{
		RunID: "run-prepare-3",
		Text:  "hello",
	})
	if err == nil {
		t.Fatalf("expected PrepareUserInput() to fail without preparer")
	}

	errorEvent := mustReadRuntimeEvent(t, svc.Events())
	if errorEvent.Type != EventError {
		t.Fatalf("expected event %q, got %q", EventError, errorEvent.Type)
	}
}

func TestServiceSubmitWithoutPreparerReturnsError(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	svc, _ := newPrepareTestService(t, workdir, false)

	err := svc.Submit(context.Background(), PrepareInput{
		RunID: "run-submit-1",
		Text:  "hello",
	})
	if err == nil {
		t.Fatalf("expected Submit() to fail without preparer")
	}

	errorEvent := mustReadRuntimeEvent(t, svc.Events())
	if errorEvent.Type != EventError {
		t.Fatalf("expected event %q, got %q", EventError, errorEvent.Type)
	}
}

func TestServicePrepareUserInputDoesNotBlockWhenPrepareEventQueueIsFull(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	svc, _ := newPrepareTestService(t, workdir, true)
	for index := 0; index < cap(svc.events); index++ {
		svc.events <- RuntimeEvent{Type: EventToolChunk}
	}

	start := time.Now()
	input, err := svc.PrepareUserInput(context.Background(), PrepareInput{
		RunID: "run-prepare-full-queue",
		Text:  "hello",
	})
	if err != nil {
		t.Fatalf("PrepareUserInput() error = %v", err)
	}
	if input.SessionID == "" {
		t.Fatalf("expected prepared session id")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("PrepareUserInput() blocked too long with full event queue: %v", elapsed)
	}
}

func TestServicePrepareUserInputAppliesRuntimeAssetLimitsToSessionStore(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	runtimeCfg := config.StaticDefaults().Runtime
	runtimeCfg.Assets.MaxSessionAssetBytes = 32
	runtimeCfg.Assets.MaxSessionAssetsTotalBytes = 32
	svc, _ := newPrepareTestServiceWithRuntimeConfig(t, workdir, true, runtimeCfg)

	imagePath := filepath.Join(workdir, "img.png")
	if err := os.WriteFile(imagePath, minimalPNGBytesForRuntimeTest(), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	_, err := svc.PrepareUserInput(context.Background(), PrepareInput{
		RunID:  "run-prepare-limit-1",
		Text:   "hello",
		Images: []UserImageInput{{Path: imagePath, MimeType: "image/png"}},
	})
	if err == nil {
		t.Fatal("expected PrepareUserInput() to fail when runtime assets limit is too small")
	}
}

func newPrepareTestService(t *testing.T, workdir string, withPreparer bool) (*Service, *agentsession.SQLiteStore) {
	return newPrepareTestServiceWithRuntimeConfig(t, workdir, withPreparer, config.StaticDefaults().Runtime)
}

func newPrepareTestServiceWithRuntimeConfig(
	t *testing.T,
	workdir string,
	withPreparer bool,
	runtimeCfg config.RuntimeConfig,
) (*Service, *agentsession.SQLiteStore) {
	t.Helper()

	cfg := config.StaticDefaults()
	cfg.Workdir = workdir
	cfg.Runtime = runtimeCfg
	loader := config.NewLoader(t.TempDir(), cfg)
	manager := config.NewManager(loader)
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}

	store := agentsession.NewSQLiteStore(t.TempDir(), workdir)
	t.Cleanup(func() {
		_ = store.Close()
	})
	svc := NewWithFactory(manager, nil, store, nil, nil)
	svc.SetSessionAssetStore(store)
	if withPreparer {
		svc.SetUserInputPreparer(NewSessionInputPreparer(store, store))
	}
	return svc, store
}

func mustReadRuntimeEvent(t *testing.T, events <-chan RuntimeEvent) RuntimeEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime event")
		return RuntimeEvent{}
	}
}

func minimalPNGBytesForRuntimeTest() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92,
		0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

func TestServicePrepareUserInputInlinesTextAssetAndReportsCount(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	svc, _ := newPrepareTestService(t, workdir, true)

	textPath := filepath.Join(workdir, "notes.md")
	if err := os.WriteFile(textPath, []byte("# Title\nbody content"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}

	input, err := svc.PrepareUserInput(context.Background(), PrepareInput{
		RunID:  "run-prepare-text-1",
		Text:   "user query",
		Images: []UserImageInput{{Path: textPath, MimeType: "text/markdown"}},
	})
	if err != nil {
		t.Fatalf("PrepareUserInput() error = %v", err)
	}
	if len(input.Parts) != 2 {
		t.Fatalf("expected 2 parts (user text + inlined markdown), got %d", len(input.Parts))
	}
	// 第一个 part 是用户文本；第二个 part 应是 markdown 内容（被内联为 text part）。
	if input.Parts[0].Kind != providertypes.ContentPartText {
		t.Errorf("Parts[0].Kind = %q, want text", input.Parts[0].Kind)
	}
	if input.Parts[1].Kind != providertypes.ContentPartText {
		t.Errorf("Parts[1].Kind = %q, want text (inlined from text/markdown asset)", input.Parts[1].Kind)
	}
	if !strings.Contains(input.Parts[1].Text, "# Title") {
		t.Errorf("Parts[1].Text = %q, want to contain markdown content", input.Parts[1].Text)
	}

	// 第一个事件必须是 input_normalized，TextAssetCount = 1。
	event := mustReadRuntimeEvent(t, svc.Events())
	if event.Type != EventInputNormalized {
		t.Fatalf("expected event %q, got %q", EventInputNormalized, event.Type)
	}
	payload, ok := event.Payload.(InputNormalizedPayload)
	if !ok {
		t.Fatalf("unexpected payload type: %T", event.Payload)
	}
	if payload.TextAssetCount != 1 {
		t.Errorf("TextAssetCount = %d, want 1", payload.TextAssetCount)
	}
	if payload.TextLength == 0 {
		t.Errorf("TextLength = 0, want > 0")
	}
}

func TestServicePrepareUserInputMixedTextAndImageAssets(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	svc, _ := newPrepareTestService(t, workdir, true)

	imagePath := filepath.Join(workdir, "img.png")
	if err := os.WriteFile(imagePath, minimalPNGBytesForRuntimeTest(), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	textPath := filepath.Join(workdir, "data.json")
	if err := os.WriteFile(textPath, []byte(`{"k":"v"}`), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}

	input, err := svc.PrepareUserInput(context.Background(), PrepareInput{
		RunID: "run-prepare-mixed-1",
		Text:  "user query",
		Images: []UserImageInput{
			{Path: imagePath, MimeType: "image/png"},
			{Path: textPath, MimeType: "application/json"},
		},
	})
	if err != nil {
		t.Fatalf("PrepareUserInput() error = %v", err)
	}
	// 期望：user text + image asset + inlined json text = 3 parts。
	if len(input.Parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %+v", len(input.Parts), input.Parts)
	}
	images, texts := 0, 0
	for _, p := range input.Parts {
		switch p.Kind {
		case providertypes.ContentPartImage:
			images++
		case providertypes.ContentPartText:
			texts++
		}
	}
	if images != 1 {
		t.Errorf("images = %d, want 1 (image kept)", images)
	}
	if texts != 2 {
		t.Errorf("texts = %d, want 2 (user text + inlined json)", texts)
	}
}

func TestServicePrepareUserInputRespectsTextAssetDisabledConfig(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	runtimeCfg := config.StaticDefaults().Runtime
	disabled := false
	runtimeCfg.Assets.TextAssetEnabled = &disabled
	svc, _ := newPrepareTestServiceWithRuntimeConfig(t, workdir, true, runtimeCfg)

	textPath := filepath.Join(workdir, "notes.md")
	if err := os.WriteFile(textPath, []byte("# Title"), 0o644); err != nil {
		t.Fatalf("write text: %v", err)
	}

	input, err := svc.PrepareUserInput(context.Background(), PrepareInput{
		RunID:  "run-prepare-text-disabled",
		Text:   "user query",
		Images: []UserImageInput{{Path: textPath, MimeType: "text/markdown"}},
	})
	if err != nil {
		t.Fatalf("PrepareUserInput() error = %v", err)
	}
	// 关闭开关时文本 asset 被丢弃，只剩用户文本 part，不保留为会失败的 image part。
	if len(input.Parts) != 1 {
		t.Fatalf("expected 1 part (text only), got %d: %+v", len(input.Parts), input.Parts)
	}
	if input.Parts[0].Kind != providertypes.ContentPartText {
		t.Errorf("Parts[0].Kind = %q, want text (text asset dropped)", input.Parts[0].Kind)
	}

	// 先读到 EventError（文本 asset 被丢弃的通知），再读到 InputNormalized。
	dropEvent := mustReadRuntimeEvent(t, svc.Events())
	if dropEvent.Type != EventError {
		t.Fatalf("expected EventError for dropped text asset, got %v", dropEvent.Type)
	}
	dropMsg, ok := dropEvent.Payload.(string)
	if !ok {
		t.Fatalf("unexpected drop event payload type: %T", dropEvent.Payload)
	}
	if !strings.Contains(dropMsg, "text asset dropped") || !strings.Contains(dropMsg, "text_asset_enabled=false") {
		t.Errorf("drop event message = %q, want contains 'text asset dropped' and 'text_asset_enabled=false'", dropMsg)
	}

	event := mustReadRuntimeEvent(t, svc.Events())
	payload, ok := event.Payload.(InputNormalizedPayload)
	if !ok {
		t.Fatalf("unexpected payload type: %T", event.Payload)
	}
	if payload.TextAssetCount != 0 {
		t.Errorf("TextAssetCount = %d, want 0 (text inline disabled)", payload.TextAssetCount)
	}
}

func TestSessionInputPreparerSetTextAssetPolicyNil(t *testing.T) {
	t.Parallel()

	var preparer sessionInputPreparer
	preparer.SetTextAssetPolicy(agentsession.DefaultTextAssetPolicy())
}
