package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWorkspacePath(t *testing.T) {
	base := t.TempDir()
	subdir := filepath.Join(base, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tests := []struct {
		name      string
		base      string
		requested string
		check     func(t *testing.T, got string)
		wantErr   bool
	}{
		{"resolve absolute path", base, subdir, func(t *testing.T, got string) {
			if got != subdir {
				t.Errorf("expected %v, got %v", subdir, got)
			}
		}, false},
		{"resolve relative path", base, "sub", func(t *testing.T, got string) {
			if got != subdir {
				t.Errorf("expected %v, got %v", subdir, got)
			}
		}, false},
		{"empty base uses cwd", "", ".", func(t *testing.T, got string) {
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}
			if got != cwd {
				t.Errorf("expected %v, got %v", cwd, got)
			}
		}, false},
		{"empty requested uses dot", base, "", func(t *testing.T, got string) {
			if got != base {
				t.Errorf("expected %v, got %v", base, got)
			}
		}, false},
		{"non-existent path", base, "nonexistent", func(t *testing.T, got string) {}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveWorkspacePath(tt.base, tt.requested)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResolveWorkspacePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestSelectSessionWorkdir(t *testing.T) {
	tests := []struct {
		name           string
		sessionWorkdir string
		defaultWorkdir string
		want           string
	}{
		{"prefer session workdir", "/session", "/default", "/session"},
		{"fallback to default", "", "/default", "/default"},
		{"both empty", "", "", ""},
		{"session with whitespace", "  /session  ", "/default", "/session"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SelectSessionWorkdir(tt.sessionWorkdir, tt.defaultWorkdir); got != tt.want {
				t.Errorf("SelectSessionWorkdir() = %v, want %v", got, tt.want)
			}
		})
	}
}
