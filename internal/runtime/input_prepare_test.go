package runtime

import (
	"context"
	"os"
	"path/filepath"
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

func newPrepareTestService(t *testing.T, workdir string, withPreparer bool) (*Service, *agentsession.SQLiteStore) {
	t.Helper()

	cfg := config.StaticDefaults()
	cfg.Workdir = workdir
	loader := config.NewLoader(t.TempDir(), cfg)
	manager := config.NewManager(loader)
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}

	store := agentsession.NewStore(t.TempDir(), workdir)
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
