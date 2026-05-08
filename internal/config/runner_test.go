package config

import (
	"testing"
	"time"
)

func TestRunnerConfigApplyDefaultsCloneAndDurations(t *testing.T) {
	var nilCfg *RunnerConfig
	nilCfg.ApplyDefaults(defaultRunnerConfig())

	cfg := RunnerConfig{
		WorkdirAllowlist: []string{"/tmp/work"},
	}
	defaults := defaultRunnerConfig()
	cfg.ApplyDefaults(defaults)

	if cfg.GatewayAddress != DefaultRunnerGatewayAddress {
		t.Fatalf("GatewayAddress = %q", cfg.GatewayAddress)
	}
	if cfg.HeartbeatInterval() != 10*time.Second {
		t.Fatalf("HeartbeatInterval() = %s", cfg.HeartbeatInterval())
	}
	if cfg.ReconnectBackoffMin() != 500*time.Millisecond {
		t.Fatalf("ReconnectBackoffMin() = %s", cfg.ReconnectBackoffMin())
	}
	if cfg.ReconnectBackoffMax() != 10*time.Second {
		t.Fatalf("ReconnectBackoffMax() = %s", cfg.ReconnectBackoffMax())
	}
	if cfg.RequestTimeout() != 30*time.Second {
		t.Fatalf("RequestTimeout() = %s", cfg.RequestTimeout())
	}

	if (RunnerConfig{}).HeartbeatInterval() != 10*time.Second {
		t.Fatalf("zero HeartbeatInterval() = %s", (RunnerConfig{}).HeartbeatInterval())
	}
	if (RunnerConfig{}).ReconnectBackoffMin() != 500*time.Millisecond {
		t.Fatalf("zero ReconnectBackoffMin() = %s", (RunnerConfig{}).ReconnectBackoffMin())
	}
	if (RunnerConfig{}).ReconnectBackoffMax() != 10*time.Second {
		t.Fatalf("zero ReconnectBackoffMax() = %s", (RunnerConfig{}).ReconnectBackoffMax())
	}
	if (RunnerConfig{}).RequestTimeout() != 30*time.Second {
		t.Fatalf("zero RequestTimeout() = %s", (RunnerConfig{}).RequestTimeout())
	}

	clone := cfg.Clone()
	clone.WorkdirAllowlist[0] = "/changed"
	if cfg.WorkdirAllowlist[0] != "/tmp/work" {
		t.Fatal("Clone() did not deep copy WorkdirAllowlist")
	}
}

func TestRunnerConfigValidate(t *testing.T) {
	if err := (RunnerConfig{}).Validate(); err != nil {
		t.Fatalf("disabled RunnerConfig.Validate() error = %v", err)
	}

	cases := []RunnerConfig{
		{Enabled: true},
		{Enabled: true, GatewayAddress: "127.0.0.1:8080", HeartbeatIntervalSec: -1},
		{Enabled: true, GatewayAddress: "127.0.0.1:8080", HeartbeatIntervalSec: 1, ReconnectBackoffMinM: -1, ReconnectBackoffMaxM: 1},
		{Enabled: true, GatewayAddress: "127.0.0.1:8080", HeartbeatIntervalSec: 1, ReconnectBackoffMinM: 2, ReconnectBackoffMaxM: 1},
		{Enabled: true, GatewayAddress: "127.0.0.1:8080", HeartbeatIntervalSec: 1, ReconnectBackoffMinM: 1, ReconnectBackoffMaxM: 2, RequestTimeoutSec: -1},
	}
	for _, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate() error = nil for %#v", cfg)
		}
	}

	valid := RunnerConfig{
		Enabled:              true,
		GatewayAddress:       "127.0.0.1:8080",
		HeartbeatIntervalSec: 1,
		ReconnectBackoffMinM: 1,
		ReconnectBackoffMaxM: 2,
		RequestTimeoutSec:    3,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfigValidateSnapshotIncludesRunnerValidation(t *testing.T) {
	cfg := StaticDefaults()
	cfg.Providers = []ProviderConfig{testDefaultProviderConfig()}
	cfg.SelectedProvider = testProviderName
	cfg.Workdir = t.TempDir()
	cfg.Runner = RunnerConfig{
		Enabled:              true,
		GatewayAddress:       "127.0.0.1:8080",
		HeartbeatIntervalSec: 1,
		ReconnectBackoffMinM: 2,
		ReconnectBackoffMaxM: 1,
		RequestTimeoutSec:    1,
	}
	if err := cfg.ValidateSnapshot(); err == nil || err.Error() != "config: runner: reconnect_backoff_min_ms must be less than or equal to reconnect_backoff_max_ms" {
		t.Fatalf("ValidateSnapshot() error = %v", err)
	}
}
