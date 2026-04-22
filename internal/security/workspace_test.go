package security

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkspaceSandboxCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prepare   func(t *testing.T, root string, outside string)
		action    func(root string, outside string) Action
		expectErr string
	}{
		{
			name: "read path inside workspace is allowed",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				mustWriteWorkspaceFile(t, filepath.Join(root, "notes.txt"), "hello")
			},
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, "notes.txt")
			},
		},
		{
			name: "read traversal is rejected",
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, filepath.Join("..", "outside.txt"))
			},
			expectErr: "escapes workspace root",
		},
		{
			name: "absolute path outside workspace is rejected",
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, outside)
			},
			expectErr: "escapes workspace root",
		},
		{
			name: "symlinked file outside workspace is rejected",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				mustWriteWorkspaceFile(t, outside, "secret")
				mustSymlinkOrSkip(t, outside, filepath.Join(root, "linked.txt"))
			},
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, "linked.txt")
			},
			expectErr: "via symlink",
		},
		{
			name: "symlinked parent directory outside workspace is rejected",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				outsideDir := filepath.Dir(outside)
				if err := os.MkdirAll(outsideDir, 0o755); err != nil {
					t.Fatalf("mkdir outside dir: %v", err)
				}
				mustSymlinkOrSkip(t, outsideDir, filepath.Join(root, "linked-dir"))
			},
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeWrite, "filesystem_write_file", "write_file", root, filepath.Join("linked-dir", "new.txt"))
			},
			expectErr: "via symlink",
		},
		{
			name: "missing nested write path inside workspace is allowed",
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeWrite, "filesystem_write_file", "write_file", root, filepath.Join("new", "nested.txt"))
			},
		},
		{
			name: "grep defaults to workspace root when dir is empty",
			action: func(root string, outside string) Action {
				return Action{
					Type: ActionTypeRead,
					Payload: ActionPayload{
						ToolName:          "filesystem_grep",
						Resource:          "filesystem_grep",
						Operation:         "grep",
						Workdir:           root,
						TargetType:        TargetTypeDirectory,
						SandboxTargetType: TargetTypeDirectory,
					},
				}
			},
		},
		{
			name: "bash workdir inside workspace is allowed",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
					t.Fatalf("mkdir scripts: %v", err)
				}
			},
			action: func(root string, outside string) Action {
				return bashAction(root, "pwd", "scripts")
			},
		},
		{
			name: "bash workdir traversal is rejected",
			action: func(root string, outside string) Action {
				return bashAction(root, "pwd", filepath.Join("..", "outside"))
			},
			expectErr: "escapes workspace root",
		},
		{
			name: "webfetch does not trigger workspace checks",
			action: func(root string, outside string) Action {
				return Action{
					Type: ActionTypeRead,
					Payload: ActionPayload{
						ToolName:   "webfetch",
						Resource:   "webfetch",
						Operation:  "fetch",
						Workdir:    root,
						TargetType: TargetTypeURL,
						Target:     "https://example.com",
					},
				}
			},
		},
		{
			name: "missing workspace root is rejected for path action",
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", "", "notes.txt")
			},
			expectErr: "workspace root is empty",
		},
		{
			name: "empty file target is deferred to tool validation",
			action: func(root string, outside string) Action {
				return fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, "")
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			outsideRoot := t.TempDir()
			outsideFile := filepath.Join(outsideRoot, "outside.txt")
			if tt.prepare != nil {
				tt.prepare(t, root, outsideFile)
			}

			sandbox := NewWorkspaceSandbox()
			_, err := sandbox.Check(context.Background(), tt.action(root, outsideFile))
			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestWorkspaceSandboxCheckShortCircuits(t *testing.T) {
	t.Parallel()

	sandbox := NewWorkspaceSandbox()
	root := t.TempDir()
	action := fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, "notes.txt")

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sandbox.Check(canceledCtx, action)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}

	_, err = sandbox.Check(context.Background(), Action{
		Type: ActionTypeRead,
		Payload: ActionPayload{
			Resource: "filesystem_read_file",
			Workdir:  root,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "tool_name is empty") {
		t.Fatalf("expected action validation error, got %v", err)
	}
}

func TestWorkspaceSandboxCheckRejectsInvalidCapabilityToken(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	action := fileAction(ActionTypeRead, "filesystem_read_file", "read_file", root, "notes.txt")
	action.Payload.CapabilityToken = &CapabilityToken{}

	_, err := NewWorkspaceSandbox().Check(context.Background(), action)
	if err == nil || !strings.Contains(err.Error(), "capability token path not allowed") {
		t.Fatalf("expected capability token path rejection, got %v", err)
	}
}

func TestBuildWorkspacePlan(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tests := []struct {
		name       string
		action     Action
		wantOK     bool
		wantTarget string
		wantErr    string
	}{
		{
			name: "mcp action bypasses workspace sandbox",
			action: Action{
				Type: ActionTypeMCP,
				Payload: ActionPayload{
					ToolName:   "mcp.call",
					Resource:   "mcp",
					Workdir:    root,
					TargetType: TargetTypeMCP,
				},
			},
			wantOK: false,
		},
		{
			name: "missing root is rejected when sandbox is needed",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:          "filesystem_read_file",
					Resource:          "filesystem_read_file",
					TargetType:        TargetTypePath,
					Target:            "notes.txt",
					SandboxTargetType: TargetTypePath,
				},
			},
			wantErr: "workspace root is empty",
		},
		{
			name: "empty path target falls back to tool validation",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:          "filesystem_read_file",
					Resource:          "filesystem_read_file",
					Workdir:           root,
					TargetType:        TargetTypePath,
					SandboxTargetType: TargetTypePath,
				},
			},
			wantOK: false,
		},
		{
			name: "directory target defaults to current workspace",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:          "filesystem_grep",
					Resource:          "filesystem_grep",
					Workdir:           root,
					TargetType:        TargetTypeDirectory,
					SandboxTargetType: TargetTypeDirectory,
				},
			},
			wantOK:     true,
			wantTarget: ".",
		},
		{
			name: "sandbox target type falls back to target type",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					ToolName:   "filesystem_grep",
					Resource:   "filesystem_grep",
					Workdir:    root,
					TargetType: TargetTypeDirectory,
					Target:     "docs",
				},
			},
			wantOK:     true,
			wantTarget: "docs",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			plan, ok, err := buildWorkspacePlan(tt.action)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantTarget != "" && plan.target != tt.wantTarget {
				t.Fatalf("target = %q, want %q", plan.target, tt.wantTarget)
			}
		})
	}
}

