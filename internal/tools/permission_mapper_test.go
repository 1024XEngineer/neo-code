package tools

import (
	"testing"

	"neo-code/internal/security"
)

func TestBuildPermissionActionRejectsEmptyToolName(t *testing.T) {
	if _, err := buildPermissionAction(ToolCallInput{}); err == nil {
		t.Fatal("expected error for empty tool name")
	}
}

func TestBuildPermissionActionBashGitDefaultsSandboxAndSemanticFields(t *testing.T) {
	action, err := buildPermissionAction(ToolCallInput{
		Name:      ToolNameBash,
		SessionID: "session-1",
		TaskID:    "task-1",
		AgentID:   "agent-1",
		Arguments: []byte(`{"command":"git status --short"}`),
	})
	if err != nil {
		t.Fatalf("buildPermissionAction() error = %v", err)
	}

	if action.Type != security.ActionTypeBash {
		t.Fatalf("action.Type = %q, want %q", action.Type, security.ActionTypeBash)
	}
	if action.Payload.Operation != "git_status" {
		t.Fatalf("operation = %q, want git_status", action.Payload.Operation)
	}
	if action.Payload.TargetType != security.TargetTypeCommand {
		t.Fatalf("target type = %q, want %q", action.Payload.TargetType, security.TargetTypeCommand)
	}
	if action.Payload.Target != "git status --short" {
		t.Fatalf("target = %q", action.Payload.Target)
	}
	if action.Payload.Resource == "" {
		t.Fatal("expected git resource to be set")
	}
	if action.Payload.SemanticType != "git" {
		t.Fatalf("semantic type = %q, want git", action.Payload.SemanticType)
	}
	if action.Payload.PermissionFingerprint == "" {
		t.Fatal("expected permission fingerprint to be populated")
	}
	if action.Payload.SandboxTargetType != security.TargetTypeDirectory {
		t.Fatalf("sandbox target type = %q, want %q", action.Payload.SandboxTargetType, security.TargetTypeDirectory)
	}
	if action.Payload.SandboxTarget != "." {
		t.Fatalf("sandbox target = %q, want .", action.Payload.SandboxTarget)
	}
}

func TestBuildPermissionActionReadFileFallsBackForWindowsPathLikePayload(t *testing.T) {
	action, err := buildPermissionAction(ToolCallInput{
		Name:      ToolNameFilesystemReadFile,
		Arguments: []byte(`{"path":"C:\repo\main.go"}`),
	})
	if err != nil {
		t.Fatalf("buildPermissionAction() error = %v", err)
	}

	if action.Type != security.ActionTypeRead {
		t.Fatalf("action.Type = %q, want %q", action.Type, security.ActionTypeRead)
	}
	if action.Payload.Target != `C:\repo\main.go` {
		t.Fatalf("target = %q", action.Payload.Target)
	}
	if action.Payload.SandboxTarget != `C:\repo\main.go` {
		t.Fatalf("sandbox target = %q", action.Payload.SandboxTarget)
	}
}

func TestBuildPermissionActionSpawnSubAgentUsesAllowedPathAndStableTarget(t *testing.T) {
	action, err := buildPermissionAction(ToolCallInput{
		Name:    ToolNameSpawnSubAgent,
		Workdir: "/workspace",
		Arguments: []byte(`{
			"id":"fallback",
			"items":[{"id":"task-a"},{"id":"task-b"}],
			"allowed_paths":["/workspace/pkg"]
		}`),
	})
	if err != nil {
		t.Fatalf("buildPermissionAction() error = %v", err)
	}

	if action.Type != security.ActionTypeWrite {
		t.Fatalf("action.Type = %q, want %q", action.Type, security.ActionTypeWrite)
	}
	if action.Payload.Target != "task-a,task-b" {
		t.Fatalf("target = %q, want task-a,task-b", action.Payload.Target)
	}
	if action.Payload.SandboxTarget != "/workspace/pkg" {
		t.Fatalf("sandbox target = %q, want /workspace/pkg", action.Payload.SandboxTarget)
	}
}

func TestBuildPermissionActionSupportsMCPIdentity(t *testing.T) {
	action, err := buildPermissionAction(ToolCallInput{
		Name: "MCP.GitHub.Create_Issue",
	})
	if err != nil {
		t.Fatalf("buildPermissionAction() error = %v", err)
	}

	if action.Type != security.ActionTypeMCP {
		t.Fatalf("action.Type = %q, want %q", action.Type, security.ActionTypeMCP)
	}
	if action.Payload.Target != "mcp.github.create_issue" {
		t.Fatalf("target = %q, want mcp.github.create_issue", action.Payload.Target)
	}
	if got := mcpServerTarget("MCP.GitHub.Create_Issue"); got != "mcp.github" {
		t.Fatalf("mcpServerTarget() = %q, want mcp.github", got)
	}
}
