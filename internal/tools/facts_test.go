package tools

import (
	"testing"

	"neo-code/internal/security"
)

func TestEnrichToolResultFactsDefaultsFromAction(t *testing.T) {
	t.Parallel()

	read := EnrichToolResultFacts(security.Action{Type: security.ActionTypeRead}, ToolResult{})
	if read.Facts.WorkspaceWrite {
		t.Fatalf("expected read action to default workspace_write=false")
	}

	bash := EnrichToolResultFacts(security.Action{Type: security.ActionTypeBash}, ToolResult{})
	if bash.Facts.WorkspaceWrite {
		t.Fatalf("expected bash action to default workspace_write=false")
	}

	mcp := EnrichToolResultFacts(security.Action{Type: security.ActionTypeMCP}, ToolResult{})
	if mcp.Facts.WorkspaceWrite {
		t.Fatalf("expected mcp action to default workspace_write=false")
	}
}

func TestEnrichToolResultFactsIgnoresUntrustedMetadata(t *testing.T) {
	t.Parallel()

	result := EnrichToolResultFacts(
		security.Action{Type: security.ActionTypeMCP},
		ToolResult{
			Metadata: map[string]any{
				"workspace_write":        false,
				"verification_performed": true,
				"verification_passed":    true,
				"verification_scope":     "workspace",
			},
		},
	)
	if result.Facts.WorkspaceWrite {
		t.Fatalf("expected metadata workspace_write to be ignored")
	}
	if result.Facts.VerificationPerformed || result.Facts.VerificationPassed {
		t.Fatalf("expected metadata verification facts to be ignored, got %+v", result.Facts)
	}
	if result.Facts.VerificationScope != "" {
		t.Fatalf("expected empty verification scope, got %q", result.Facts.VerificationScope)
	}
}

func TestEnrichToolResultFactsRespectsTrustedFacts(t *testing.T) {
	t.Parallel()

	result := EnrichToolResultFacts(
		security.Action{Type: security.ActionTypeBash},
		ToolResult{
			Facts: ToolExecutionFacts{
				WorkspaceWrite:        true,
				VerificationPerformed: true,
				VerificationPassed:    true,
				VerificationScope:     " workspace ",
			},
		},
	)
	if !result.Facts.WorkspaceWrite {
		t.Fatalf("expected trusted workspace write fact to be preserved")
	}
	if !result.Facts.VerificationPerformed || !result.Facts.VerificationPassed {
		t.Fatalf("expected trusted verification facts to be preserved, got %+v", result.Facts)
	}
	if result.Facts.VerificationScope != "workspace" {
		t.Fatalf("verification scope = %q, want workspace", result.Facts.VerificationScope)
	}
}
