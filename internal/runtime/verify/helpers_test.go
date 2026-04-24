package verify

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestVerifierMetadataResult(t *testing.T) {
	t.Parallel()

	optional := verifierMetadataResult("file_exists", false, "expected_files", "skip")
	if optional.Status != VerificationPass || optional.Summary != "skip" {
		t.Fatalf("unexpected optional result: %+v", optional)
	}

	required := verifierMetadataResult("file_exists", true, "expected_files", "skip")
	if required.Status != VerificationSoftBlock || !strings.Contains(required.Summary, "expected_files") {
		t.Fatalf("unexpected required result: %+v", required)
	}
}

func TestVerificationDeniedResult(t *testing.T) {
	t.Parallel()

	result := verificationDeniedResult("file_exists", "denied", "policy", map[string]any{"a": 1})
	if result.Status != VerificationFail || result.ErrorClass != ErrorClassPermissionDenied {
		t.Fatalf("unexpected denied result: %+v", result)
	}
}

func TestMetadataStringSlice(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		meta map[string]any
		key  string
		want []string
	}{
		{name: "empty", meta: nil, key: "k", want: nil},
		{name: "string slice", meta: map[string]any{"k": []string{" a ", "", "b"}}, key: "k", want: []string{"a", "b"}},
		{name: "any slice", meta: map[string]any{"k": []any{" a ", 2, ""}}, key: "k", want: []string{"a", "2"}},
		{name: "single string", meta: map[string]any{"k": "  a  "}, key: "k", want: []string{"a"}},
		{name: "unsupported", meta: map[string]any{"k": 1}, key: "k", want: nil},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := metadataStringSlice(tc.meta, tc.key)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("metadataStringSlice() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestMetadataStringMapSlice(t *testing.T) {
	t.Parallel()

	t.Run("map string slices", func(t *testing.T) {
		t.Parallel()
		got := metadataStringMapSlice(map[string]any{
			"rules": map[string][]string{" a.txt ": []string{" x ", ""}},
		}, "rules")
		want := map[string][]string{"a.txt": []string{"x"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("metadataStringMapSlice() = %#v, want %#v", got, want)
		}
	})

	t.Run("map any", func(t *testing.T) {
		t.Parallel()
		got := metadataStringMapSlice(map[string]any{
			"rules": map[string]any{"a.txt": []any{" x ", 2}, " ": "ignored"},
		}, "rules")
		want := map[string][]string{"a.txt": []string{"x", "2"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("metadataStringMapSlice() = %#v, want %#v", got, want)
		}
	})

	t.Run("invalid returns nil", func(t *testing.T) {
		t.Parallel()
		if got := metadataStringMapSlice(map[string]any{"rules": "bad"}, "rules"); got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})
}

func TestResolvePathWithinWorkdir(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	child, err := resolvePathWithinWorkdir(workdir, "a/b.txt")
	if err != nil {
		t.Fatalf("resolvePathWithinWorkdir() error = %v", err)
	}
	if !strings.HasPrefix(child, workdir) {
		t.Fatalf("resolved path %q should be in workdir %q", child, workdir)
	}

	absChild, err := resolvePathWithinWorkdir(workdir, filepath.Join(workdir, "a.txt"))
	if err != nil {
		t.Fatalf("resolvePathWithinWorkdir(abs) error = %v", err)
	}
	if !strings.HasPrefix(absChild, workdir) {
		t.Fatalf("resolved abs path %q should be in workdir %q", absChild, workdir)
	}

	if _, err := resolvePathWithinWorkdir("", "a.txt"); err == nil {
		t.Fatalf("expected empty workdir error")
	}
	if _, err := resolvePathWithinWorkdir(workdir, ""); err == nil {
		t.Fatalf("expected empty path error")
	}
	if _, err := resolvePathWithinWorkdir(workdir, "../outside.txt"); err == nil {
		t.Fatalf("expected traversal error")
	}
}

func TestResolvePathWithinWorkdirRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	workdir := filepath.Join(root, "work")
	outsideDir := filepath.Join(root, "outside")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(workdir) error = %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}
	linkPath := filepath.Join(workdir, "link.txt")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink is not available on this platform: %v", err)
	}

	if _, err := resolvePathWithinWorkdir(workdir, "link.txt"); err == nil {
		t.Fatalf("expected symlink escape to be denied")
	}
}
