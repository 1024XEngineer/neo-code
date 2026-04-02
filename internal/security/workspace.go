package security

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// WorkspaceSandbox enforces workspace-relative path boundaries for tool actions.
type WorkspaceSandbox struct {
	canonicalRoots sync.Map
}

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

	return s.validateWorkspacePlan(plan)
}

type workspacePlan struct {
	root   string
	target string
}

// buildWorkspacePlan extracts the workspace root and the sandbox-specific target
// that should be validated. Actions that do not touch local workspace paths are
// ignored here and continue through the permission pipeline unchanged.
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

// sandboxTarget returns the path-like value that should be checked against the
// workspace boundary. This is intentionally separate from ActionPayload.Target,
// because some tools expose one value to policy while validating another one for
// local filesystem access.
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

// validateWorkspacePlan resolves the canonical workspace root, expands the
// requested target to an absolute path, verifies it stays inside the workspace,
// and then checks the nearest existing path ancestor for symlink escape.
func (s *WorkspaceSandbox) validateWorkspacePlan(plan workspacePlan) error {
	root, err := s.canonicalWorkspaceRoot(plan.root)
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

// canonicalWorkspaceRoot resolves the configured workspace root to a canonical
// directory path and caches non-symlink workspace roots for repeated tool calls.
func (s *WorkspaceSandbox) canonicalWorkspaceRoot(root string) (string, error) {
	absoluteRoot, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return "", fmt.Errorf("security: resolve workspace root: %w", err)
	}
	cacheKey := cleanedPathKey(absoluteRoot)
	if cached, ok := s.canonicalRoots.Load(cacheKey); ok {
		return cached.(string), nil
	}

	canonicalRoot, cacheable, err := resolveCanonicalWorkspaceRoot(cacheKey)
	if err != nil {
		return "", err
	}
	if cacheable {
		s.canonicalRoots.Store(cacheKey, canonicalRoot)
	}
	return canonicalRoot, nil
}

// resolveCanonicalWorkspaceRoot resolves the configured workspace root to a
// canonical directory path. The root must already exist so validation cannot
// silently rely on a string-only path that may later resolve through a
// symlinked parent.
func resolveCanonicalWorkspaceRoot(absoluteRoot string) (string, bool, error) {
	info, err := os.Stat(absoluteRoot)
	if err != nil {
		return "", false, fmt.Errorf("security: resolve workspace root: %w", err)
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("security: workspace root %q is not a directory", absoluteRoot)
	}

	canonicalRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return "", false, fmt.Errorf("security: resolve workspace root: %w", err)
	}

	cleanedCanonical := cleanedPathKey(canonicalRoot)
	return cleanedCanonical, samePathKey(absoluteRoot, cleanedCanonical), nil
}

// absoluteWorkspaceTarget expands a workspace-relative target into an absolute
// path and rejects Windows volume changes up front so later boundary checks do
// not depend on platform-specific filepath.Rel behavior.
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

	if err := validateTargetVolume(root, absoluteTarget); err != nil {
		return "", err
	}

	return filepath.Clean(absoluteTarget), nil
}

// ensureNoSymlinkEscape resolves the nearest existing path on the way to target
// and ensures that any symlink in the existing prefix still resolves inside the
// workspace.
//
// This blocks common symlink escape attempts such as placing a link inside the
// workspace that points to a file or directory outside the workspace tree while
// avoiding a path-by-path stat on every component.
//
// Known limitation: this validation is still subject to TOCTOU races between the
// sandbox check and the later filesystem operation in the executor. Eliminating
// that window requires executor-level changes such as descriptor-based access or
// no-follow file opening, which are outside this lightweight sandbox layer.
func ensureNoSymlinkEscape(root string, target string, original string) error {
	existingPath, err := nearestExistingPath(root, target)
	if err != nil {
		return err
	}
	if existingPath == root {
		return nil
	}

	resolved, err := filepath.EvalSymlinks(existingPath)
	if err != nil {
		return fmt.Errorf("security: resolve symlink %q: %w", existingPath, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("security: resolve symlink %q: %w", existingPath, err)
	}
	if !isWithinWorkspace(root, resolved) {
		return fmt.Errorf("security: path %q escapes workspace root via symlink", original)
	}
	return nil
}

func validateTargetVolume(root string, target string) error {
	rootVolume := normalizeVolumeName(root)
	targetVolume := normalizeVolumeName(target)
	if rootVolume == "" || targetVolume == "" {
		return nil
	}
	if !strings.EqualFold(rootVolume, targetVolume) {
		return fmt.Errorf("security: path %q is on different volume than workspace root", target)
	}
	return nil
}

func normalizeVolumeName(path string) string {
	volume := strings.TrimSpace(filepath.VolumeName(cleanedPathKey(path)))
	volume = strings.TrimPrefix(volume, `\\?\`)
	return strings.ToLower(volume)
}

func nearestExistingPath(root string, target string) (string, error) {
	current := cleanedPathKey(target)
	root = cleanedPathKey(root)
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || current != root {
				return current, nil
			}
			return root, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("security: inspect path %q: %w", current, err)
		}
		if samePathKey(current, root) {
			return root, nil
		}

		parent := filepath.Dir(current)
		if samePathKey(parent, current) {
			return "", fmt.Errorf("security: compare workspace target %q: reached filesystem root", target)
		}
		current = parent
	}
}

func cleanedPathKey(path string) string {
	cleaned := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

func samePathKey(a string, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(cleanedPathKey(a), cleanedPathKey(b))
	}
	return cleanedPathKey(a) == cleanedPathKey(b)
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