func TestWorkspaceSandboxValidateWorkspacePlanErrors(t *testing.T) {
	t.Parallel()

	sandbox := NewWorkspaceSandbox()
	_, err := sandbox.validateWorkspacePlan(workspacePlan{
		root:       filepath.Join(t.TempDir(), "missing"),
		target:     "notes.txt",
		targetType: TargetTypePath,
		actionType: ActionTypeRead,
	})
	if err == nil || !strings.Contains(err.Error(), "resolve workspace root") {
		t.Fatalf("expected resolve workspace root error, got %v", err)
	}
}

func TestNeedsWorkspaceSandbox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		typ    ActionType
		expect bool
	}{
		{name: "read is sandboxed", typ: ActionTypeRead, expect: true},
		{name: "write is sandboxed", typ: ActionTypeWrite, expect: true},
		{name: "bash is sandboxed", typ: ActionTypeBash, expect: true},
		{name: "mcp is not sandboxed", typ: ActionTypeMCP, expect: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := needsWorkspaceSandbox(Action{Type: tt.typ}); got != tt.expect {
				t.Fatalf("needsWorkspaceSandbox(%q) = %v, want %v", tt.typ, got, tt.expect)
			}
		})
	}
}

func TestSandboxTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		action     Action
		wantTarget string
		wantOK     bool
	}{
		{
			name: "bash defaults to current directory",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					SandboxTarget: "",
				},
			},
			wantTarget: ".",
			wantOK:     true,
		},
		{
			name: "bash uses explicit sandbox target",
			action: Action{
				Type: ActionTypeBash,
				Payload: ActionPayload{
					SandboxTarget: "scripts",
				},
			},
			wantTarget: "scripts",
			wantOK:     true,
		},
		{
			name: "fallback to target when sandbox target is empty",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					TargetType: TargetTypePath,
					Target:     "main.go",
				},
			},
			wantTarget: "main.go",
			wantOK:     true,
		},
		{
			name: "directory target defaults to dot",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					TargetType: TargetTypeDirectory,
				},
			},
			wantTarget: ".",
			wantOK:     true,
		},
		{
			name: "empty path target is ignored",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					TargetType: TargetTypePath,
				},
			},
			wantOK: false,
		},
		{
			name: "unsupported target type is ignored",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					TargetType: TargetTypeURL,
					Target:     "https://example.com",
				},
			},
			wantOK: false,
		},
		{
			name: "sandbox target takes precedence",
			action: Action{
				Type: ActionTypeRead,
				Payload: ActionPayload{
					TargetType:        TargetTypePath,
					Target:            "main.go",
					SandboxTargetType: TargetTypeDirectory,
					SandboxTarget:     "docs",
				},
			},
			wantTarget: "docs",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotTarget, gotOK := sandboxTarget(tt.action)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotTarget != tt.wantTarget {
				t.Fatalf("target = %q, want %q", gotTarget, tt.wantTarget)
			}
		})
	}
}

