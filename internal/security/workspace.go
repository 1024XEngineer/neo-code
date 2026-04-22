package security

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var evalSymlinks = filepath.EvalSymlinks

// WorkspaceSandbox enforces workspace-relative path boundaries for tool actions.
type WorkspaceSandbox struct {
	canonicalRoots sync.Map
}

// NewWorkspaceSandbox creates a sandbox that blocks traversal and symlink escape.
func NewWorkspaceSandbox() *WorkspaceSandbox {
	return &WorkspaceSandbox{}
}

// Check validates that the action stays within the configured workspace root.
func (s *WorkspaceSandbox) Check(ctx context.Context, action Action) (*WorkspaceExecutionPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := action.Validate(); err != nil {
		return nil, err
	}
	// 对携带 capability token 的子代理动作，先执行路径 allowlist 复核。
	if err := ValidateCapabilityForWorkspace(action); err != nil {
		return nil, err
	}

	plan, ok, err := buildWorkspacePlan(action)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	return s.validateWorkspacePlan(plan)
}

type workspacePlan struct {
	root       string
	target     string
	targetType TargetType
	actionType ActionType
}

// WorkspaceExecutionPlan binds a validated workspace target to the later
// execution phase.
type WorkspaceExecutionPlan struct {
	Root            string
	Target          string
	RequestedTarget string
	TargetType      TargetType
	ActionType      ActionType
	anchorPath      string
	anchorSnapshot  pathSnapshot
}

