package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionWorkdirKeyAndMemoryMap(t *testing.T) {
	t.Parallel()

	serviceA := &Service{}
	serviceB := &Service{}

	keyA := serviceA.sessionWorkdirKey("session-1")
	keyB := serviceB.sessionWorkdirKey("session-1")
	if keyA == keyB {
		t.Fatalf("expected unique key per service instance, got %q", keyA)
	}

	if got := serviceA.sessionWorkdir("session-1", "/fallback"); got != "/fallback" {
		t.Fatalf("expected fallback workdir, got %q", got)
	}

	target := t.TempDir()
	serviceA.setSessionWorkdir("session-1", target)
	if got := serviceA.sessionWorkdir("session-1", "/fallback"); got != target {
		t.Fatalf("expected mapped workdir %q, got %q", target, got)
	}
}

func TestResolveWorkdirForSessionAndNormalizeErrors(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	current := t.TempDir()
	relativeDir := filepath.Join(current, "child")
	if err := os.MkdirAll(relativeDir, 0o755); err != nil {
		t.Fatalf("mkdir relative dir: %v", err)
	}
	absoluteDir := t.TempDir()

	got, err := resolveWorkdirForSession(base, "", "")
	if err != nil || got != filepath.Clean(base) {
		t.Fatalf("expected base workdir %q, got %q / %v", filepath.Clean(base), got, err)
	}

	got, err = resolveWorkdirForSession(base, current, "child")
	if err != nil || got != filepath.Clean(relativeDir) {
		t.Fatalf("expected relative workdir %q, got %q / %v", filepath.Clean(relativeDir), got, err)
	}

	got, err = resolveWorkdirForSession(base, current, absoluteDir)
	if err != nil || got != filepath.Clean(absoluteDir) {
		t.Fatalf("expected absolute workdir %q, got %q / %v", filepath.Clean(absoluteDir), got, err)
	}

	_, err = resolveWorkdirForSession("", "", "")
	if err == nil || !strings.Contains(err.Error(), "workdir is empty") {
		t.Fatalf("expected empty workdir error, got %v", err)
	}

	_, err = normalizeExistingWorkdir(filepath.Join(base, "missing"))
	if err == nil || !strings.Contains(err.Error(), "resolve workdir") {
		t.Fatalf("expected missing path error, got %v", err)
	}

	filePath := filepath.Join(base, "note.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err = normalizeExistingWorkdir(filePath)
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("expected non-directory error, got %v", err)
	}
}

func TestLoadSessionUsesFallbackWorkdirWhenMemoryMissing(t *testing.T) {
	t.Parallel()

	manager := newRuntimeConfigManager(t)
	store := newMemoryStore()
	session := newSession("fallback")
	session.Workdir = t.TempDir()
	store.sessions[session.ID] = cloneSession(session)

	service := NewWithFactory(manager, nil, store, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	loaded, err := service.LoadSession(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.Workdir != session.Workdir {
		t.Fatalf("expected fallback to persisted workdir %q, got %q", session.Workdir, loaded.Workdir)
	}
}