func TestCanonicalWorkspaceRoot(t *testing.T) {
	t.Parallel()

	sandbox := NewWorkspaceSandbox()
	existing := t.TempDir()
	got, err := sandbox.canonicalWorkspaceRoot(existing)
	if err != nil {
		t.Fatalf("canonicalWorkspaceRoot(existing) error: %v", err)
	}
	expectedExisting, err := filepath.Abs(existing)
	if err != nil {
		t.Fatalf("filepath.Abs(existing): %v", err)
	}
	if !samePathKey(got, expectedExisting) {
		t.Fatalf("canonicalWorkspaceRoot(existing) = %q, want %q", got, filepath.Clean(expectedExisting))
	}
	if _, ok := sandbox.canonicalRoots.Load(cleanedPathKey(existing)); !ok {
		t.Fatalf("expected canonical root cache entry for %q", existing)
	}
	gotCached, err := sandbox.canonicalWorkspaceRoot(existing)
	if err != nil {
		t.Fatalf("canonicalWorkspaceRoot(cached) error: %v", err)
	}
	if !samePathKey(gotCached, got) {
		t.Fatalf("canonicalWorkspaceRoot(cached) = %q, want %q", gotCached, got)
	}

	missing := filepath.Join(t.TempDir(), "missing", "dir")
	_, err = sandbox.canonicalWorkspaceRoot(missing)
	if err == nil || !strings.Contains(err.Error(), "resolve workspace root") {
		t.Fatalf("expected missing root error, got %v", err)
	}

	notDirRoot := filepath.Join(t.TempDir(), "file.txt")
	mustWriteWorkspaceFile(t, notDirRoot, "content")
	_, err = sandbox.canonicalWorkspaceRoot(notDirRoot)
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("expected not-a-directory error, got %v", err)
	}

	symlinkRoot := t.TempDir()
	targetRoot := filepath.Join(symlinkRoot, "target")
	linkRoot := filepath.Join(symlinkRoot, "link")
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		t.Fatalf("mkdir target root: %v", err)
	}
	if err := os.Symlink(targetRoot, linkRoot); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
	got, err = sandbox.canonicalWorkspaceRoot(linkRoot)
	if err != nil {
		t.Fatalf("canonicalWorkspaceRoot(link) error: %v", err)
	}
	expectedLink, err := filepath.Abs(targetRoot)
	if err != nil {
		t.Fatalf("filepath.Abs(targetRoot): %v", err)
	}
	if !samePathKey(got, expectedLink) {
		t.Fatalf("canonicalWorkspaceRoot(link) = %q, want %q", got, filepath.Clean(expectedLink))
	}
	if _, ok := sandbox.canonicalRoots.Load(cleanedPathKey(linkRoot)); ok {
		t.Fatalf("did not expect symlinked root %q to be cached", linkRoot)
	}
}

