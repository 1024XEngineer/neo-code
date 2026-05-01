package session

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	providertypes "neo-code/internal/provider/types"
)

func TestInputPreparerPrepareTextOnly(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := newInputPreparerTestStore(t, workdir)
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
	loaded, err := store.LoadSession(context.Background(), result.SessionID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.TaskState.VerificationProfile != VerificationProfileTaskOnly {
		t.Fatalf("verification profile = %q, want %q", loaded.TaskState.VerificationProfile, VerificationProfileTaskOnly)
	}
}

func TestInputPreparerPrepareTextAndImage(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := newInputPreparerTestStore(t, workdir)
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
	store := newInputPreparerTestStore(t, workdir)
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
	store := newInputPreparerTestStore(t, workdir)
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

	session, err := store.LoadSession(context.Background(), result.SessionID)
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
	store := newInputPreparerTestStore(t, workdir)

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
		if err := createSessionForPreparerTest(context.Background(), store, existing); err != nil {
			t.Fatalf("createSessionForPreparerTest() error = %v", err)
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

		if _, loadErr := store.LoadSession(context.Background(), existing.ID); loadErr != nil {
			t.Fatalf("expected existing session to remain, load error = %v", loadErr)
		}
	})

	t.Run("existing session cleanup removes previously saved assets on later failure", func(t *testing.T) {
		existing := NewWithWorkdir("existing-cleanup", workdir)
		if err := createSessionForPreparerTest(context.Background(), store, existing); err != nil {
			t.Fatalf("createSessionForPreparerTest() error = %v", err)
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

		entries, readErr := os.ReadDir(filepath.Join(store.assetsDir, existing.ID))
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
		if err := createSessionForPreparerTest(context.Background(), store, existing); err != nil {
			t.Fatalf("createSessionForPreparerTest() error = %v", err)
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

		loaded, loadErr := store.LoadSession(context.Background(), existing.ID)
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
	store := newInputPreparerTestStore(t, workdir)
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

	store := newInputPreparerTestStore(t, defaultWorkdir)
	session := NewWithWorkdir("existing", currentWorkdir)
	session.Provider = "provider-a"
	session.Model = "model-a"
	session.TokenInputTotal = 13
	session.TokenOutputTotal = 21
	session.TaskState = TaskState{
		Goal:      "keep original state",
		Progress:  []string{"captured"},
		NextStep:  "verify workdir-only update",
		Blockers:  []string{"none"},
		Decisions: []string{"preserve head fields"},
	}
	session.Todos = []TodoItem{{
		ID:        "todo-preserve",
		Content:   "must survive prepare workdir update",
		Status:    TodoStatusInProgress,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}}
	if err := createSessionForPreparerTest(context.Background(), store, session); err != nil {
		t.Fatalf("createSessionForPreparerTest() error = %v", err)
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

	loaded, err := store.LoadSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Workdir != targetWorkdir {
		t.Fatalf("expected persisted workdir %q, got %q", targetWorkdir, loaded.Workdir)
	}
	if loaded.Provider != session.Provider {
		t.Fatalf("expected provider %q, got %q", session.Provider, loaded.Provider)
	}
	if loaded.Model != session.Model {
		t.Fatalf("expected model %q, got %q", session.Model, loaded.Model)
	}
	if loaded.TokenInputTotal != session.TokenInputTotal || loaded.TokenOutputTotal != session.TokenOutputTotal {
		t.Fatalf("expected token totals %d/%d, got %d/%d",
			session.TokenInputTotal,
			session.TokenOutputTotal,
			loaded.TokenInputTotal,
			loaded.TokenOutputTotal,
		)
	}
	if loaded.TaskState.Goal != session.TaskState.Goal || loaded.TaskState.NextStep != session.TaskState.NextStep {
		t.Fatalf("expected task state to remain unchanged, got %+v", loaded.TaskState)
	}
	if len(loaded.Todos) != 1 || loaded.Todos[0].ID != session.Todos[0].ID || loaded.Todos[0].Status != session.Todos[0].Status {
		t.Fatalf("expected todos to remain unchanged, got %+v", loaded.Todos)
	}
}

func TestInputPreparerPreparePromotesDefaultSessionTitle(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := newInputPreparerTestStore(t, workdir)
	preparer := NewInputPreparer(store, store)

	session := NewWithWorkdir(defaultSessionTitle, workdir)
	if err := createSessionForPreparerTest(context.Background(), store, session); err != nil {
		t.Fatalf("createSessionForPreparerTest() error = %v", err)
	}

	result, err := preparer.Prepare(context.Background(), PrepareInput{
		SessionID:      session.ID,
		Text:           "investigate session title regression",
		DefaultWorkdir: workdir,
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if result.SessionID != session.ID {
		t.Fatalf("expected session id %q, got %q", session.ID, result.SessionID)
	}

	loaded, err := store.LoadSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.Title != "investigate session title regression" {
		t.Fatalf("expected promoted title, got %q", loaded.Title)
	}
}

func TestInputPreparerPrepareKeepsExistingNonDefaultTitle(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	store := newInputPreparerTestStore(t, workdir)
	preparer := NewInputPreparer(store, store)

	session := NewWithWorkdir("Already Titled", workdir)
	if err := createSessionForPreparerTest(context.Background(), store, session); err != nil {
		t.Fatalf("createSessionForPreparerTest() error = %v", err)
	}

	if _, err := preparer.Prepare(context.Background(), PrepareInput{
		SessionID:      session.ID,
		Text:           "follow-up prompt should not replace title",
		DefaultWorkdir: workdir,
	}); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	loaded, err := store.LoadSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.Title != "Already Titled" {
		t.Fatalf("expected original title to be kept, got %q", loaded.Title)
	}
}

func TestInputPreparerShouldPromoteSessionTitle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		current string
		next    string
		want    bool
	}{
		{name: "promote default", current: "New Session", next: "real title", want: true},
		{name: "reject empty", current: "New Session", next: "   ", want: false},
		{name: "reject default next", current: "New Session", next: "new session", want: false},
		{name: "reject non-default current", current: "Named", next: "other", want: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldPromoteSessionTitle(tc.current, tc.next); got != tc.want {
				t.Fatalf("shouldPromoteSessionTitle(%q,%q)=%v, want %v", tc.current, tc.next, got, tc.want)
			}
		})
	}
}

func TestInputPreparerPrepareWorkdirUpdatePreservesConcurrentSessionHeadChanges(t *testing.T) {
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

	store := newInputPreparerTestStore(t, defaultWorkdir)
	session := NewWithWorkdir("existing", currentWorkdir)
	session.Provider = "provider-before"
	session.Model = "model-before"
	session.TokenInputTotal = 3
	session.TokenOutputTotal = 5
	if err := createSessionForPreparerTest(context.Background(), store, session); err != nil {
		t.Fatalf("createSessionForPreparerTest() error = %v", err)
	}

	concurrentState := UpdateSessionStateInput{
		SessionID: session.ID,
		Title:     session.Title,
		UpdatedAt: session.UpdatedAt.Add(time.Minute),
		Head: SessionHead{
			Provider: "provider-after",
			Model:    "model-after",
			Workdir:  currentWorkdir,
			TaskState: TaskState{
				Goal:     "newer task state",
				NextStep: "must survive workdir update",
			},
			Todos: []TodoItem{{
				ID:        "todo-newer",
				Content:   "written by concurrent run",
				Status:    TodoStatusCompleted,
				CreatedAt: session.CreatedAt,
				UpdatedAt: session.UpdatedAt.Add(time.Minute),
			}},
			TokenInputTotal:  55,
			TokenOutputTotal: 89,
		},
	}

	preparerStore := &workdirRaceStore{
		SQLiteStore: store,
		beforeWorkdirUpdate: func(ctx context.Context) error {
			return store.UpdateSessionState(ctx, concurrentState)
		},
	}
	preparer := NewInputPreparer(preparerStore, store)

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

	loaded, err := store.LoadSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Workdir != targetWorkdir {
		t.Fatalf("expected persisted workdir %q, got %q", targetWorkdir, loaded.Workdir)
	}
	if loaded.Provider != concurrentState.Head.Provider || loaded.Model != concurrentState.Head.Model {
		t.Fatalf("expected provider/model %q/%q, got %q/%q",
			concurrentState.Head.Provider, concurrentState.Head.Model, loaded.Provider, loaded.Model)
	}
	if loaded.TokenInputTotal != concurrentState.Head.TokenInputTotal || loaded.TokenOutputTotal != concurrentState.Head.TokenOutputTotal {
		t.Fatalf("expected token totals %d/%d, got %d/%d",
			concurrentState.Head.TokenInputTotal,
			concurrentState.Head.TokenOutputTotal,
			loaded.TokenInputTotal,
			loaded.TokenOutputTotal,
		)
	}
	if loaded.TaskState.Goal != concurrentState.Head.TaskState.Goal || loaded.TaskState.NextStep != concurrentState.Head.TaskState.NextStep {
		t.Fatalf("expected newer task state to survive, got %+v", loaded.TaskState)
	}
	if len(loaded.Todos) != 1 || loaded.Todos[0].ID != concurrentState.Head.Todos[0].ID || loaded.Todos[0].Status != concurrentState.Head.Todos[0].Status {
		t.Fatalf("expected newer todos to survive, got %+v", loaded.Todos)
	}
}

func createSessionForPreparerTest(ctx context.Context, store *SQLiteStore, session Session) error {
	_, err := store.CreateSession(ctx, CreateSessionInput{
		ID:        session.ID,
		Title:     session.Title,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
		Head:      session.HeadSnapshot(),
	})
	return err
}

func newInputPreparerTestStore(t *testing.T, workdir string) *SQLiteStore {
	t.Helper()

	store := NewSQLiteStore(t.TempDir(), workdir)
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

type workdirRaceStore struct {
	*SQLiteStore
	beforeWorkdirUpdate func(ctx context.Context) error
}

// UpdateSessionWorkdir 在真正更新 workdir 前注入一次更晚的会话头写入，用于回归 stale snapshot 覆盖问题。
func (s *workdirRaceStore) UpdateSessionWorkdir(ctx context.Context, input UpdateSessionWorkdirInput) error {
	if s.beforeWorkdirUpdate != nil {
		if err := s.beforeWorkdirUpdate(ctx); err != nil {
			return err
		}
	}
	return s.SQLiteStore.UpdateSessionWorkdir(ctx, input)
}
