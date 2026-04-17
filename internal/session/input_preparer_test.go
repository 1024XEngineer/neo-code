package session

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	providertypes "neo-code/internal/provider/types"
)

func TestInputPreparerPrepareTextOnly(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := NewStore(t.TempDir(), workdir)
	preparer := NewInputPreparer(store, store)

	result, err := preparer.Prepare(context.Background(), PrepareInput{
		Text:           "hello world",
		DefaultWorkdir: workdir,
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if result.SessionID == "" {
		t.Fatalf("expected non-empty session id")
	}
	if len(result.Parts) != 1 || result.Parts[0].Kind != providertypes.ContentPartText || result.Parts[0].Text != "hello world" {
		t.Fatalf("unexpected prepared parts: %+v", result.Parts)
	}
	if len(result.SavedAssets) != 0 {
		t.Fatalf("expected no saved assets, got %+v", result.SavedAssets)
	}
}

func TestInputPreparerPrepareTextAndImage(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := NewStore(t.TempDir(), workdir)
	preparer := NewInputPreparer(store, store)

	imagePath := filepath.Join(workdir, "img.png")
	payload := minimalPNGBytes()
	if err := os.WriteFile(imagePath, payload, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	result, err := preparer.Prepare(context.Background(), PrepareInput{
		Text:           "with image",
		Images:         []PrepareImageInput{{Path: imagePath, MimeType: "image/png"}},
		DefaultWorkdir: workdir,
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(result.SavedAssets) != 1 {
		t.Fatalf("expected one saved asset, got %+v", result.SavedAssets)
	}
	if len(result.Parts) != 2 {
		t.Fatalf("expected 2 parts, got %+v", result.Parts)
	}
	imagePart := result.Parts[1]
	if imagePart.Kind != providertypes.ContentPartImage || imagePart.Image == nil || imagePart.Image.Asset == nil {
		t.Fatalf("expected session asset image part, got %+v", imagePart)
	}
	if imagePart.Image.Asset.ID != result.SavedAssets[0].ID {
		t.Fatalf("expected image part asset id %q, got %+v", result.SavedAssets[0].ID, imagePart.Image.Asset)
	}

	rc, meta, err := store.Open(context.Background(), result.SessionID, result.SavedAssets[0].ID)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if meta.MimeType != "image/png" || string(got) != string(payload) {
		t.Fatalf("unexpected stored asset mime=%q payload=%q", meta.MimeType, string(got))
	}
}

func TestInputPreparerPrepareImageInfersMimeWhenMissing(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := NewStore(t.TempDir(), workdir)
	preparer := NewInputPreparer(store, store)

	imagePath := filepath.Join(workdir, "auto.png")
	if err := os.WriteFile(imagePath, minimalPNGBytes(), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	result, err := preparer.Prepare(context.Background(), PrepareInput{
		Text:           "infer mime",
		Images:         []PrepareImageInput{{Path: imagePath}},
		DefaultWorkdir: workdir,
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(result.SavedAssets) != 1 {
		t.Fatalf("expected one saved asset, got %+v", result.SavedAssets)
	}
	if result.SavedAssets[0].MimeType != "image/png" {
		t.Fatalf("expected inferred mime image/png, got %q", result.SavedAssets[0].MimeType)
	}
}

func TestInputPreparerPrepareImageOnlyUsesImageTitle(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := NewStore(t.TempDir(), workdir)
	preparer := NewInputPreparer(store, store)

	imagePath := filepath.Join(workdir, "only.png")
	if err := os.WriteFile(imagePath, minimalPNGBytes(), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	result, err := preparer.Prepare(context.Background(), PrepareInput{
		Images:         []PrepareImageInput{{Path: imagePath, MimeType: "image/png"}},
		DefaultWorkdir: workdir,
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(result.Parts) != 1 || result.Parts[0].Kind != providertypes.ContentPartImage {
		t.Fatalf("expected one image part, got %+v", result.Parts)
	}

	session, err := store.Load(context.Background(), result.SessionID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if session.Title != imageOnlySessionTitle {
		t.Fatalf("expected image-only title %q, got %q", imageOnlySessionTitle, session.Title)
	}
}

func TestInputPreparerPrepareErrors(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := NewStore(t.TempDir(), workdir)

	t.Run("missing store", func(t *testing.T) {
		preparer := NewInputPreparer(nil, nil)
		if _, err := preparer.Prepare(context.Background(), PrepareInput{Text: "x", DefaultWorkdir: workdir}); err == nil {
			t.Fatalf("expected missing store error")
		}
	})

	t.Run("missing asset store", func(t *testing.T) {
		preparer := NewInputPreparer(store, nil)
		_, err := preparer.Prepare(context.Background(), PrepareInput{
			Images:         []PrepareImageInput{{Path: "x", MimeType: "image/png"}},
			DefaultWorkdir: workdir,
		})
		if err == nil {
			t.Fatalf("expected missing asset store error")
		}
	})

	t.Run("empty content", func(t *testing.T) {
		preparer := NewInputPreparer(store, store)
		if _, err := preparer.Prepare(context.Background(), PrepareInput{DefaultWorkdir: workdir}); err == nil {
			t.Fatalf("expected empty content error")
		}
	})

	t.Run("asset save error is structured", func(t *testing.T) {
		preparer := NewInputPreparer(store, store)
		_, err := preparer.Prepare(context.Background(), PrepareInput{
			Images:         []PrepareImageInput{{Path: "not-found.png", MimeType: "image/png"}},
			DefaultWorkdir: workdir,
		})
		if err == nil {
			t.Fatalf("expected asset save error")
		}
		var saveErr *AssetSaveError
		if !errors.As(err, &saveErr) {
			t.Fatalf("expected AssetSaveError, got %T %v", err, err)
		}
		if saveErr.Index != 0 {
			t.Fatalf("expected save error index 0, got %d", saveErr.Index)
		}
		if saveErr.SessionID == "" {
			t.Fatalf("expected save error session id")
		}
	})

	t.Run("new session is rolled back when asset save fails", func(t *testing.T) {
		preparer := NewInputPreparer(store, store)
		_, err := preparer.Prepare(context.Background(), PrepareInput{
			Images:         []PrepareImageInput{{Path: "not-found.png", MimeType: "image/png"}},
			DefaultWorkdir: workdir,
		})
		if err == nil {
			t.Fatalf("expected asset save error")
		}

		summaries, listErr := store.ListSummaries(context.Background())
		if listErr != nil {
			t.Fatalf("ListSummaries() error = %v", listErr)
		}
		if len(summaries) != 0 {
			t.Fatalf("expected no persisted session after rollback, got %+v", summaries)
		}
	})

	t.Run("existing session is kept when asset save fails", func(t *testing.T) {
		existing := NewWithWorkdir("existing", workdir)
		if err := store.Save(context.Background(), &existing); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		preparer := NewInputPreparer(store, store)
		_, err := preparer.Prepare(context.Background(), PrepareInput{
			SessionID:      existing.ID,
			Images:         []PrepareImageInput{{Path: "not-found.png", MimeType: "image/png"}},
			DefaultWorkdir: workdir,
		})
		if err == nil {
			t.Fatalf("expected asset save error")
		}

		if _, loadErr := store.Load(context.Background(), existing.ID); loadErr != nil {
			t.Fatalf("expected existing session to remain, load error = %v", loadErr)
		}
	})

	t.Run("existing session cleanup removes previously saved assets on later failure", func(t *testing.T) {
		existing := NewWithWorkdir("existing-cleanup", workdir)
		if err := store.Save(context.Background(), &existing); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		okImage := filepath.Join(workdir, "ok.png")
		if err := os.WriteFile(okImage, minimalPNGBytes(), 0o644); err != nil {
			t.Fatalf("write image: %v", err)
		}

		preparer := NewInputPreparer(store, store)
		_, err := preparer.Prepare(context.Background(), PrepareInput{
			SessionID: existing.ID,
			Text:      "cleanup",
			Images: []PrepareImageInput{
				{Path: okImage},
				{Path: "not-found.png", MimeType: "image/png"},
			},
			DefaultWorkdir: workdir,
		})
		if err == nil {
			t.Fatalf("expected prepare error")
		}

		entries, readErr := os.ReadDir(store.assetsDir(existing.ID))
		if readErr != nil {
			t.Fatalf("ReadDir() error = %v", readErr)
		}
		if len(entries) != 0 {
			t.Fatalf("expected no leftover assets, got %d files", len(entries))
		}
	})

	t.Run("existing session workdir change is not persisted when prepare fails", func(t *testing.T) {
		currentWorkdir := filepath.Join(workdir, "current")
		if err := os.MkdirAll(currentWorkdir, 0o755); err != nil {
			t.Fatalf("mkdir current workdir: %v", err)
		}
		targetWorkdir := filepath.Join(currentWorkdir, "nested")
		if err := os.MkdirAll(targetWorkdir, 0o755); err != nil {
			t.Fatalf("mkdir nested workdir: %v", err)
		}

		existing := NewWithWorkdir("existing-workdir", currentWorkdir)
		if err := store.Save(context.Background(), &existing); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		preparer := NewInputPreparer(store, store)
		_, err := preparer.Prepare(context.Background(), PrepareInput{
			SessionID:        existing.ID,
			Text:             "will fail",
			RequestedWorkdir: "nested",
			Images:           []PrepareImageInput{{Path: "not-found.png", MimeType: "image/png"}},
			DefaultWorkdir:   workdir,
		})
		if err == nil {
			t.Fatalf("expected prepare error")
		}

		loaded, loadErr := store.Load(context.Background(), existing.ID)
		if loadErr != nil {
			t.Fatalf("Load() error = %v", loadErr)
		}
		if loaded.Workdir != currentWorkdir {
			t.Fatalf("expected workdir to stay %q, got %q", currentWorkdir, loaded.Workdir)
		}
	})
}

func TestInputPreparerPrepareImagePathAndMimeValidation(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := NewStore(t.TempDir(), workdir)
	preparer := NewInputPreparer(store, store)

	t.Run("relative path is resolved by workdir", func(t *testing.T) {
		relativeDir := filepath.Join(workdir, "images")
		if err := os.MkdirAll(relativeDir, 0o755); err != nil {
			t.Fatalf("mkdir images: %v", err)
		}
		imagePath := filepath.Join(relativeDir, "a.png")
		if err := os.WriteFile(imagePath, minimalPNGBytes(), 0o644); err != nil {
			t.Fatalf("write image: %v", err)
		}

		result, err := preparer.Prepare(context.Background(), PrepareInput{
			Text:           "relative path",
			Images:         []PrepareImageInput{{Path: filepath.Join("images", "a.png")}},
			DefaultWorkdir: workdir,
		})
		if err != nil {
			t.Fatalf("Prepare() error = %v", err)
		}
		if len(result.SavedAssets) != 1 || result.SavedAssets[0].MimeType != "image/png" {
			t.Fatalf("unexpected saved assets: %+v", result.SavedAssets)
		}
	})

	t.Run("path outside workdir is rejected", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "outside.png")
		if err := os.WriteFile(outside, minimalPNGBytes(), 0o644); err != nil {
			t.Fatalf("write outside image: %v", err)
		}

		_, err := preparer.Prepare(context.Background(), PrepareInput{
			Text:           "outside",
			Images:         []PrepareImageInput{{Path: outside, MimeType: "image/png"}},
			DefaultWorkdir: workdir,
		})
		if err == nil {
			t.Fatalf("expected outside workdir error")
		}
		if !strings.Contains(err.Error(), "escapes base dir") {
			t.Fatalf("expected escapes base dir error, got %v", err)
		}
	})

	t.Run("declared mime mismatch with file header is rejected", func(t *testing.T) {
		imagePath := filepath.Join(workdir, "declared-mismatch.png")
		if err := os.WriteFile(imagePath, minimalPNGBytes(), 0o644); err != nil {
			t.Fatalf("write image: %v", err)
		}

		_, err := preparer.Prepare(context.Background(), PrepareInput{
			Text:           "declared mismatch",
			Images:         []PrepareImageInput{{Path: imagePath, MimeType: "image/jpeg"}},
			DefaultWorkdir: workdir,
		})
		if err == nil {
			t.Fatalf("expected mime mismatch error")
		}
		if !strings.Contains(err.Error(), "mismatches detected") {
			t.Fatalf("expected mismatch error, got %v", err)
		}
	})
}

func TestAssetSaveErrorMethods(t *testing.T) {
	t.Parallel()

	if err := (*AssetSaveError)(nil).Unwrap(); err != nil {
		t.Fatalf("expected nil asset save error unwrap to return nil, got %v", err)
	}
	if msg := (*AssetSaveError)(nil).Error(); msg != "session: asset save failed" {
		t.Fatalf("unexpected nil asset save error message: %q", msg)
	}

	inner := errors.New("boom")
	assetErr := &AssetSaveError{
		SessionID: "session-1",
		Index:     2,
		Path:      "/tmp/image.png",
		Err:       inner,
	}
	if !errors.Is(assetErr, inner) {
		t.Fatalf("expected asset save error to unwrap inner error")
	}
	if !strings.Contains(assetErr.Error(), "image.png") || !strings.Contains(assetErr.Error(), "index 2") {
		t.Fatalf("unexpected asset save error message: %q", assetErr.Error())
	}
}

func minimalPNGBytes() []byte {
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

func TestInputPreparerPrepareUpdatesExistingSessionWorkdir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	defaultWorkdir := filepath.Join(base, "workspace")
	if err := os.MkdirAll(defaultWorkdir, 0o755); err != nil {
		t.Fatalf("mkdir default workdir: %v", err)
	}
	currentWorkdir := filepath.Join(defaultWorkdir, "current")
	if err := os.MkdirAll(currentWorkdir, 0o755); err != nil {
		t.Fatalf("mkdir current workdir: %v", err)
	}
	targetWorkdir := filepath.Join(currentWorkdir, "nested")
	if err := os.MkdirAll(targetWorkdir, 0o755); err != nil {
		t.Fatalf("mkdir nested workdir: %v", err)
	}

	store := NewStore(t.TempDir(), defaultWorkdir)
	session := NewWithWorkdir("existing", currentWorkdir)
	if err := store.Save(context.Background(), &session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	preparer := NewInputPreparer(store, store)
	result, err := preparer.Prepare(context.Background(), PrepareInput{
		SessionID:        session.ID,
		Text:             "update workdir",
		DefaultWorkdir:   defaultWorkdir,
		RequestedWorkdir: "nested",
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if result.Workdir != targetWorkdir {
		t.Fatalf("expected target workdir %q, got %q", targetWorkdir, result.Workdir)
	}

	loaded, err := store.Load(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Workdir != targetWorkdir {
		t.Fatalf("expected persisted workdir %q, got %q", targetWorkdir, loaded.Workdir)
	}
}