func TestCanonicalWorkspaceRootPermissionErrorFallsBackToAbsoluteRoot(t *testing.T) {
	originalEvalSymlinks := evalSymlinks
	evalSymlinks = func(path string) (string, error) {
		return "", os.ErrPermission
	}
	defer func() {
		evalSymlinks = originalEvalSymlinks
	}()

	root := t.TempDir()
	got, err := NewWorkspaceSandbox().canonicalWorkspaceRoot(root)
	if err != nil {
		t.Fatalf("expected permission fallback for workspace root, got %v", err)
	}
	want, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("filepath.Abs(root): %v", err)
	}
	if !samePathKey(got, want) {
		t.Fatalf("canonicalWorkspaceRoot() = %q, want %q", got, want)
	}
}

func TestCanonicalWorkspaceRootPermissionErrorRejectsSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("mkdir real root: %v", err)
	}
	linkRoot := filepath.Join(base, "root-link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	originalEvalSymlinks := evalSymlinks
	evalSymlinks = func(path string) (string, error) {
		return "", os.ErrPermission
	}
	defer func() {
		evalSymlinks = originalEvalSymlinks
	}()

	_, err := NewWorkspaceSandbox().canonicalWorkspaceRoot(linkRoot)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected symlink root to reject permission fallback, got %v", err)
	}
}

func TestAbsoluteWorkspaceTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	absoluteInside := filepath.Join(root, "abs.txt")

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{
			name:   "empty target resolves to root",
			target: "",
			want:   root,
		},
		{
			name:   "relative target resolves from root",
			target: filepath.Join("dir", "file.txt"),
			want:   filepath.Join(root, "dir", "file.txt"),
		},
		{
			name:   "absolute target keeps absolute form",
			target: absoluteInside,
			want:   absoluteInside,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := absoluteWorkspaceTarget(root, tt.target)
			if err != nil {
				t.Fatalf("absoluteWorkspaceTarget() error: %v", err)
			}
			wantAbs, err := filepath.Abs(tt.want)
			if err != nil {
				t.Fatalf("filepath.Abs(%q): %v", tt.want, err)
			}
			wantCanonical := cleanedPathKey(wantAbs)
			if got != wantCanonical {
				t.Fatalf("absoluteWorkspaceTarget() = %q, want %q", got, wantCanonical)
			}
		})
	}
}

func TestValidateTargetVolume(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "windows" {
		t.Skip("volume validation is Windows-specific")
	}

	root := `C:\workspace`
	inside := `C:\workspace\file.txt`
	outside := `D:\secret.txt`

	if err := validateTargetVolume(root, inside); err != nil {
		t.Fatalf("validateTargetVolume(inside) error: %v", err)
	}
	if err := validateTargetVolume(root, outside); err == nil || !strings.Contains(err.Error(), "different volume") {
		t.Fatalf("expected different volume error, got %v", err)
	}
}