type pathSnapshot struct {
	mode       fs.FileMode
	size       int64
	modUnix    int64
	linkTarget string
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

	targetType := action.Payload.SandboxTargetType
	if targetType == "" {
		targetType = action.Payload.TargetType
	}

	return workspacePlan{
		root:       root,
		target:     target,
		targetType: targetType,
		actionType: action.Type,
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
func (s *WorkspaceSandbox) validateWorkspacePlan(plan workspacePlan) (*WorkspaceExecutionPlan, error) {
	root, err := s.canonicalWorkspaceRoot(plan.root)
	if err != nil {
		return nil, err
	}

	target, err := absoluteWorkspaceTarget(root, plan.target)
	if err != nil {
		return nil, err
	}
	if !isWithinWorkspace(root, target) {
		return nil, fmt.Errorf("security: path %q escapes workspace root", plan.target)
	}

	anchorPath, err := ensureNoSymlinkEscape(root, target, plan.target)
	if err != nil {
		return nil, err
	}
	anchorSnapshot, err := capturePathSnapshot(anchorPath)
	if err != nil {
		return nil, err
	}

	return &WorkspaceExecutionPlan{
		Root:            root,
		Target:          target,
		RequestedTarget: plan.target,
		TargetType:      plan.targetType,
		ActionType:      plan.actionType,
		anchorPath:      anchorPath,
		anchorSnapshot:  anchorSnapshot,
	}, nil
}

// ValidateForExecution rechecks the validated workspace anchor before a tool
// touches the filesystem to narrow the TOCTOU window between sandbox check and
// execution.
func (p *WorkspaceExecutionPlan) ValidateForExecution() error {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(p.Root) == "" {
		return errors.New("security: workspace plan root is empty")
	}
	if strings.TrimSpace(p.Target) == "" {
		return errors.New("security: workspace plan target is empty")
	}

	currentAnchor, err := nearestExistingPath(p.Root, p.Target)
	if err != nil {
		return err
	}
	if !samePathKey(currentAnchor, p.anchorPath) {
		return fmt.Errorf("security: workspace target changed before execution: %q", p.RequestedTarget)
	}

	currentSnapshot, err := capturePathSnapshot(currentAnchor)
	if err != nil {
		return err
	}
	if !p.anchorSnapshot.Equal(currentSnapshot) {
		return fmt.Errorf("security: workspace target changed before execution: %q", p.RequestedTarget)
	}

	return ensureResolvedPathWithinWorkspace(p.Root, currentAnchor, p.RequestedTarget)
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

	canonicalRoot, err := evalSymlinks(absoluteRoot)
	if err != nil {
		if !errors.Is(err, os.ErrPermission) {
			return "", false, fmt.Errorf("security: resolve workspace root: %w", err)
		}
		allowed, inspectErr := canFallbackToCandidateOnPermission(absoluteRoot, absoluteRoot)
		if inspectErr != nil {
			return "", false, inspectErr
		}
		if !allowed {
			return "", false, fmt.Errorf("security: resolve workspace root %q: %w", absoluteRoot, err)
		}
		canonicalRoot = absoluteRoot
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

	return cleanedPathKey(absoluteTarget), nil
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
func ensureNoSymlinkEscape(root string, target string, original string) (string, error) {
	existingPath, err := nearestExistingPath(root, target)
	if err != nil {
		return "", err
	}
	if existingPath == root {
		return root, nil
	}

	if err := ensureResolvedPathWithinWorkspace(root, existingPath, original); err != nil {
		return "", err
	}
	return existingPath, nil
}

func ensureResolvedPathWithinWorkspace(root string, candidate string, original string) error {
	if samePathKey(root, candidate) {
		return nil
	}
	resolved, err := evalSymlinks(candidate)
	if err != nil {
		if !errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("security: resolve symlink %q: %w", candidate, err)
		}
		fallbackAllowed, inspectErr := canFallbackToCandidateOnPermission(root, candidate)
		if inspectErr != nil {
			return inspectErr
		}
		if !fallbackAllowed {
			return fmt.Errorf("security: resolve symlink %q: %w", candidate, err)
		}
		resolved = candidate
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("security: resolve symlink %q: %w", candidate, err)
	}
	if !isWithinWorkspace(root, resolved) {
		return fmt.Errorf("security: path %q escapes workspace root via symlink", original)
	}
	return nil
}

// canFallbackToCandidateOnPermission 在 EvalSymlinks 遇到权限错误时，逐段确认 root 到 candidate 的现存路径不含符号链接。
func canFallbackToCandidateOnPermission(root string, candidate string) (bool, error) {
	rootInfo, err := os.Lstat(filepath.Clean(root))
	if err != nil {
		return false, fmt.Errorf("security: inspect path %q: %w", root, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}

	relativePath, err := filepath.Rel(root, candidate)
	if err != nil {
		return false, fmt.Errorf("security: compare workspace target %q: %w", candidate, err)
	}
	if relativePath == "." {
		return true, nil
	}

	current := cleanedPathKey(root)
	for _, segment := range splitRelativePath(relativePath) {
		current = cleanedPathKey(filepath.Join(current, segment))
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return false, fmt.Errorf("security: inspect path %q: %w", current, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, nil
		}
	}
	return true, nil
}

func capturePathSnapshot(path string) (pathSnapshot, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return pathSnapshot{}, fmt.Errorf("security: inspect path %q: %w", path, err)
	}

	snapshot := pathSnapshot{
		mode:    info.Mode(),
		size:    info.Size(),
		modUnix: info.ModTime().UnixNano(),
	}
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, readErr := os.Readlink(path)
		if readErr != nil {
			return pathSnapshot{}, fmt.Errorf("security: read symlink %q: %w", path, readErr)
		}
		snapshot.linkTarget = filepath.Clean(linkTarget)
	}
	return snapshot, nil
}

// Equal reports whether two path snapshots represent the same path identity.
func (s pathSnapshot) Equal(other pathSnapshot) bool {
	return s.mode == other.mode &&
		s.size == other.size &&
		s.modUnix == other.modUnix &&
		s.linkTarget == other.linkTarget
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
	originalTarget := current
	root = cleanedPathKey(root)
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if !samePathKey(current, originalTarget) && info.Mode()&os.ModeSymlink == 0 && !info.IsDir() {
				return "", fmt.Errorf("security: inspect path %q: parent is not a directory", current)
			}
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
