package tools

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"neo-code/internal/security"
)

// ResolveEffectiveRoot resolves a user-supplied workdir against the configured
// workspace root and ensures the result stays within the workspace boundary.
func ResolveEffectiveRoot(defaultRoot string, workdir string) (string, error) {
	base := strings.TrimSpace(workdir)
	if base == "" {
		return defaultRoot, nil
	}
	_, resolved, err := security.ResolveWorkspacePath(defaultRoot, base)
	return resolved, err
}

type workspaceResolver func(root string, requested string) (string, error)

// ResolveWorkspaceTarget resolves the effective execution target for one tool
// call. When a workspace execution plan exists it is validated and preferred,
// which binds sandbox validation to execution-time use.
func ResolveWorkspaceTarget(
	call ToolCallInput,
	expectedType security.TargetType,
	root string,
	requested string,
	fallback workspaceResolver,
) (resolvedRoot string, target string, err error) {
	if call.WorkspacePlan == nil {
		target, err = fallback(root, requested)
		if err != nil {
			return "", "", err
		}
		return root, target, nil
	}

	plan := call.WorkspacePlan
	if err := plan.ValidateForExecution(); err != nil {
		return "", "", err
	}
	if expectedType != "" && plan.TargetType != expectedType {
		return "", "", fmt.Errorf(
			"tools: workspace plan target type %q does not match expected %q",
			plan.TargetType,
			expectedType,
		)
	}

	expectedTarget, err := fallback(plan.Root, requested)
	if err != nil {
		return "", "", err
	}
	if !sameWorkspacePath(expectedTarget, plan.Target) {
		return "", "", fmt.Errorf(
			"tools: workspace plan target mismatch: expected %q, got %q",
			expectedTarget,
			plan.Target,
		)
	}

	return plan.Root, plan.Target, nil
}

func sameWorkspacePath(a string, b string) bool {
	cleanA := filepath.Clean(a)
	cleanB := filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(cleanA, cleanB)
	}
	return cleanA == cleanB
}
