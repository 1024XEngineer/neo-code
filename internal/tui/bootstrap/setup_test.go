package bootstrap

import (
	"bufio"
	"strings"
	"testing"

	"go-llm-demo/configs"
)

func TestApplyAPIKeyEnvNameUpdatesConfig(t *testing.T) {
	cfg := configs.DefaultAppConfig()
	applyAPIKeyEnvName(cfg, "  TEST_KEY_ENV  ")

	if got := cfg.AI.APIKey; got != "TEST_KEY_ENV" {
		t.Fatalf("expected API key env name to be trimmed, got %q", got)
	}
}

func TestReadInteractiveLineRejectsEmptyInputThenReadsValue(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("\n  /retry  \n"))

	got, ok, err := readInteractiveLine(scanner, "> ")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got != "/retry" {
		t.Fatalf("expected trimmed input, got %q", got)
	}
}

func TestReadInteractiveLineTreatsExitAsStop(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("/exit\n"))

	got, ok, err := readInteractiveLine(scanner, "> ")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for /exit")
	}
	if got != "" {
		t.Fatalf("expected empty value, got %q", got)
	}
}

func TestHandleSetupDecisionHandlesProviderSwitch(t *testing.T) {
	cfg := configs.DefaultAppConfig()
	scanner := bufio.NewScanner(strings.NewReader("/provider openai\n"))

	decision, err := handleSetupDecision(scanner, cfg, false, "config.yaml")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if decision != setupRetry {
		t.Fatalf("expected setupRetry, got %v", decision)
	}
	if cfg.AI.Provider != "openai" {
		t.Fatalf("expected provider to switch, got %q", cfg.AI.Provider)
	}
	if cfg.AI.Model == "" {
		t.Fatal("expected provider switch to set a default model")
	}
}

func TestHandleSetupDecisionRejectsContinueWhenNotAllowed(t *testing.T) {
	cfg := configs.DefaultAppConfig()
	scanner := bufio.NewScanner(strings.NewReader("/continue\n/retry\n"))

	decision, err := handleSetupDecision(scanner, cfg, false, "config.yaml")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if decision != setupRetry {
		t.Fatalf("expected setupRetry after rejecting continue, got %v", decision)
	}
}
