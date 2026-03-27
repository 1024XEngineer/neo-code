package bash

import (
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

func TestToolHelpers(t *testing.T) {
	t.Parallel()

	tool := New(t.TempDir(), defaultShell(), 2*time.Second)
	if tool.Description() == "" {
		t.Fatalf("expected non-empty description")
	}
	if tool.Schema()["type"] != "object" {
		t.Fatalf("expected schema object")
	}

	tests := []struct {
		name  string
		shell string
		want  []string
	}{
		{
			name:  "powershell shell args",
			shell: "powershell",
			want:  []string{"powershell", "-NoProfile", "-Command"},
		},
		{
			name:  "bash shell args",
			shell: "bash",
			want:  []string{"bash", "-lc"},
		},
		{
			name:  "sh shell args",
			shell: "sh",
			want:  []string{"sh", "-lc"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := New(t.TempDir(), tt.shell, time.Second).shellArgs("echo hi")
			if len(got) < len(tt.want) {
				t.Fatalf("expected shell args prefix %v, got %v", tt.want, got)
			}
			for idx := range tt.want {
				if got[idx] != tt.want[idx] {
					t.Fatalf("expected shell args prefix %v, got %v", tt.want, got)
				}
			}
		})
	}

	t.Run("default shell args", func(t *testing.T) {
		got := New(t.TempDir(), "unknown", time.Second).shellArgs("echo hi")
		if goruntime.GOOS == "windows" {
			if got[0] != "powershell" {
				t.Fatalf("expected windows fallback to powershell, got %v", got)
			}
			return
		}
		if got[0] != "sh" {
			t.Fatalf("expected unix fallback to sh, got %v", got)
		}
	})
}

func TestResolveWorkdirVariants(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	subdir := filepath.Join(root, "sub")

	tests := []struct {
		name      string
		requested string
		expectErr string
		assert    func(t *testing.T, got string)
	}{
		{
			name:      "empty requested returns root",
			requested: "",
			assert: func(t *testing.T, got string) {
				t.Helper()
				if got != root {
					t.Fatalf("expected root %q, got %q", root, got)
				}
			},
		},
		{
			name:      "relative path joins root",
			requested: "sub",
			assert: func(t *testing.T, got string) {
				t.Helper()
				if got != subdir {
					t.Fatalf("expected subdir %q, got %q", subdir, got)
				}
			},
		},
		{
			name:      "absolute path inside root is allowed",
			requested: subdir,
			assert: func(t *testing.T, got string) {
				t.Helper()
				if got != subdir {
					t.Fatalf("expected subdir %q, got %q", subdir, got)
				}
			},
		},
		{
			name:      "escape is rejected",
			requested: filepath.Join("..", "escape"),
			expectErr: "escapes workspace root",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveWorkdir(root, tt.requested)
			if tt.expectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectErr) {
					t.Fatalf("expected error containing %q, got %v", tt.expectErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.assert != nil {
				tt.assert(t, got)
			}
		})
	}
}
