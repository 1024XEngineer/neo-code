package infra

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenResourceCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		goos         string
		target       string
		wantCommand  string
		wantArgsHead []string
	}{
		{
			name:         "windows",
			goos:         "windows",
			target:       "https://www.modelscope.cn/",
			wantCommand:  "cmd",
			wantArgsHead: []string{"/c", "start", ""},
		},
		{
			name:         "darwin",
			goos:         "darwin",
			target:       "https://www.modelscope.cn/",
			wantCommand:  "open",
			wantArgsHead: []string{},
		},
		{
			name:         "linux-default",
			goos:         "linux",
			target:       "https://www.modelscope.cn/",
			wantCommand:  "xdg-open",
			wantArgsHead: []string{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotCommand, gotArgs, err := openResourceCommand(tt.goos, tt.target)
			if err != nil {
				t.Fatalf("openResourceCommand() error = %v", err)
			}
			if gotCommand != tt.wantCommand {
				t.Fatalf("openResourceCommand() command = %q, want %q", gotCommand, tt.wantCommand)
			}
			if len(gotArgs) == 0 || gotArgs[len(gotArgs)-1] != tt.target {
				t.Fatalf("openResourceCommand() args should end with target, got %v", gotArgs)
			}
			if len(tt.wantArgsHead) > 0 {
				if len(gotArgs) < len(tt.wantArgsHead)+1 {
					t.Fatalf("openResourceCommand() args too short: %v", gotArgs)
				}
				for i := range tt.wantArgsHead {
					if gotArgs[i] != tt.wantArgsHead[i] {
						t.Fatalf("openResourceCommand() args[%d] = %q, want %q", i, gotArgs[i], tt.wantArgsHead[i])
					}
				}
			}
		})
	}
}

func TestNormalizeOpenResourceTargetAllowsHTTPAndHTTPS(t *testing.T) {
	t.Parallel()

	tests := []string{
		"https://www.modelscope.cn/",
		"http://localhost:8080",
	}
	for _, target := range tests {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeOpenResourceTarget(target)
			if err != nil {
				t.Fatalf("normalizeOpenResourceTarget() error = %v", err)
			}
			if got != target {
				t.Fatalf("normalizeOpenResourceTarget() = %q, want %q", got, target)
			}
		})
	}
}

func TestNormalizeOpenResourceTargetResolvesRelativeFilePathViaAbsResolver(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "modelscope-guide.html")
	if err := os.WriteFile(filePath, []byte("guide"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	oldAbsResolver := absPathForOpenResource
	absPathForOpenResource = func(path string) (string, error) {
		if path == "modelscope-guide.html" {
			return filePath, nil
		}
		return oldAbsResolver(path)
	}
	t.Cleanup(func() {
		absPathForOpenResource = oldAbsResolver
	})

	got, err := normalizeOpenResourceLocalPath("modelscope-guide.html")
	if err != nil {
		t.Fatalf("normalizeOpenResourceLocalPath() error = %v", err)
	}
	if got != filePath {
		t.Fatalf("normalizeOpenResourceLocalPath() = %q, want %q", got, filePath)
	}
}

func TestNormalizeOpenResourceTargetRejectsInvalidTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		target    string
		errorPart string
	}{
		{
			name:      "empty",
			target:    " ",
			errorPart: "target is empty",
		},
		{
			name:      "missing-file",
			target:    filepath.Join(t.TempDir(), "missing.html"),
			errorPart: "stat",
		},
		{
			name:      "directory",
			target:    t.TempDir(),
			errorPart: "is a directory",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalizeOpenResourceTarget(tt.target)
			if err == nil || !strings.Contains(err.Error(), tt.errorPart) {
				t.Fatalf("normalizeOpenResourceTarget() error = %v, want contains %q", err, tt.errorPart)
			}
		})
	}
}

func TestNormalizeOpenResourceTargetValidatesFileURLPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filePath := filepath.Join(root, "modelscope-guide.html")
	if err := os.WriteFile(filePath, []byte("guide"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filePath)}).String()
	got, err := normalizeOpenResourceTarget(fileURL)
	if err != nil {
		t.Fatalf("normalizeOpenResourceTarget(fileURL) error = %v", err)
	}
	if got != filePath {
		t.Fatalf("normalizeOpenResourceTarget(fileURL) = %q, want %q", got, filePath)
	}
}

func TestNormalizeOpenResourceTargetRejectsInvalidFileURLTargets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tests := []struct {
		name      string
		target    string
		errorPart string
	}{
		{
			name:      "unsupported-host",
			target:    "file://example.com/tmp/guide.html",
			errorPart: "unsupported file url host",
		},
		{
			name:      "missing-file",
			target:    (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(root, "missing.html"))}).String(),
			errorPart: "stat",
		},
		{
			name:      "directory",
			target:    (&url.URL{Scheme: "file", Path: filepath.ToSlash(root)}).String(),
			errorPart: "is a directory",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalizeOpenResourceTarget(tt.target)
			if err == nil || !strings.Contains(err.Error(), tt.errorPart) {
				t.Fatalf("normalizeOpenResourceTarget(%q) error = %v, want contains %q", tt.target, err, tt.errorPart)
			}
		})
	}
}
