package ptyproxy

import (
	"strings"
	"testing"
)

func TestMergeEnvVarOverridesExistingValue(t *testing.T) {
	merged := MergeEnvVar([]string{
		"PATH=/bin",
		"NEOCODE_DIAG_SOCKET=/tmp/old.sock",
		"HOME=/home/tester",
	}, DiagSocketEnv, "/tmp/new.sock")

	var socketEntries []string
	for _, item := range merged {
		if strings.HasPrefix(item, DiagSocketEnv+"=") {
			socketEntries = append(socketEntries, item)
		}
	}
	if len(socketEntries) != 1 {
		t.Fatalf("socket entries len = %d, want 1", len(socketEntries))
	}
	if socketEntries[0] != DiagSocketEnv+"=/tmp/new.sock" {
		t.Fatalf("socket entry = %q", socketEntries[0])
	}
}

func TestMergeEnvVarEmptyKeyReturnsCopy(t *testing.T) {
	original := []string{"PATH=/bin", "HOME=/home/tester"}
	merged := MergeEnvVar(original, "", "/tmp/new.sock")
	if len(merged) != len(original) {
		t.Fatalf("merged len = %d, want %d", len(merged), len(original))
	}
	for i, item := range original {
		if merged[i] != item {
			t.Fatalf("merged[%d] = %q, want %q", i, merged[i], item)
		}
	}
}

func TestNormalizeShellOptionsDefaultsStdio(t *testing.T) {
	opts, err := NormalizeShellOptions(ManualShellOptions{
		Workdir: "/tmp",
		Shell:   "/bin/bash",
	})
	if err != nil {
		t.Fatalf("NormalizeShellOptions() error = %v", err)
	}
	if opts.Stdin == nil {
		t.Fatal("Stdin should not be nil after normalization")
	}
	if opts.Stdout == nil {
		t.Fatal("Stdout should not be nil after normalization")
	}
	if opts.Stderr == nil {
		t.Fatal("Stderr should not be nil after normalization")
	}
}

func TestNormalizeShellOptionsTrimsWhitespace(t *testing.T) {
	opts, err := NormalizeShellOptions(ManualShellOptions{
		Workdir:              "/tmp",
		Shell:                "  /bin/zsh  ",
		SocketPath:           "  /tmp/diag.sock  ",
		GatewayListenAddress: "  /tmp/gw.sock  ",
		GatewayTokenFile:     "  /tmp/token  ",
	})
	if err != nil {
		t.Fatalf("NormalizeShellOptions() error = %v", err)
	}
	if opts.Shell != "/bin/zsh" {
		t.Fatalf("Shell = %q, want %q", opts.Shell, "/bin/zsh")
	}
	if opts.SocketPath != "/tmp/diag.sock" {
		t.Fatalf("SocketPath = %q, want %q", opts.SocketPath, "/tmp/diag.sock")
	}
	if opts.GatewayListenAddress != "/tmp/gw.sock" {
		t.Fatalf("GatewayListenAddress = %q, want %q", opts.GatewayListenAddress, "/tmp/gw.sock")
	}
	if opts.GatewayTokenFile != "/tmp/token" {
		t.Fatalf("GatewayTokenFile = %q, want %q", opts.GatewayTokenFile, "/tmp/token")
	}
}

func TestNormalizeShellOptionsResolvesEmptyWorkdir(t *testing.T) {
	opts, err := NormalizeShellOptions(ManualShellOptions{})
	if err != nil {
		t.Fatalf("NormalizeShellOptions() error = %v", err)
	}
	if opts.Workdir == "" {
		t.Fatal("Workdir should not be empty after normalization")
	}
}

func TestIsAltScreenGuardEnabledFromEnv(t *testing.T) {
	t.Setenv(DiagAltScreenGuardDisableEnv, "")
	if !IsAltScreenGuardEnabledFromEnv() {
		t.Fatal("expected alt-screen guard enabled by default")
	}

	t.Setenv(DiagAltScreenGuardDisableEnv, "true")
	if IsAltScreenGuardEnabledFromEnv() {
		t.Fatal("expected alt-screen guard disabled when env=true")
	}

	t.Setenv(DiagAltScreenGuardDisableEnv, "false")
	if !IsAltScreenGuardEnabledFromEnv() {
		t.Fatal("expected alt-screen guard enabled when env=false")
	}

	t.Setenv(DiagAltScreenGuardDisableEnv, "invalid")
	if IsAltScreenGuardEnabledFromEnv() {
		t.Fatal("expected alt-screen guard disabled for non-empty invalid env")
	}
}

func TestFeatureRollbackEnvs(t *testing.T) {
	t.Setenv(IDMSessionPlanModeDisableEnv, "")
	if !IsIDMPlanModeEnabledFromEnv() {
		t.Fatal("expected IDM plan mode enabled by default")
	}
	t.Setenv(IDMSessionPlanModeDisableEnv, "1")
	if IsIDMPlanModeEnabledFromEnv() {
		t.Fatal("expected IDM plan mode disabled when env=1")
	}

	t.Setenv(DiagFastResponseDisableEnv, "")
	if !IsDiagFastResponseEnabledFromEnv() {
		t.Fatal("expected fast response enabled by default")
	}
	t.Setenv(DiagFastResponseDisableEnv, "invalid")
	if IsDiagFastResponseEnabledFromEnv() {
		t.Fatal("expected invalid non-empty fast response env to disable feature")
	}

	t.Setenv(DiagCacheDisableEnv, "")
	if !IsDiagCacheEnabledFromEnv() {
		t.Fatal("expected diagnosis cache enabled by default")
	}
	t.Setenv(DiagCacheDisableEnv, "true")
	if IsDiagCacheEnabledFromEnv() {
		t.Fatal("expected diagnosis cache disabled when env=true")
	}
}
