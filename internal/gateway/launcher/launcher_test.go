package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestResolveGatewayLaunchSpecWithDeps(t *testing.T) {
	t.Run("explicit binary has highest priority", func(t *testing.T) {
		spec, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{ExplicitBinary: "/opt/tools/neocode-gateway"},
			func(binary string) (string, error) {
				if binary == "/opt/tools/neocode-gateway" {
					return binary, nil
				}
				return "", errors.New("unexpected lookup")
			},
			func() (string, error) {
				return "/usr/local/bin/neocode", nil
			},
		)
		if err != nil {
			t.Fatalf("resolveGatewayLaunchSpecWithDeps() error = %v", err)
		}
		if spec.LaunchMode != LaunchModeExplicitPath {
			t.Fatalf("launch mode = %q, want %q", spec.LaunchMode, LaunchModeExplicitPath)
		}
		if spec.Executable != "/opt/tools/neocode-gateway" {
			t.Fatalf("executable = %q, want %q", spec.Executable, "/opt/tools/neocode-gateway")
		}
		if len(spec.Args) != 0 {
			t.Fatalf("args = %#v, want empty", spec.Args)
		}
	})

	t.Run("path binary preferred over fallback", func(t *testing.T) {
		spec, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{},
			func(binary string) (string, error) {
				if binary == "neocode-gateway" {
					return "/usr/local/bin/neocode-gateway", nil
				}
				return "", errors.New("unexpected lookup")
			},
			func() (string, error) {
				return "/usr/local/bin/neocode", nil
			},
		)
		if err != nil {
			t.Fatalf("resolveGatewayLaunchSpecWithDeps() error = %v", err)
		}
		if spec.LaunchMode != LaunchModePathBinary {
			t.Fatalf("launch mode = %q, want %q", spec.LaunchMode, LaunchModePathBinary)
		}
		if spec.Executable != "/usr/local/bin/neocode-gateway" {
			t.Fatalf("executable = %q, want %q", spec.Executable, "/usr/local/bin/neocode-gateway")
		}
		if len(spec.Args) != 0 {
			t.Fatalf("args = %#v, want empty", spec.Args)
		}
	})

	t.Run("fallback to current executable subcommand", func(t *testing.T) {
		spec, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{},
			func(string) (string, error) {
				return "", errors.New("not found")
			},
			func() (string, error) {
				return "/usr/local/bin/neocode", nil
			},
		)
		if err != nil {
			t.Fatalf("resolveGatewayLaunchSpecWithDeps() error = %v", err)
		}
		if spec.LaunchMode != LaunchModeFallbackSubcommand {
			t.Fatalf("launch mode = %q, want %q", spec.LaunchMode, LaunchModeFallbackSubcommand)
		}
		if spec.Executable != "/usr/local/bin/neocode" {
			t.Fatalf("executable = %q, want %q", spec.Executable, "/usr/local/bin/neocode")
		}
		if !reflect.DeepEqual(spec.Args, []string{"gateway"}) {
			t.Fatalf("args = %#v, want %#v", spec.Args, []string{"gateway"})
		}
	})

	t.Run("explicit binary lookup failure returns error", func(t *testing.T) {
		_, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{ExplicitBinary: "/missing/neocode-gateway"},
			func(string) (string, error) {
				return "", errors.New("missing")
			},
			func() (string, error) {
				return "/usr/local/bin/neocode", nil
			},
		)
		if err == nil {
			t.Fatal("expected explicit lookup error")
		}
	})

	t.Run("fallback fails when current executable unavailable", func(t *testing.T) {
		_, err := resolveGatewayLaunchSpecWithDeps(
			ResolveOptions{},
			func(string) (string, error) {
				return "", errors.New("not found")
			},
			func() (string, error) {
				return "", errors.New("unavailable")
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
