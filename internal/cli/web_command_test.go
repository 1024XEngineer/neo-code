package cli

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeWebCommandTestFile 写入 web 命令测试所需的最小文件内容，避免各测试重复拼装目录。
func writeWebCommandTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// chdirForWebCommandTest 切换当前工作目录，并在测试结束后恢复。
func chdirForWebCommandTest(t *testing.T, dir string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

// stubResolveExecutablePath 替换可执行文件路径解析，便于覆盖发布包布局分支。
func stubResolveExecutablePath(t *testing.T, fn func() (string, error)) {
	t.Helper()
	original := resolveExecutablePath
	resolveExecutablePath = fn
	t.Cleanup(func() {
		resolveExecutablePath = original
	})
}

// stubWebCommandHooks 替换 web 命令测试中的可注入执行点，并在结束后恢复。
func stubWebCommandHooks(
	t *testing.T,
	startGateway func(context.Context, gatewayCommandOptions, string, fs.FS, func(string)) error,
	build func(string, *log.Logger) error,
	lookPath func(string) (string, error),
) {
	t.Helper()
	originalStart := webCommandStartGatewayServer
	originalBuild := webCommandBuildFrontend
	originalLookPath := webCommandLookPath
	if startGateway != nil {
		webCommandStartGatewayServer = startGateway
	}
	if build != nil {
		webCommandBuildFrontend = build
	}
	if lookPath != nil {
		webCommandLookPath = lookPath
	}
	t.Cleanup(func() {
		webCommandStartGatewayServer = originalStart
		webCommandBuildFrontend = originalBuild
		webCommandLookPath = originalLookPath
	})
}

func TestFindWebSourceDirUsesCurrentWorkdir(t *testing.T) {
	tempDir := t.TempDir()
	chdirForWebCommandTest(t, tempDir)
	stubResolveExecutablePath(t, func() (string, error) {
		return "", errors.New("skip executable lookup")
	})

	writeWebCommandTestFile(t, filepath.Join(tempDir, "web", "package.json"), "{}")

	got := findWebSourceDir()
	want := filepath.Join(tempDir, "web")
	if got != want {
		t.Fatalf("findWebSourceDir() = %q, want %q", got, want)
	}
}

func TestFindWebSourceDirFallsBackToExecutableDir(t *testing.T) {
	tempDir := t.TempDir()
	chdirForWebCommandTest(t, tempDir)

	releaseDir := filepath.Join(tempDir, "release")
	writeWebCommandTestFile(t, filepath.Join(releaseDir, "web", "package.json"), "{}")
	stubResolveExecutablePath(t, func() (string, error) {
		return filepath.Join(releaseDir, "neocode.exe"), nil
	})

	got := findWebSourceDir()
	want := filepath.Join(releaseDir, "web")
	if got != want {
		t.Fatalf("findWebSourceDir() = %q, want %q", got, want)
	}
}

func TestResolveWebStaticDirFallsBackToExecutableDir(t *testing.T) {
	tempDir := t.TempDir()
	chdirForWebCommandTest(t, tempDir)

	releaseDir := filepath.Join(tempDir, "release")
	writeWebCommandTestFile(t, filepath.Join(releaseDir, "web", "dist", "index.html"), "<html></html>")
	stubResolveExecutablePath(t, func() (string, error) {
		return filepath.Join(releaseDir, "neocode.exe"), nil
	})

	got, err := resolveWebStaticDir("")
	if err != nil {
		t.Fatalf("resolveWebStaticDir returned error: %v", err)
	}
	want := filepath.Join(releaseDir, "web", "dist")
	if got != want {
		t.Fatalf("resolveWebStaticDir() = %q, want %q", got, want)
	}
}

func TestFindNPMBinaryMissingMessage(t *testing.T) {
	stubWebCommandHooks(t, nil, nil, func(string) (string, error) {
		return "", errors.New("not found")
	})

	_, err := findNPMBinary()
	if err == nil {
		t.Fatal("findNPMBinary() error = nil, want error")
	}
	message := err.Error()
	if !strings.Contains(message, "Node.js and npm") {
		t.Fatalf("findNPMBinary() error = %q, want Node.js/npm guidance", message)
	}
	if !strings.Contains(message, "`neocode web`") {
		t.Fatalf("findNPMBinary() error = %q, want neocode web guidance", message)
	}
}

func TestRunWebCommandBuildsFrontendWhenDistMissing(t *testing.T) {
	tempDir := t.TempDir()
	chdirForWebCommandTest(t, tempDir)
	writeWebCommandTestFile(t, filepath.Join(tempDir, "web", "package.json"), "{}")

	buildCalled := false
	var capturedStaticDir string
	sentinelErr := errors.New("stop after start")
	stubWebCommandHooks(
		t,
		func(_ context.Context, _ gatewayCommandOptions, staticDir string, _ fs.FS, _ func(string)) error {
			capturedStaticDir = staticDir
			return sentinelErr
		},
		func(webDir string, _ *log.Logger) error {
			buildCalled = true
			writeWebCommandTestFile(t, filepath.Join(webDir, "dist", "index.html"), "<html></html>")
			return nil
		},
		nil,
	)

	err := runWebCommand(context.Background(), webCommandOptions{
		HTTPAddress: "127.0.0.1:8080",
		LogLevel:    "info",
		OpenBrowser: false,
		Workdir:     tempDir,
	})
	if !errors.Is(err, sentinelErr) {
		t.Fatalf("runWebCommand() error = %v, want sentinel error %v", err, sentinelErr)
	}
	if !buildCalled {
		t.Fatal("runWebCommand() did not invoke frontend build when dist was missing")
	}
	wantStaticDir := filepath.Join(tempDir, "web", "dist")
	if capturedStaticDir != wantStaticDir {
		t.Fatalf("startGatewayServer staticDir = %q, want %q", capturedStaticDir, wantStaticDir)
	}
}
