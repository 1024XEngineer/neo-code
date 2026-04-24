package infra

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func exitCommand(code int) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "exit", string(rune('0'+code)))
	}
	return exec.Command("sh", "-c", "exit "+string(rune('0'+code)))
}

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

func TestOpenExternalResourceRunsCommandAndReturnsRunError(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "guide.html")
	if err := os.WriteFile(filePath, []byte("guide"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	oldGOOS := runtimeGOOSForOpenResource
	oldExec := execCommandForOpenResource
	runtimeGOOSForOpenResource = "linux"
	t.Cleanup(func() {
		runtimeGOOSForOpenResource = oldGOOS
		execCommandForOpenResource = oldExec
	})

	var gotName string
	var gotArgs []string
	execCommandForOpenResource = func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return exitCommand(0)
	}

	if err := OpenExternalResource(filePath); err != nil {
		t.Fatalf("OpenExternalResource() error = %v", err)
	}
	if gotName != "xdg-open" {
		t.Fatalf("expected command xdg-open, got %q", gotName)
	}
	if len(gotArgs) != 1 || gotArgs[0] != filePath {
		t.Fatalf("unexpected args: %v", gotArgs)
	}

	execCommandForOpenResource = func(name string, args ...string) *exec.Cmd {
		return exitCommand(7)
	}
	err := OpenExternalResource(filePath)
	if err == nil || !strings.Contains(err.Error(), "open resource") {
		t.Fatalf("expected wrapped run error, got %v", err)
	}

	if err := OpenExternalResource(" "); err == nil || !strings.Contains(err.Error(), "target is empty") {
		t.Fatalf("expected empty target error, got %v", err)
	}
}

func TestFileURLToLocalPathGuardBranches(t *testing.T) {
	if _, err := fileURLToLocalPath(nil); err == nil || !strings.Contains(err.Error(), "invalid file url") {
		t.Fatalf("expected invalid file url error, got %v", err)
	}

	oldGOOS := runtimeGOOSForOpenResource
	runtimeGOOSForOpenResource = "windows"
	t.Cleanup(func() {
		runtimeGOOSForOpenResource = oldGOOS
	})

	parsed := &url.URL{Scheme: "file", Host: "C:", Path: "/Users/test/guide.html"}
	got, err := fileURLToLocalPath(parsed)
	if err != nil {
		t.Fatalf("fileURLToLocalPath() error = %v", err)
	}
	if !strings.HasPrefix(got, "C:") || !strings.Contains(got, "Users") {
		t.Fatalf("unexpected windows path result %q", got)
	}

	parsed = &url.URL{Scheme: "file", Path: "/D:/workspace/guide.html"}
	got, err = fileURLToLocalPath(parsed)
	if err != nil {
		t.Fatalf("fileURLToLocalPath() windows prefixed path error = %v", err)
	}
	if strings.HasPrefix(got, "/D:") || !strings.HasPrefix(got, "D:") {
		t.Fatalf("expected /D:/... prefix to be normalized, got %q", got)
	}
}

func TestWindowsDriveHelperFunctions(t *testing.T) {
	testsPrefix := []struct {
		input string
		want  bool
	}{
		{input: "/C:/tmp/a.txt", want: true},
		{input: "/z:/tmp/a.txt", want: true},
		{input: "C:/tmp/a.txt", want: false},
		{input: "/12/tmp", want: false},
	}
	for _, tt := range testsPrefix {
		if got := hasWindowsFileURLDrivePrefix(tt.input); got != tt.want {
			t.Fatalf("hasWindowsFileURLDrivePrefix(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}

	testsHost := []struct {
		input string
		want  bool
	}{
		{input: "C:", want: true},
		{input: "d:", want: true},
		{input: "cd", want: false},
		{input: "C:/", want: false},
	}
	for _, tt := range testsHost {
		if got := hasWindowsDriveHost(tt.input); got != tt.want {
			t.Fatalf("hasWindowsDriveHost(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeOpenResourceLocalPathGuardBranches(t *testing.T) {
	if _, err := normalizeOpenResourceLocalPath(" "); err == nil || !strings.Contains(err.Error(), "local path is empty") {
		t.Fatalf("expected empty local path error, got %v", err)
	}

	oldAbs := absPathForOpenResource
	absPathForOpenResource = func(path string) (string, error) {
		return "", os.ErrInvalid
	}
	t.Cleanup(func() {
		absPathForOpenResource = oldAbs
	})

	if _, err := normalizeOpenResourceLocalPath("relative/path"); err == nil || !strings.Contains(err.Error(), "resolve absolute path") {
		t.Fatalf("expected absolute path resolver error, got %v", err)
	}
}

func TestFileURLToLocalPathErrorBranches(t *testing.T) {
	oldGOOS := runtimeGOOSForOpenResource
	runtimeGOOSForOpenResource = "linux"
	t.Cleanup(func() {
		runtimeGOOSForOpenResource = oldGOOS
	})

	if _, err := fileURLToLocalPath(&url.URL{Scheme: "file", Host: "remote", Path: "/tmp/a.txt"}); err == nil ||
		!strings.Contains(err.Error(), "unsupported file url host") {
		t.Fatalf("expected unsupported host error, got %v", err)
	}
	if _, err := fileURLToLocalPath(&url.URL{Scheme: "file", Host: "localhost", Path: ""}); err == nil ||
		!strings.Contains(err.Error(), "file url path is empty") {
		t.Fatalf("expected empty file url path error, got %v", err)
	}
	if _, err := fileURLToLocalPath(&url.URL{Scheme: "file", Path: "/tmp/%zz"}); err == nil ||
		!strings.Contains(err.Error(), "decode file url path") {
		t.Fatalf("expected decode file url path error, got %v", err)
	}
}
