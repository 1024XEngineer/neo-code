package ptyproxy

import (
	"strings"
	"testing"
)

func TestMergeEnvVarOverridesExistingValue(t *testing.T) {
	merged := MergeEnvVar([]string{
		"PATH=/bin",
		"NEOCODE_SHELL_SESSION=shell-old",
		"HOME=/home/tester",
	}, ShellSessionEnv, "shell-new")

	var sessionEntries []string
	for _, item := range merged {
		if strings.HasPrefix(item, ShellSessionEnv+"=") {
			sessionEntries = append(sessionEntries, item)
		}
	}
	if len(sessionEntries) != 1 {
		t.Fatalf("session entries len = %d, want 1", len(sessionEntries))
	}
	if sessionEntries[0] != ShellSessionEnv+"=shell-new" {
		t.Fatalf("session entry = %q", sessionEntries[0])
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
		GatewayListenAddress: "  /tmp/gw.sock  ",
		GatewayTokenFile:     "  /tmp/token  ",
	})
	if err != nil {
		t.Fatalf("NormalizeShellOptions() error = %v", err)
	}
	if opts.Shell != "/bin/zsh" {
		t.Fatalf("Shell = %q, want %q", opts.Shell, "/bin/zsh")
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
