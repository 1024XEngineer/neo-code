package security

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorkspaceSandbox enforces workspace-relative path boundaries for tool actions.
type WorkspaceSandbox struct{}

// NewWorkspaceSandbox creates a sandbox that blocks traversal and symlink escape.
func NewWorkspaceSandbox() *WorkspaceSandbox {
	return &WorkspaceSandbox{}
}

// Check validates that the action stays within the configured workspace root.
func (s *WorkspaceSandbox) Check(ctx context.Context, action Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := action.Validate(); err != nil {
		return err
	}

	plan, ok, err := buildWorkspacePlan(action)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	return validateWorkspacePlan(plan)
}

type workspacePlan struct {
	root   string
	target string
}

func buildWorkspacePlan(action Action) (workspacePlan, bool, error) {
	if !needsWorkspaceSandbox(action) {
		return workspacePlan{}, false, nil
	}

	root := strings.TrimSpace(action.Payload.Workdir)
	if root == "" {
		return workspacePlan{}, false, errors.New("security: workspace root is empty")
	}

	target, ok := sandboxTarget(action)
	if !ok {
		return workspacePlan{}, false, nil
	}

	return workspacePlan{
		root:   root,
		target: target,
	}, true, nil
}

func needsWorkspaceSandbox(action Action) bool {
	switch action.Type {
	case ActionTypeRead, ActionTypeWrite, ActionTypeBash:
		return true
	default:
		return false
	}
}

func sandboxTarget(action Action) (string, bool) {
	if action.Type == ActionTypeBash {
		target := strings.TrimSpace(action.Payload.SandboxTarget)
		if target == "" {
			return ".", true
		}
		return target, true
	}

	targetType := action.Payload.SandboxTargetType
	if targetType == "" {
		targetType = action.Payload.TargetType
	}

	target := strings.TrimSpace(action.Payload.SandboxTarget)
	if target == "" {
		target = strings.TrimSpace(action.Payload.Target)
	}

	switch targetType {
	case TargetTypeDirectory:
		if target == "" {
			return ".", true
		}
		return target, true
	case TargetTypePath:
		if target == "" {
			return "", false
		}
		return target, true
	default:
		return "", false
	}
}

func validateWorkspacePlan(plan workspacePlan) error {
	root, err := canonicalWorkspaceRoot(plan.root)
	if err != nil {
		return err
	}

	target, err := absoluteWorkspaceTarget(root, plan.target)
	if err != nil {
		return err
	}
	if !isWithinWorkspace(root, target) {
		return fmt.Errorf("security: path %q escapes workspace root", plan.target)
	}

	return ensureNoSymlinkEscape(root, target, plan.target)
}

func canonicalWorkspaceRoot(root string) (string, error) {
	absoluteRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return "", fmt.Errorf("security: resolve workspace root: %w", err)
	}

	canonicalRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return filepath.Clean(absoluteRoot), nil
		}
		return "", fmt.Errorf("security: resolve workspace root: %w", err)
	}

	return filepath.Clean(canonicalRoot), nil
}

func absoluteWorkspaceTarget(root string, target string) (string, error) {
	trimmedTarget := strings.TrimSpace(target)
	if trimmedTarget == "" {
		trimmedTarget = "."
	}
	if !filepath.IsAbs(trimmedTarget) {
		trimmedTarget = filepath.Join(root, trimmedTarget)
	}

	absoluteTarget, err := filepath.Abs(trimmedTarget)
	if err != nil {
		return "", fmt.Errorf("security: resolve workspace target %q: %w", target, err)
	}

	return filepath.Clean(absoluteTarget), nil
}

func ensureNoSymlinkEscape(root string, target string, original string) error {
	relativeTarget, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("security: compare workspace target %q: %w", original, err)
	}

	cleanRelative := filepath.Clean(relativeTarget)
	if cleanRelative == "." {
		return nil
	}

	current := root
	for _, segment := range splitRelativePath(cleanRelative) {
		next := filepath.Join(current, segment)
		info, err := os.Lstat(next)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("security: inspect path %q: %w", next, err)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(next)
			if err != nil {
				return fmt.Errorf("security: resolve symlink %q: %w", next, err)
			}
			resolved, err = filepath.Abs(resolved)
			if err != nil {
				return fmt.Errorf("security: resolve symlink %q: %w", next, err)
			}
			if !isWithinWorkspace(root, resolved) {
				return fmt.Errorf("security: path %q escapes workspace root via symlink", original)
			}
			current = filepath.Clean(resolved)
			continue
		}

		current = next
	}

	return nil
}

func splitRelativePath(path string) []string {
	cleanPath := filepath.Clean(path)
	if cleanPath == "." {
		return nil
	}
	return strings.Split(cleanPath, string(os.PathSeparator))
}

func isWithinWorkspace(root string, target string) bool {
	relativePath, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return relativePath == "." ||
		(relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)))
}