func TestNormalizeVolumeName(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "windows" {
		t.Skip("volume normalization is Windows-specific")
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "drive letter is normalized",
			path: `C:\workspace`,
			want: `c:`,
		},
		{
			name: "extended path prefix is removed",
			path: `\\?\C:\workspace`,
			want: `c:`,
		},
		{
			name: "unc share is normalized",
			path: `\\server\share\dir`,
			want: `\\server\share`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeVolumeName(tt.path); got != tt.want {
				t.Fatalf("normalizeVolumeName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestEnsureNoSymlinkEscape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(t *testing.T) (root string, target string, original string)
		expectErr string
	}{
		{
			name: "root target is allowed",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				root := t.TempDir()
				return root, root, "."
			},
		},
		{
			name: "symlink inside workspace is allowed",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				root := t.TempDir()
				realDir := filepath.Join(root, "real")
				if err := os.MkdirAll(realDir, 0o755); err != nil {
					t.Fatalf("mkdir real dir: %v", err)
				}
				linkDir := filepath.Join(root, "link")
				if err := os.Symlink(realDir, linkDir); err != nil {
					t.Skipf("symlink not supported in this environment: %v", err)
				}
				return root, filepath.Join(root, "link", "new.txt"), filepath.Join("link", "new.txt")
			},
		},
		{
			name: "broken symlink is rejected",
			setup: func(t *testing.T) (string, string, string) {
				t.Helper()
				root := t.TempDir()
				linkDir := filepath.Join(root, "broken")
				if err := os.Symlink(filepath.Join(root, "missing"), linkDir); err != nil {
					t.Skipf("symlink not supported in this environment: %v", err)
				}
				return root, filepath.Join(root, "broken", "new.txt"), filepath.Join("broken", "new.txt")
			},
			expectErr: "resolve symlink",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, target, original := tt.setup(t)
			_, err := ensureNoSymlinkEscape(root, target, original)
			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestEnsureResolvedPathWithinWorkspacePermissionErrorFallsBackForPlainPath(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "notes.txt")
	mustWriteWorkspaceFile(t, candidate, "hello")

	originalEvalSymlinks := evalSymlinks
	evalSymlinks = func(path string) (string, error) {
		return "", os.ErrPermission
	}
	defer func() {
		evalSymlinks = originalEvalSymlinks
	}()

	err := ensureResolvedPathWithinWorkspace(root, candidate, "notes.txt")
	if err != nil {
		t.Fatalf("expected plain path permission fallback, got %v", err)
	}
}

func TestEnsureResolvedPathWithinWorkspacePermissionErrorRejectsSymlinkedPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	mustWriteWorkspaceFile(t, target, "secret")

	link := filepath.Join(root, "linked.txt")
	mustSymlinkOrSkip(t, target, link)

	originalEvalSymlinks := evalSymlinks
	evalSymlinks = func(path string) (string, error) {
		return "", os.ErrPermission
	}
	defer func() {
		evalSymlinks = originalEvalSymlinks
	}()

	err := ensureResolvedPathWithinWorkspace(root, link, "linked.txt")
	if err == nil || !strings.Contains(err.Error(), "resolve symlink") {
		t.Fatalf("expected symlink permission error, got %v", err)
	}
}

func TestCanFallbackToCandidateOnPermissionRejectsSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("mkdir real root: %v", err)
	}

	symlinkRoot := filepath.Join(base, "root-link")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	candidate := filepath.Join(symlinkRoot, "notes.txt")
	mustWriteWorkspaceFile(t, filepath.Join(realRoot, "notes.txt"), "hello")

	allowed, err := canFallbackToCandidateOnPermission(symlinkRoot, candidate)
	if err != nil {
		t.Fatalf("canFallbackToCandidateOnPermission() error: %v", err)
	}
	if allowed {
		t.Fatalf("expected symlink workspace root to reject permission fallback")
	}
}

