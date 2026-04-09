package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	contextcompact "neo-code/internal/context/compact"
	providertypes "neo-code/internal/provider/types"
	agentsession "neo-code/internal/session"
)

func TestServiceCompactValidationAndStoreErrors(t *testing.T) {
	t.Run("rejects canceled context", func(t *testing.T) {
		service := &Service{}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := service.Compact(ctx, CompactInput{SessionID: "session-1"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled error, got %v", err)
		}
	})

	t.Run("rejects empty session id", func(t *testing.T) {
		service := &Service{}
		_, err := service.Compact(context.Background(), CompactInput{})
		if err == nil || err.Error() != "runtime: compact session_id is empty" {
			t.Fatalf("expected empty session id error, got %v", err)
		}
	})

	t.Run("fails when session store is nil", func(t *testing.T) {
		manager := newRuntimeConfigManager(t)
		service := NewWithFactory(manager, nil, nil, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)

		_, err := service.Compact(context.Background(), CompactInput{SessionID: "session-1"})
		if err == nil || err.Error() != "runtime: session store is nil" {
			t.Fatalf("expected nil store error, got %v", err)
		}
	})
}

func TestRunCompactForSessionNonFatalErrorBranches(t *testing.T) {
	t.Run("default runner resolution error becomes compact event when failOnError false", func(t *testing.T) {
		manager := newRuntimeConfigManager(t)
		service := NewWithFactory(manager, nil, newMemoryStore(), &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
		cfg := manager.Get()
		session := agentsession.Session{
			ID:       "session-default-runner-error",
			Provider: "missing-provider",
			Model:    "missing-model",
			Messages: []providertypes.Message{{Role: providertypes.RoleUser, Content: "hello"}},
		}

		returned, result, err := service.runCompactForSession(context.Background(), "run-1", session, cfg, false)
		if err != nil {
			t.Fatalf("expected failOnError=false to swallow error, got %v", err)
		}
		if returned.ID != session.ID || result.Applied || len(result.Messages) != 0 {
			t.Fatalf("expected original session and zero result, got session=%+v result=%+v", returned, result)
		}

		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{EventCompactError})
	})

	t.Run("runner failure emits compact error without returning error when non fatal", func(t *testing.T) {
		manager := newRuntimeConfigManager(t)
		service := NewWithFactory(manager, nil, newMemoryStore(), &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
		service.compactRunner = &stubCompactRunner{err: errors.New("compact runner failed")}
		session := agentsession.Session{
			ID:       "session-runner-error",
			Workdir:  t.TempDir(),
			Messages: []providertypes.Message{{Role: providertypes.RoleUser, Content: "hello"}},
		}

		returned, result, err := service.runCompactForSession(context.Background(), "run-2", session, manager.Get(), false)
		if err != nil {
			t.Fatalf("expected non-fatal runner error to be swallowed, got %v", err)
		}
		if returned.ID != session.ID || result.Applied || len(result.Messages) != 0 {
			t.Fatalf("expected original session and zero result, got session=%+v result=%+v", returned, result)
		}

		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{EventCompactStart, EventCompactError})
	})

	t.Run("save failure restores original messages when non fatal", func(t *testing.T) {
		manager := newRuntimeConfigManager(t)
		baseStore := newMemoryStore()
		store := &failingStore{
			Store:      baseStore,
			saveErr:    errors.New("save compacted session failed"),
			failOnSave: 1,
		}
		service := NewWithFactory(manager, nil, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
		service.compactRunner = &stubCompactRunner{
			result: contextcompact.Result{
				Applied: true,
				Messages: []providertypes.Message{
					{Role: providertypes.RoleAssistant, Content: "compacted"},
				},
				Metrics: contextcompact.Metrics{
					BeforeChars: 20,
					AfterChars:  10,
					SavedRatio:  0.5,
				},
			},
		}
		originalMessages := []providertypes.Message{{Role: providertypes.RoleUser, Content: "hello"}}
		session := agentsession.Session{
			ID:       "session-save-error",
			Workdir:  t.TempDir(),
			Messages: append([]providertypes.Message(nil), originalMessages...),
		}

		returned, result, err := service.runCompactForSession(context.Background(), "run-3", session, manager.Get(), false)
		if err != nil {
			t.Fatalf("expected non-fatal save error to be swallowed, got %v", err)
		}
		if result.Applied || len(result.Messages) != 0 {
			t.Fatalf("expected zero result after save failure, got %+v", result)
		}
		if len(returned.Messages) != len(originalMessages) || returned.Messages[0].Content != originalMessages[0].Content {
			t.Fatalf("expected original messages restored after save failure, got %+v", returned.Messages)
		}

		events := collectRuntimeEvents(service.Events())
		assertEventSequence(t, events, []EventType{EventCompactStart, EventCompactError})
	})
}

func TestResolveCompactProviderSelectionBranches(t *testing.T) {
	manager := newRuntimeConfigManagerWithProviderEnvs(t, map[string]string{
		"openai": runtimeTestAPIKeyEnv(t),
	})
	cfg := manager.Get()

	sessionResolved, sessionModel, err := resolveCompactProviderSelection(agentsession.Session{
		Provider: cfg.SelectedProvider,
		Model:    "session-model",
	}, cfg)
	if err != nil {
		t.Fatalf("expected session provider selection to resolve, got %v", err)
	}
	if sessionResolved.Name == "" || sessionModel != "session-model" {
		t.Fatalf("expected session provider/model to be preferred, got provider=%+v model=%q", sessionResolved, sessionModel)
	}

	fallbackResolved, fallbackModel, err := resolveCompactProviderSelection(agentsession.Session{}, cfg)
	if err != nil {
		t.Fatalf("expected fallback provider selection to resolve, got %v", err)
	}
	if fallbackResolved.Name == "" || fallbackModel != cfg.CurrentModel {
		t.Fatalf("expected fallback provider/model from config, got provider=%+v model=%q", fallbackResolved, fallbackModel)
	}

	if _, _, err := resolveCompactProviderSelection(agentsession.Session{
		Provider: "missing-provider",
		Model:    "session-model",
	}, cfg); err == nil {
		t.Fatalf("expected invalid session provider to fail")
	}

	badCfg := cfg
	badCfg.SelectedProvider = "missing-provider"
	if _, err := resolveSelectedProviderFromConfig(badCfg); err == nil {
		t.Fatalf("expected invalid selected provider to fail")
	}
}

func TestWorkspaceSwitchHelperBranches(t *testing.T) {
	t.Run("initialize workspace state prefers git root", func(t *testing.T) {
		manager := newRuntimeConfigManager(t)
		repoRoot := t.TempDir()
		subdir := filepath.Join(repoRoot, "pkg", "child")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatalf("mkdir subdir: %v", err)
		}
		if output, err := runGitCommand(t, repoRoot, "init"); err != nil {
			t.Fatalf("git init: %v (%s)", err, output)
		}

		service := NewWithFactory(manager, nil, newMemoryStore(), &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
		service.initializeWorkspaceState(subdir)

		root, workdir := service.currentWorkspaceState()
		if root != filepath.Clean(repoRoot) || workdir != filepath.Clean(subdir) {
			t.Fatalf("expected initialized workspace root=%q workdir=%q, got root=%q workdir=%q", filepath.Clean(repoRoot), filepath.Clean(subdir), root, workdir)
		}
	})

	t.Run("helper functions normalize workspace paths", func(t *testing.T) {
		root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
		if effectiveWorkspaceBase("  ", "/repo") != "/repo" {
			t.Fatalf("expected workspace base fallback to root")
		}
		if !sameWorkspacePath(filepath.Join("a", "..", "b"), filepath.Join(".", "b")) {
			t.Fatalf("expected normalized workspace paths to match")
		}
		if !isFilesystemRoot(root) {
			t.Fatalf("expected %q to be recognized as filesystem root", root)
		}
		if normalizeWorkspacePathKey("   ") != "" {
			t.Fatalf("expected blank workspace key to stay blank")
		}
	})

	t.Run("session store provider falls back and handles nil", func(t *testing.T) {
		service := &Service{}
		if store := service.sessionStoreForWorkspace("workspace"); store != nil {
			t.Fatalf("expected nil store provider to return nil store")
		}

		memory := newMemoryStore()
		service.storeProvider = fixedWorkspaceStoreProvider{store: memory}
		if store := service.sessionStoreForWorkspace("workspace"); store != memory {
			t.Fatalf("expected fixed store provider to return backing store")
		}
	})
}

func TestResolveRequestedWorkspacePathUsesBaseAndRejectsInvalidBase(t *testing.T) {
	base := t.TempDir()
	child := filepath.Join(base, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	resolved, err := resolveRequestedWorkspacePath(base, "child")
	if err != nil {
		t.Fatalf("expected relative workspace path to resolve, got %v", err)
	}
	if resolved != filepath.Clean(child) {
		t.Fatalf("expected resolved workdir %q, got %q", filepath.Clean(child), resolved)
	}

	if _, err := resolveRequestedWorkspacePath(filepath.Join(base, "missing"), "child"); err == nil {
		t.Fatalf("expected invalid base workdir to fail")
	}
}

func TestDetectWorkspaceGitRootRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := detectWorkspaceGitRoot(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context error, got %v", err)
	}
}

func TestRunCompactForSessionPersistsUpdatedTimestamp(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	service := NewWithFactory(manager, nil, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	service.compactRunner = &stubCompactRunner{
		result: contextcompact.Result{
			Applied: true,
			Messages: []providertypes.Message{
				{Role: providertypes.RoleAssistant, Content: "compacted"},
			},
			Metrics: contextcompact.Metrics{
				BeforeChars: 12,
				AfterChars:  6,
				SavedRatio:  0.5,
			},
		},
	}

	session := agentsession.Session{
		ID:        "session-compact-success",
		Workdir:   t.TempDir(),
		UpdatedAt: time.Now().Add(-time.Hour),
		Messages: []providertypes.Message{
			{Role: providertypes.RoleUser, Content: "hello"},
		},
	}

	returned, result, err := service.runCompactForSession(context.Background(), "run-4", session, manager.Get(), true)
	if err != nil {
		t.Fatalf("expected compact success, got %v", err)
	}
	if !result.Applied || len(returned.Messages) != 1 || returned.Messages[0].Content != "compacted" {
		t.Fatalf("expected compacted messages to be persisted, got session=%+v result=%+v", returned, result)
	}
	if !returned.UpdatedAt.After(session.UpdatedAt) {
		t.Fatalf("expected compact to refresh updated timestamp, got before=%v after=%v", session.UpdatedAt, returned.UpdatedAt)
	}

	events := collectRuntimeEvents(service.Events())
	assertEventSequence(t, events, []EventType{EventCompactStart, EventCompactDone})
}
