package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// assertLaunchSpecEqual 校验解析出的启动规格，保持测试断言结构一致。
func assertLaunchSpecEqual(t *testing.T, spec LaunchSpec, want LaunchSpec) {
	t.Helper()

	if spec.LaunchMode != want.LaunchMode {
		t.Fatalf("launch mode = %q, want %q", spec.LaunchMode, want.LaunchMode)
	}
	if spec.Executable != want.Executable {
		t.Fatalf("executable = %q, want %q", spec.Executable, want.Executable)
	}
	if !reflect.DeepEqual(spec.Args, want.Args) {
		t.Fatalf("args = %#v, want %#v", spec.Args, want.Args)
	}
}

func testAbsolutePath(name string) string {
	if runtime.GOOS == "windows" {
		return `C:\tools\` + name
	}
	return "/opt/tools/" + name
}

func testPathBinary(name string) string {
	if runtime.GOOS == "windows" {
		return `C:\usr\local\bin\` + name
	}
	return "/usr/local/bin/" + name
}

func TestResolveGatewayLaunchSpecWithDeps(t *testing.T) {
	t.Run("explicit binary has highest priority", func(t *testing.T) {
		executable := testAbsolutePath("neocode-gateway")
		spec, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{ExplicitBinary: executable},
			func(binary string) (string, error) {
				if binary == executable {
					return binary, nil
				}
				return "", errors.New("unexpected lookup")
			},
		)
		if err != nil {
			t.Fatalf("resolveGatewayLaunchSpecWithDeps() error = %v", err)
		}
		assertLaunchSpecEqual(t, spec, LaunchSpec{
			LaunchMode: LaunchModeExplicitPath,
			Executable: executable,
		})
	})

	t.Run("path binary preferred over fallback", func(t *testing.T) {
		executable := testPathBinary("neocode-gateway")
		spec, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{},
			func(binary string) (string, error) {
				if binary == "neocode-gateway" {
					return executable, nil
				}
				return "", errors.New("unexpected lookup")
			},
		)
		if err != nil {
			t.Fatalf("resolveGatewayLaunchSpecWithDeps() error = %v", err)
		}
		assertLaunchSpecEqual(t, spec, LaunchSpec{
			LaunchMode: LaunchModePathBinary,
			Executable: executable,
		})
	})

	t.Run("fallback to neocode subcommand", func(t *testing.T) {
		executable := testPathBinary("neocode")
		spec, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{},
			func(binary string) (string, error) {
				switch binary {
				case "neocode-gateway":
					return "", errors.New("not found")
				case "neocode":
					return executable, nil
				default:
					return "", errors.New("unexpected lookup")
				}
			},
		)
		if err != nil {
			t.Fatalf("resolveGatewayLaunchSpecWithDeps() error = %v", err)
		}
		assertLaunchSpecEqual(t, spec, LaunchSpec{
			LaunchMode: LaunchModeFallbackSubcommand,
			Executable: executable,
			Args:       []string{"gateway"},
		})
	})

	t.Run("explicit binary lookup failure returns error", func(t *testing.T) {
		_, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{ExplicitBinary: "/missing/neocode-gateway"},
			func(string) (string, error) {
				return "", errors.New("missing")
			},
		)
		if err == nil {
			t.Fatal("expected explicit lookup error")
		}
	})

	t.Run("explicit binary must be absolute path", func(t *testing.T) {
		lookupCalled := false
		_, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{ExplicitBinary: "neocode-gateway"},
			func(string) (string, error) {
				lookupCalled = true
				return "", nil
			},
		)
		if err == nil {
			t.Fatal("expected explicit path validation error")
		}
		if lookupCalled {
			t.Fatal("lookPath should not be called for invalid explicit path")
		}
	})

	t.Run("unix style path is not treated as absolute on windows", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("windows-only path semantics")
		}

		lookupCalled := false
		_, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{ExplicitBinary: "/tools/neocode-gateway.exe"},
			func(string) (string, error) {
				lookupCalled = true
				return "", nil
			},
		)
		if err == nil {
			t.Fatal("expected windows unix-style path validation error")
		}
		if lookupCalled {
			t.Fatal("lookPath should not be called for invalid explicit path")
		}
	})

	t.Run("path binary resolution rejects non-absolute path", func(t *testing.T) {
		_, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{},
			func(binary string) (string, error) {
				switch binary {
				case "neocode-gateway":
					return "neocode-gateway", nil
				case "neocode":
					return "/usr/local/bin/neocode", nil
				default:
					return "", errors.New("unexpected lookup")
				}
			},
		)
		if err == nil {
			t.Fatal("expected non-absolute path resolution error")
		}
		if !strings.Contains(err.Error(), "not an absolute path") {
			t.Fatalf("error = %v, want contains %q", err, "not an absolute path")
		}
	})

	t.Run("fallback binary resolution rejects non-absolute path", func(t *testing.T) {
		_, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{},
			func(binary string) (string, error) {
				switch binary {
				case "neocode-gateway":
					return "", errors.New("not found")
				case "neocode":
					return "neocode", nil
				default:
					return "", errors.New("unexpected lookup")
				}
			},
		)
		if err == nil {
			t.Fatal("expected non-absolute fallback path resolution error")
		}
		if !strings.Contains(err.Error(), "not an absolute path") {
			t.Fatalf("error = %v, want contains %q", err, "not an absolute path")
		}
	})

	t.Run("fallback fails when neocode is unavailable", func(t *testing.T) {
		_, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{},
			func(binary string) (string, error) {
				if binary == "neocode-gateway" || binary == "neocode" {
					return "", errors.New("not found")
				}
				return "", errors.New("unexpected lookup")
			},
		)
		if err == nil {
			t.Fatal("expected fallback resolution error")
		}
	})
}

func TestResolveGatewayLaunchSpec(t *testing.T) {
	executablePath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}

	spec, err := ResolveGatewayLaunchSpec(ResolveOptions{ExplicitBinary: executablePath})
	if err != nil {
		t.Fatalf("ResolveGatewayLaunchSpec() error = %v", err)
	}
	if spec.LaunchMode != LaunchModeExplicitPath {
		t.Fatalf("launch mode = %q, want %q", spec.LaunchMode, LaunchModeExplicitPath)
	}
	if spec.Executable == "" {
		t.Fatal("executable should not be empty")
	}
}

func TestStartDetachedGateway(t *testing.T) {
	t.Run("empty executable rejected", func(t *testing.T) {
		err := StartDetachedGateway(LaunchSpec{})
		if err == nil {
			t.Fatal("expected empty executable error")
		}
	})

	t.Run("starts process successfully", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("windows command start behavior differs in sandbox; skip process spawn assertion")
		}
		scriptDir := t.TempDir()
		markerPath := filepath.Join(scriptDir, "started.txt")
		scriptPath := filepath.Join(scriptDir, "start-gateway.sh")
		scriptContent := "#!/bin/sh\nprintf 'ok' > \"$1\"\n"
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o700); err != nil {
			t.Fatalf("write script: %v", err)
		}

		if err := StartDetachedGateway(LaunchSpec{
			Executable: scriptPath,
			Args:       []string{markerPath},
		}); err != nil {
			t.Fatalf("StartDetachedGateway() error = %v", err)
		}

		// 子进程异步启动，给少量时间完成写入。
		for i := 0; i < 20; i++ {
			if _, err := os.Stat(markerPath); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("expected marker file %q to be created", markerPath)
	})
}
