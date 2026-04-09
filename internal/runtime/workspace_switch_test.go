package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/config"
	agentsession "neo-code/internal/session"
	"neo-code/internal/tools"
)

func TestServiceSwitchWorkspaceScopesSessionsByWorkspace(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = workspaceA
		return nil
	}); err != nil {
		t.Fatalf("update workdir: %v", err)
	}

	router := agentsession.NewScopedStoreRouter(t.TempDir(), workspaceA)
	storeA := router.StoreForWorkspace(workspaceA)
	storeB := router.StoreForWorkspace(workspaceB)

	sessionA := agentsession.NewWithWorkdir("workspace-a", workspaceA)
	sessionB := agentsession.NewWithWorkdir("workspace-b", workspaceB)
	if err := storeA.Save(context.Background(), &sessionA); err != nil {
		t.Fatalf("save workspace A session: %v", err)
	}
	if err := storeB.Save(context.Background(), &sessionB); err != nil {
		t.Fatalf("save workspace B session: %v", err)
	}

	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "filesystem_read_file", content: "default"})
	service := NewWithFactory(manager, registry, router, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)

	summaries, err := service.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != sessionA.ID {
		t.Fatalf("expected workspace A summaries, got %+v", summaries)
	}

	result, err := service.SwitchWorkspace(context.Background(), WorkspaceSwitchInput{RequestedPath: workspaceB})
	if err != nil {
		t.Fatalf("SwitchWorkspace() error = %v", err)
	}
	if !result.WorkspaceChanged || !result.ResetToDraft {
		t.Fatalf("expected cross-workspace switch to reset draft, got %+v", result)
	}
	if result.WorkspaceRoot != filepath.Clean(workspaceB) {
		t.Fatalf("expected workspace root %q, got %q", filepath.Clean(workspaceB), result.WorkspaceRoot)
	}

	summaries, err = service.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions() after switch error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != sessionB.ID {
		t.Fatalf("expected workspace B summaries, got %+v", summaries)
	}

	if _, err := service.LoadSession(context.Background(), sessionA.ID); err == nil {
		t.Fatalf("expected previous workspace session to be hidden after switch")
	}
}

func TestServiceSwitchWorkspaceDraftDoesNotCreateSession(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = workspaceA
		return nil
	}); err != nil {
		t.Fatalf("update workdir: %v", err)
	}

	router := agentsession.NewScopedStoreRouter(t.TempDir(), workspaceA)
	service := NewWithFactory(manager, tools.NewRegistry(), router, &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)

	result, err := service.SwitchWorkspace(context.Background(), WorkspaceSwitchInput{RequestedPath: workspaceB})
	if err != nil {
		t.Fatalf("SwitchWorkspace() error = %v", err)
	}
	if !result.ResetToDraft {
		t.Fatalf("expected draft workspace switch to stay in draft, got %+v", result)
	}

	summaries, err := service.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected no sessions created during draft workspace switch, got %+v", summaries)
	}
}

func TestServiceSwitchWorkspacePrefersGitRoot(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	workspaceA := t.TempDir()
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = workspaceA
		return nil
	}); err != nil {
		t.Fatalf("update workdir: %v", err)
	}

	repoRoot := t.TempDir()
	subdir := filepath.Join(repoRoot, "pkg", "child")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if output, err := runGitCommand(t, repoRoot, "init"); err != nil {
		t.Fatalf("git init: %v (%s)", err, output)
	}

	service := NewWithFactory(manager, tools.NewRegistry(), agentsession.NewScopedStoreRouter(t.TempDir(), workspaceA), &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	result, err := service.SwitchWorkspace(context.Background(), WorkspaceSwitchInput{RequestedPath: subdir})
	if err != nil {
		t.Fatalf("SwitchWorkspace() error = %v", err)
	}
	if result.WorkspaceRoot != filepath.Clean(repoRoot) {
		t.Fatalf("expected git root %q, got %q", filepath.Clean(repoRoot), result.WorkspaceRoot)
	}
	if result.Workdir != filepath.Clean(subdir) {
		t.Fatalf("expected workdir %q, got %q", filepath.Clean(subdir), result.Workdir)
	}
}

func TestServiceSwitchWorkspaceRejectsCrossWorkspaceWhileBusy(t *testing.T) {
	manager := newRuntimeConfigManager(t)
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.Workdir = workspaceA
		return nil
	}); err != nil {
		t.Fatalf("update workdir: %v", err)
	}

	service := NewWithFactory(manager, tools.NewRegistry(), agentsession.NewScopedStoreRouter(t.TempDir(), workspaceA), &scriptedProviderFactory{provider: &scriptedProvider{}}, nil)
	service.setOperationState("run")
	defer service.clearOperationState()

	_, err := service.SwitchWorkspace(context.Background(), WorkspaceSwitchInput{RequestedPath: workspaceB})
	if err == nil || !strings.Contains(err.Error(), "cannot switch workspace while run is running") {
		t.Fatalf("expected busy switch rejection, got %v", err)
	}
}

func runGitCommand(t *testing.T, workdir string, args ...string) (string, error) {
	t.Helper()

	command := exec.Command("git", append([]string{"-C", workdir}, args...)...)
	output, err := command.CombinedOutput()
	return string(output), err
}