func TestWorkspaceExecutionPlanValidateForExecution(t *testing.T) {
	t.Parallel()

	t.Run("nil plan is allowed", func(t *testing.T) {
		t.Parallel()
		var plan *WorkspaceExecutionPlan
		if err := plan.ValidateForExecution(); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("empty root is rejected", func(t *testing.T) {
		t.Parallel()
		plan := &WorkspaceExecutionPlan{Target: "target.txt"}
		err := plan.ValidateForExecution()
		if err == nil || !strings.Contains(err.Error(), "workspace plan root is empty") {
			t.Fatalf("expected empty root error, got %v", err)
		}
	})

	t.Run("empty target is rejected", func(t *testing.T) {
		t.Parallel()
		plan := &WorkspaceExecutionPlan{Root: t.TempDir()}
		err := plan.ValidateForExecution()
		if err == nil || !strings.Contains(err.Error(), "workspace plan target is empty") {
			t.Fatalf("expected empty target error, got %v", err)
		}
	})

	t.Run("anchor path change is rejected", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		insideDir := filepath.Join(root, "inside")
		if err := os.MkdirAll(insideDir, 0o755); err != nil {
			t.Fatalf("mkdir inside: %v", err)
		}
		outsideDir := t.TempDir()

		linkPath := filepath.Join(root, "swap-link")
		if err := os.Symlink(insideDir, linkPath); err != nil {
			t.Skipf("symlink not supported in this environment: %v", err)
		}

		plan, err := NewWorkspaceSandbox().Check(context.Background(), fileAction(
			ActionTypeRead,
			"filesystem_read_file",
			"read_file",
			root,
			filepath.Join("swap-link", "a.txt"),
		))
		if err != nil {
			t.Fatalf("build plan: %v", err)
		}
		if plan == nil {
			t.Fatalf("expected non-nil plan")
		}

		if err := os.Remove(linkPath); err != nil {
			t.Fatalf("remove link: %v", err)
		}
		if err := os.Symlink(outsideDir, linkPath); err != nil {
			t.Fatalf("replace link: %v", err)
		}

		err = plan.ValidateForExecution()
		if err == nil || !strings.Contains(err.Error(), "changed before execution") {
			t.Fatalf("expected changed-before-execution error, got %v", err)
		}
	})

	t.Run("anchor snapshot change is rejected", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		file := filepath.Join(root, "main.go")
		mustWriteWorkspaceFile(t, file, "short")

		plan, err := NewWorkspaceSandbox().Check(context.Background(), fileAction(
			ActionTypeRead,
			"filesystem_read_file",
			"read_file",
			root,
			"main.go",
		))
		if err != nil {
			t.Fatalf("build plan: %v", err)
		}
		if plan == nil {
			t.Fatalf("expected non-nil plan")
		}

		if err := os.WriteFile(file, []byte("longer-content"), 0o644); err != nil {
			t.Fatalf("mutate file: %v", err)
		}

		err = plan.ValidateForExecution()
		if err == nil || !strings.Contains(err.Error(), "changed before execution") {
			t.Fatalf("expected changed-before-execution error, got %v", err)
		}
	})

	t.Run("valid unchanged plan passes", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		mustWriteWorkspaceFile(t, filepath.Join(root, "main.go"), "package main\n")
		plan, err := NewWorkspaceSandbox().Check(context.Background(), fileAction(
			ActionTypeRead,
			"filesystem_read_file",
			"read_file",
			root,
			"main.go",
		))
		if err != nil {
			t.Fatalf("build plan: %v", err)
		}
		if plan == nil {
			t.Fatalf("expected non-nil plan")
		}
		if err := plan.ValidateForExecution(); err != nil {
			t.Fatalf("expected valid plan, got %v", err)
		}
	})

	t.Run("nearest existing path failure is returned", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		file := filepath.Join(root, "file.txt")
		mustWriteWorkspaceFile(t, file, "x")
		plan := &WorkspaceExecutionPlan{
			Root:            root,
			Target:          filepath.Join(file, "child.txt"),
			RequestedTarget: filepath.Join("file.txt", "child.txt"),
			anchorPath:      file,
			anchorSnapshot:  pathSnapshot{},
		}
		err := plan.ValidateForExecution()
		if err == nil || !strings.Contains(err.Error(), "inspect path") {
			t.Fatalf("expected inspect path error, got %v", err)
		}
	})

	t.Run("anchor path mismatch is rejected", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		targetA := filepath.Join(root, "a")
		targetB := filepath.Join(root, "b")
		if err := os.MkdirAll(targetA, 0o755); err != nil {
			t.Fatalf("mkdir targetA: %v", err)
		}
		if err := os.MkdirAll(targetB, 0o755); err != nil {
			t.Fatalf("mkdir targetB: %v", err)
		}
		snapshot, err := capturePathSnapshot(targetB)
		if err != nil {
			t.Fatalf("capturePathSnapshot(targetB): %v", err)
		}
		plan := &WorkspaceExecutionPlan{
			Root:            root,
			Target:          targetA,
			RequestedTarget: "a",
			anchorPath:      targetB,
			anchorSnapshot:  snapshot,
		}
		err = plan.ValidateForExecution()
		if err == nil || !strings.Contains(err.Error(), "changed before execution") {
			t.Fatalf("expected changed-before-execution error, got %v", err)
		}
	})
}

