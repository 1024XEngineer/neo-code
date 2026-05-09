package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	agentsession "neo-code/internal/session"
)

func TestResolveGatewayDefaultWorkspaceRootPrefersRequestedWorkdir(t *testing.T) {
	requestedDir := t.TempDir()
	configDir := t.TempDir()

	resolved, err := resolveGatewayDefaultWorkspaceRoot(requestedDir, configDir)
	if err != nil {
		t.Fatalf("resolveGatewayDefaultWorkspaceRoot() error = %v", err)
	}

	expected, err := filepath.Abs(requestedDir)
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	if resolved != filepath.Clean(expected) {
		t.Fatalf("resolveGatewayDefaultWorkspaceRoot() = %q, want %q", resolved, filepath.Clean(expected))
	}
}

func TestResolveGatewayDefaultWorkspaceRootFallsBackToConfigWorkdir(t *testing.T) {
	configDir := t.TempDir()

	resolved, err := resolveGatewayDefaultWorkspaceRoot("   ", configDir)
	if err != nil {
		t.Fatalf("resolveGatewayDefaultWorkspaceRoot() error = %v", err)
	}

	expected, err := filepath.Abs(configDir)
	if err != nil {
		t.Fatalf("filepath.Abs() error = %v", err)
	}
	if resolved != filepath.Clean(expected) {
		t.Fatalf("resolveGatewayDefaultWorkspaceRoot() = %q, want %q", resolved, filepath.Clean(expected))
	}
}

func TestResolveGatewayDefaultWorkspaceRootRejectsEmptyCandidate(t *testing.T) {
	if _, err := resolveGatewayDefaultWorkspaceRoot("", ""); err == nil {
		t.Fatalf("resolveGatewayDefaultWorkspaceRoot() error = nil, want non-nil")
	}
}

func TestResolveGatewayDefaultWorkspaceRootRejectsMissingDirectory(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "missing")
	if _, err := resolveGatewayDefaultWorkspaceRoot(missingPath, ""); err == nil {
		t.Fatalf("resolveGatewayDefaultWorkspaceRoot() error = nil, want non-nil")
	}
}

func TestSanitizeWorkspaceIndexRemovesInvalidAndTemporaryWorkspaces(t *testing.T) {
	baseDir := t.TempDir()
	index := agentsession.NewWorkspaceIndex(baseDir)

	defaultWorkspace := filepath.Join(baseDir, "repo")
	realWorkspace := filepath.Join(baseDir, "real-build")
	for _, dir := range []string{defaultWorkspace, realWorkspace} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
	}

	if _, err := index.Register(realWorkspace, "real-build"); err != nil {
		t.Fatalf("register real workspace: %v", err)
	}
	tempWorkspace := filepath.Join(os.TempDir(), "neocode-web-release-check", "build")
	if err := os.MkdirAll(tempWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir temp workspace: %v", err)
	}
	if _, err := index.Register(tempWorkspace, "build"); err != nil {
		t.Fatalf("register temp workspace: %v", err)
	}
	missingWorkspace := filepath.Join(baseDir, "missing")
	missingRecord := agentsession.WorkspaceRecord{
		Hash: agentsession.HashWorkspaceRoot(missingWorkspace),
		Path: agentsession.NormalizeWorkspaceRoot(missingWorkspace),
		Name: "missing",
	}
	indexRecords := append(index.List(), missingRecord)
	writeWorkspaceIndexRecords(t, baseDir, indexRecords)
	if err := index.Load(); err != nil {
		t.Fatalf("reload index: %v", err)
	}

	if err := sanitizeWorkspaceIndex(index, defaultWorkspace); err != nil {
		t.Fatalf("sanitizeWorkspaceIndex() error = %v", err)
	}

	records := index.List()
	if len(records) != 2 {
		t.Fatalf("records len = %d, want 2", len(records))
	}
	assertWorkspacePresent(t, records, defaultWorkspace)
	assertWorkspacePresent(t, records, realWorkspace)
	assertWorkspaceMissing(t, records, tempWorkspace)
	assertWorkspaceMissing(t, records, missingWorkspace)

	reloaded := agentsession.NewWorkspaceIndex(baseDir)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("load persisted index: %v", err)
	}
	persisted := reloaded.List()
	if len(persisted) != 2 {
		t.Fatalf("persisted records len = %d, want 2", len(persisted))
	}
	assertWorkspacePresent(t, persisted, defaultWorkspace)
	assertWorkspacePresent(t, persisted, realWorkspace)
}

func TestSanitizeWorkspaceIndexDoesNotRegisterTemporaryDefaultWorkspace(t *testing.T) {
	baseDir := t.TempDir()
	index := agentsession.NewWorkspaceIndex(baseDir)
	defaultWorkspace := filepath.Join(os.TempDir(), "neocode-web-release-check", "build")
	if err := os.MkdirAll(defaultWorkspace, 0o755); err != nil {
		t.Fatalf("mkdir temp workspace: %v", err)
	}

	if err := sanitizeWorkspaceIndex(index, defaultWorkspace); err != nil {
		t.Fatalf("sanitizeWorkspaceIndex() error = %v", err)
	}
	if records := index.List(); len(records) != 0 {
		t.Fatalf("records = %+v, want empty index", records)
	}
}

func writeWorkspaceIndexRecords(t *testing.T, baseDir string, records []agentsession.WorkspaceRecord) {
	t.Helper()
	indexPath := filepath.Join(baseDir, "workspaces.json")
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		t.Fatalf("marshal workspace records: %v", err)
	}
	if err := os.WriteFile(indexPath, data, 0o644); err != nil {
		t.Fatalf("write workspace index: %v", err)
	}
}

func assertWorkspacePresent(t *testing.T, records []agentsession.WorkspaceRecord, workspaceRoot string) {
	t.Helper()
	target := agentsession.WorkspacePathKey(workspaceRoot)
	for _, record := range records {
		if agentsession.WorkspacePathKey(record.Path) == target {
			return
		}
	}
	t.Fatalf("workspace %q not found in %+v", workspaceRoot, records)
}

func assertWorkspaceMissing(t *testing.T, records []agentsession.WorkspaceRecord, workspaceRoot string) {
	t.Helper()
	target := agentsession.WorkspacePathKey(workspaceRoot)
	for _, record := range records {
		if agentsession.WorkspacePathKey(record.Path) == target {
			t.Fatalf("workspace %q should be absent, records=%+v", workspaceRoot, records)
		}
	}
}
