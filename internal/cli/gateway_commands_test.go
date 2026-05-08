package cli

import (
	"reflect"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"neo-code/internal/gateway"
	agentruntime "neo-code/internal/runtime"
)

func TestNormalizeGatewayLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantError bool
	}{
		{"debug lower", "debug", "debug", false},
		{"info lower", "info", "info", false},
		{"warn lower", "warn", "warn", false},
		{"error lower", "error", "error", false},
		{"DEBUG upper", "DEBUG", "debug", false},
		{"Info mixed", "Info", "info", false},
		{"with spaces", "  warn  ", "warn", false},
		{"invalid empty", "", "", true},
		{"invalid value", "trace", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeGatewayLogLevel(tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error for input %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeGatewayLogLevel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMustReadInheritedWorkdir(t *testing.T) {
	t.Run("nil cmd", func(t *testing.T) {
		if got := mustReadInheritedWorkdir(nil); got != "" {
			t.Fatalf("mustReadInheritedWorkdir(nil) = %q, want empty", got)
		}
	})

	t.Run("cmd without workdir flag", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		// no --workdir flag registered, GetString returns error
		if got := mustReadInheritedWorkdir(cmd); got != "" {
			t.Fatalf("mustReadInheritedWorkdir(cmd without flag) = %q, want empty", got)
		}
	})

	t.Run("cmd with workdir flag", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("workdir", "", "")
		_ = cmd.Flags().Set("workdir", "/tmp/project")
		if got := mustReadInheritedWorkdir(cmd); got != "/tmp/project" {
			t.Fatalf("mustReadInheritedWorkdir(cmd) = %q, want /tmp/project", got)
		}
	})
}

func TestInjectRunnerDispatcherIntoRuntime(t *testing.T) {
	injectRunnerDispatcherIntoRuntime(nil, nil)
	injectRunnerDispatcherIntoRuntime(&gatewayRuntimePortBridge{}, nil)
	injectRunnerDispatcherIntoRuntime(&gatewayRuntimePortBridge{}, &gateway.RunnerToolManager{})

	nonServiceBridge := &gatewayRuntimePortBridge{runtime: &runtimeStub{}}
	multiNonService := gateway.NewMultiWorkspaceRuntime(nil, "", nil)
	multiNonService.PreloadWorkspaceBundle("non-service", nonServiceBridge, func() error { return nil })
	injectRunnerDispatcherIntoRuntime(multiNonService, &gateway.RunnerToolManager{})

	service := &agentruntime.Service{}
	bridge := &gatewayRuntimePortBridge{runtime: service}
	multi := gateway.NewMultiWorkspaceRuntime(nil, "", nil)
	multi.PreloadWorkspaceBundle("default", bridge, func() error { return nil })

	injectRunnerDispatcherIntoRuntime(multi, &gateway.RunnerToolManager{})

	field := reflect.ValueOf(service).Elem().FieldByName("runnerToolDispatcher")
	if !field.IsValid() || field.IsNil() {
		t.Fatal("runnerToolDispatcher was not injected")
	}
}

func TestNewGatewayIdleShutdownControllerUsesExpectedDefaultTimeout(t *testing.T) {
	controller := newGatewayIdleShutdownController(nil, nil)
	if controller.idleTimeout != 5*time.Minute {
		t.Fatalf("idleTimeout = %v, want %v", controller.idleTimeout, 5*time.Minute)
	}
}