func TestCapturePathSnapshotAndEqual(t *testing.T) {
	t.Parallel()

	t.Run("regular file snapshot and equality", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		file := filepath.Join(root, "a.txt")
		mustWriteWorkspaceFile(t, file, "hello")

		left, err := capturePathSnapshot(file)
		if err != nil {
			t.Fatalf("capturePathSnapshot(left): %v", err)
		}
		right, err := capturePathSnapshot(file)
		if err != nil {
			t.Fatalf("capturePathSnapshot(right): %v", err)
		}
		if !left.Equal(right) {
			t.Fatalf("expected equal snapshots: %#v vs %#v", left, right)
		}
	})

	t.Run("symlink snapshot captures link target", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		targetDir := filepath.Join(root, "target")
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		linkPath := filepath.Join(root, "link")
		if err := os.Symlink(targetDir, linkPath); err != nil {
			t.Skipf("symlink not supported in this environment: %v", err)
		}

		snapshot, err := capturePathSnapshot(linkPath)
		if err != nil {
			t.Fatalf("capturePathSnapshot(link): %v", err)
		}
		if snapshot.linkTarget == "" {
			t.Fatalf("expected link target in snapshot, got %#v", snapshot)
		}
	})

	t.Run("missing path returns inspect error", func(t *testing.T) {
		t.Parallel()

		_, err := capturePathSnapshot(filepath.Join(t.TempDir(), "missing.txt"))
		if err == nil || !strings.Contains(err.Error(), "inspect path") {
			t.Fatalf("expected inspect error, got %v", err)
		}
	})

	t.Run("different snapshots are not equal", func(t *testing.T) {
		t.Parallel()

		a := pathSnapshot{mode: 0o644, size: 10, modUnix: 1, linkTarget: "a"}
		b := pathSnapshot{mode: 0o755, size: 10, modUnix: 1, linkTarget: "a"}
		if a.Equal(b) {
			t.Fatalf("expected snapshots to differ: %#v vs %#v", a, b)
		}
	})
}

func TestNearestExistingPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(t *testing.T) (root string, target string)
		expect    func(root string, target string) string
		expectErr string
	}{
		{
			name: "returns root for missing nested path",
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				root := t.TempDir()
				return root, filepath.Join(root, "missing", "file.txt")
			},
			expect: func(root string, target string) string {
				return cleanedPathKey(root)
			},
		},
		{
			name: "returns nearest existing ancestor",
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				root := t.TempDir()
				dir := filepath.Join(root, "dir")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("mkdir dir: %v", err)
				}
				return root, filepath.Join(dir, "missing", "file.txt")
			},
			expect: func(root string, target string) string {
				return cleanedPathKey(filepath.Join(root, "dir"))
			},
		},
		{
			name: "returns broken symlink ancestor",
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				root := t.TempDir()
				link := filepath.Join(root, "broken")
				if err := os.Symlink(filepath.Join(root, "missing"), link); err != nil {
					t.Skipf("symlink not supported in this environment: %v", err)
				}
				return root, filepath.Join(link, "child.txt")
			},
			expect: func(root string, target string) string {
				return cleanedPathKey(filepath.Join(root, "broken"))
			},
		},
		{
			name: "returns inspect error for non-not-exist lstat",
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				root := t.TempDir()
				file := filepath.Join(root, "file.txt")
				mustWriteWorkspaceFile(t, file, "x")
				return root, filepath.Join(file, "child.txt")
			},
			expectErr: "inspect path",
		},
		{
			name: "missing root path returns root",
			setup: func(t *testing.T) (string, string) {
				t.Helper()
				root := filepath.Join(t.TempDir(), "missing-root")
				return root, root
			},
			expect: func(root string, target string) string {
				return cleanedPathKey(root)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, target := tt.setup(t)
			got, err := nearestExistingPath(root, target)
			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("nearestExistingPath() error: %v", err)
			}
			want := tt.expect(root, target)
			if got != want {
				t.Fatalf("nearestExistingPath() = %q, want %q", got, want)
			}
		})
	}
}

