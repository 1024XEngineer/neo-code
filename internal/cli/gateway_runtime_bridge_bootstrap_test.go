package cli

import (
	"path/filepath"
	"testing"
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
