package runner

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"neo-code/internal/security"
)

func TestCapSignerVerifyToolRequest(t *testing.T) {
	t.Run("allows request without token or allowlist", func(t *testing.T) {
		signer := NewCapSigner(nil)
		if err := signer.VerifyToolRequest(ToolExecutionRequest{ToolName: "bash"}, "/tmp/work"); err != nil {
			t.Fatalf("VerifyToolRequest() error = %v", err)
		}
	})

	t.Run("rejects invalid signature", func(t *testing.T) {
		verifier, err := security.NewCapabilitySigner([]byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatalf("NewCapabilitySigner() error = %v", err)
		}
		signer := NewCapSigner(nil)
		signer.SetCapVerifier(verifier)
		token := validCapabilityToken(t, "bash")
		if err := signer.VerifyToolRequest(ToolExecutionRequest{
			ToolName:        "bash",
			CapabilityToken: &token,
		}, "/tmp/work"); !errors.Is(err, ErrCapabilitySignatureInvalid) {
			t.Fatalf("VerifyToolRequest() error = %v, want %v", err, ErrCapabilitySignatureInvalid)
		}
	})

	t.Run("rejects expired token", func(t *testing.T) {
		signer := NewCapSigner(nil)
		token := validCapabilityToken(t, "bash")
		token.ExpiresAt = time.Now().UTC().Add(-time.Second)
		if err := signer.VerifyToolRequest(ToolExecutionRequest{
			ToolName:        "bash",
			CapabilityToken: &token,
		}, "/tmp/work"); !errors.Is(err, ErrCapabilityTokenExpired) {
			t.Fatalf("VerifyToolRequest() error = %v, want %v", err, ErrCapabilityTokenExpired)
		}
	})

	t.Run("rejects disallowed tool", func(t *testing.T) {
		signer := NewCapSigner(nil)
		token := validCapabilityToken(t, "read_file")
		if err := signer.VerifyToolRequest(ToolExecutionRequest{
			ToolName:        "bash",
			CapabilityToken: &token,
		}, "/tmp/work"); !errors.Is(err, ErrCapabilityToolNotAllowed) {
			t.Fatalf("VerifyToolRequest() error = %v, want %v", err, ErrCapabilityToolNotAllowed)
		}
	})

	t.Run("rejects arguments outside allowlist", func(t *testing.T) {
		signer := NewCapSigner([]string{"/safe"})
		args, err := json.Marshal(map[string]any{"path": "../../outside.txt"})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		if err := signer.VerifyToolRequest(ToolExecutionRequest{
			ToolName:  "read_file",
			Arguments: args,
		}, "/safe/work"); !errors.Is(err, ErrCapabilityPathNotAllowed) {
			t.Fatalf("VerifyToolRequest() error = %v, want %v", err, ErrCapabilityPathNotAllowed)
		}
	})
}

func TestCapSignerVerifyPathsInArgsIgnoresUnsupportedValues(t *testing.T) {
	signer := NewCapSigner([]string{"/safe"})
	if err := signer.verifyPathsInArgs(json.RawMessage(`{`), "/safe/work"); err != nil {
		t.Fatalf("verifyPathsInArgs() error = %v", err)
	}

	args, err := json.Marshal(map[string]any{
		"url":   "https://example.com/file",
		"count": 2,
		"path":  "./nested/file.txt",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := signer.verifyPathsInArgs(args, "/safe/work"); err != nil {
		t.Fatalf("verifyPathsInArgs() error = %v", err)
	}
}

func TestCapSignerHelpers(t *testing.T) {
	if !looksLikePath("./a.txt") {
		t.Fatal("looksLikePath() = false, want true for relative path")
	}
	if looksLikePath("https://example.com/file") {
		t.Fatal("looksLikePath() = true, want false for URL")
	}

	resolved := resolvePath("child/file.txt", "/tmp/work")
	wantResolved := filepath.Join("/tmp/work", "child/file.txt")
	if resolved != wantResolved {
		t.Fatalf("resolvePath() = %q, want %q", resolved, wantResolved)
	}
	if got := resolvePath("   ", "/tmp/work"); got != "" {
		t.Fatalf("resolvePath(empty) = %q, want empty", got)
	}
	if got := resolvePath("/tmp/abs.txt", "/tmp/work"); got != "/tmp/abs.txt" {
		t.Fatalf("resolvePath(abs) = %q", got)
	}
	if got := resolvePath("child.txt", ""); got != "child.txt" {
		t.Fatalf("resolvePath(no workdir) = %q", got)
	}

	if !isToolAllowed([]string{" Bash "}, "bash") {
		t.Fatal("isToolAllowed() = false, want true")
	}
	if isToolAllowed([]string{"read_file"}, "bash") {
		t.Fatal("isToolAllowed() = true, want false")
	}
}

func TestCapSignerVerifyPath(t *testing.T) {
	t.Run("allowlist disabled", func(t *testing.T) {
		signer := NewCapSigner(nil)
		if err := signer.VerifyPath("/any/path"); err != nil {
			t.Fatalf("VerifyPath() error = %v", err)
		}
	})

	t.Run("exact and child path allowed", func(t *testing.T) {
		signer := NewCapSigner([]string{"/safe/base", "   "})
		if err := signer.VerifyPath("/safe/base"); err != nil {
			t.Fatalf("VerifyPath(exact) error = %v", err)
		}
		if err := signer.VerifyPath("/safe/base/child.txt"); err != nil {
			t.Fatalf("VerifyPath(child) error = %v", err)
		}
	})

	t.Run("outside path rejected", func(t *testing.T) {
		signer := NewCapSigner([]string{"/safe/base"})
		if err := signer.VerifyPath("/unsafe/base"); !errors.Is(err, ErrCapabilityPathNotAllowed) {
			t.Fatalf("VerifyPath() error = %v, want %v", err, ErrCapabilityPathNotAllowed)
		}
	})

	t.Run("blank allowlist entry is ignored", func(t *testing.T) {
		signer := NewCapSigner([]string{"   ", "/safe/base"})
		if err := signer.VerifyPath("/safe/base/file.txt"); err != nil {
			t.Fatalf("VerifyPath() error = %v", err)
		}
	})

	if got := normalizePath("  "); got != "" {
		t.Fatalf("normalizePath(empty) = %q", got)
	}
	if got := normalizePath("/safe/base/../child"); got != "/safe/child" {
		t.Fatalf("normalizePath(clean) = %q", got)
	}
}

func validCapabilityToken(t *testing.T, toolName string) security.CapabilityToken {
	t.Helper()
	now := time.Now().UTC()
	return security.CapabilityToken{
		ID:              "cap-1",
		TaskID:          "run-1",
		AgentID:         "session-1",
		IssuedAt:        now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Minute),
		AllowedTools:    []string{toolName},
		AllowedPaths:    []string{"/safe"},
		NetworkPolicy:   security.NetworkPolicy{Mode: security.NetworkPermissionDenyAll},
		WritePermission: security.WritePermissionWorkspace,
	}
}