func TestEnsureNoSymlinkEscapeReturnsNearestPathError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "file.txt")
	mustWriteWorkspaceFile(t, file, "x")

	_, err := ensureNoSymlinkEscape(root, filepath.Join(file, "child.txt"), filepath.Join("file.txt", "child.txt"))
	if err == nil || !strings.Contains(err.Error(), "inspect path") {
		t.Fatalf("expected inspect path error, got %v", err)
	}
}

func TestValidateTargetVolumeNoVolumeShortCircuit(t *testing.T) {
	t.Parallel()

	if err := validateTargetVolume(filepath.Join(t.TempDir(), "workspace"), filepath.Join(t.TempDir(), "target")); err != nil {
		t.Fatalf("validateTargetVolume() error = %v, want nil on non-volume paths", err)
	}
}

func TestSplitRelativePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "dot path returns nil",
			path: ".",
			want: nil,
		},
		{
			name: "nested path is split by separator",
			path: filepath.Join("a", "b", "c"),
			want: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitRelativePath(tt.path)
			if len(got) != len(tt.want) {
				t.Fatalf("splitRelativePath(%q) len = %d, want %d", tt.path, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("splitRelativePath(%q)[%d] = %q, want %q", tt.path, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIsWithinWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inside := filepath.Join(root, "sub", "file.txt")
	outside := filepath.Join(t.TempDir(), "outside.txt")

	tests := []struct {
		name   string
		root   string
		target string
		want   bool
	}{
		{
			name:   "root itself is inside",
			root:   root,
			target: root,
			want:   true,
		},
		{
			name:   "child path is inside",
			root:   root,
			target: inside,
			want:   true,
		},
		{
			name:   "outside path is rejected",
			root:   root,
			target: outside,
			want:   false,
		},
		{
			name:   "invalid root returns false",
			root:   "",
			target: root,
			want:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isWithinWorkspace(tt.root, tt.target); got != tt.want {
				t.Fatalf("isWithinWorkspace(%q, %q) = %v, want %v", tt.root, tt.target, got, tt.want)
			}
		})
	}
}

func fileAction(actionType ActionType, toolName string, operation string, workdir string, target string) Action {
	return Action{
		Type: actionType,
		Payload: ActionPayload{
			ToolName:          toolName,
			Resource:          toolName,
			Operation:         operation,
			Workdir:           workdir,
			TargetType:        TargetTypePath,
			Target:            target,
			SandboxTargetType: TargetTypePath,
			SandboxTarget:     target,
		},
	}
}

func bashAction(workdir string, command string, requestedWorkdir string) Action {
	return Action{
		Type: ActionTypeBash,
		Payload: ActionPayload{
			ToolName:          "bash",
			Resource:          "bash",
			Operation:         "command",
			Workdir:           workdir,
			TargetType:        TargetTypeCommand,
			Target:            command,
			SandboxTargetType: TargetTypeDirectory,
			SandboxTarget:     requestedWorkdir,
		},
	}
}

func mustWriteWorkspaceFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustSymlinkOrSkip(t *testing.T, target string, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
}
