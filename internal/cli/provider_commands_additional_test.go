package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	providertypes "neo-code/internal/provider/types"
)

func TestProviderCommandResolverFallbackAndResolveError(t *testing.T) {
	t.Run("fallback to default resolver when nil", func(t *testing.T) {
		add := newProviderAddCommandWithResolver(nil)
		if add.Use != "add <name>" {
			t.Fatalf("add.Use = %q, want add <name>", add.Use)
		}
		ls := newProviderLsCommandWithResolver(nil)
		if ls.Use != "ls" {
			t.Fatalf("ls.Use = %q, want ls", ls.Use)
		}
		rm := newProviderRmCommandWithResolver(nil)
		if rm.Use != "rm <name>" {
			t.Fatalf("rm.Use = %q, want rm <name>", rm.Use)
		}
	})

	resolverErr := selectionServiceResolverFunc(func(*cobra.Command) (SelectionService, error) {
		return nil, errors.New("resolve failed")
	})

	t.Run("add resolve error", func(t *testing.T) {
		cmd := newProviderAddCommandWithResolver(resolverErr)
		cmd.SetArgs([]string{"custom", "--driver", "openaicompat", "--url", "http://mock", "--api-key-env", "KEY"})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("err = %v, want contains resolve failed", err)
		}
	})

	t.Run("ls resolve error", func(t *testing.T) {
		cmd := newProviderLsCommandWithResolver(resolverErr)
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("err = %v, want contains resolve failed", err)
		}
	})

	t.Run("rm resolve error", func(t *testing.T) {
		cmd := newProviderRmCommandWithResolver(resolverErr)
		cmd.SetArgs([]string{"custom"})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("err = %v, want contains resolve failed", err)
		}
	})
}

func TestDefaultProviderAddCommandRunnerProviderNameFallback(t *testing.T) {
	t.Setenv("PROVIDER_KEY_FALLBACK", "sk-test")
	cmd := &cobra.Command{}
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetContext(context.Background())

	svc := &mockSelectionService{
		createCustomProviderFn: func(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error) {
			return configstate.Selection{ProviderID: "", ModelID: "m-1"}, nil
		},
	}
	err := defaultProviderAddCommandRunner(cmd, svc, "fallback-provider", providerAddOptions{
		Driver:    "openaicompat",
		URL:       "http://mock",
		APIKeyEnv: "PROVIDER_KEY_FALLBACK",
	})
	if err != nil {
		t.Fatalf("defaultProviderAddCommandRunner() error = %v", err)
	}
	if !strings.Contains(out.String(), "fallback-provider") {
		t.Fatalf("output = %q, want contains fallback provider name", out.String())
	}
}

func TestDefaultProviderLsAndRmCommandRunnerAdditionalBranches(t *testing.T) {
	t.Run("ls multiple providers formatting", func(t *testing.T) {
		cmd := &cobra.Command{}
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetContext(context.Background())

		svc := &mockSelectionService{
			listProviderOptionsFn: func(context.Context) ([]configstate.ProviderOption, error) {
				return []configstate.ProviderOption{
					{ID: "p1", Name: "p1", Driver: "openaicompat", Source: string(config.ProviderSourceBuiltin), Models: []providertypes.ModelDescriptor{{ID: "m1"}}},
					{ID: "p2", Name: "p2", Driver: "anthropic", Source: string(config.ProviderSourceCustom), Models: []providertypes.ModelDescriptor{{ID: "m1"}, {ID: "m2"}}},
				}, nil
			},
		}

		if err := defaultProviderLsCommandRunner(cmd, svc); err != nil {
			t.Fatalf("defaultProviderLsCommandRunner() error = %v", err)
		}
		if !strings.Contains(out.String(), "p1") || !strings.Contains(out.String(), "p2") {
			t.Fatalf("output = %q, want contains p1 and p2", out.String())
		}
	})

	t.Run("rm returns service error", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		svc := &mockSelectionService{
			removeCustomProviderFn: func(context.Context, string) error {
				return errors.New("remove failed")
			},
		}
		err := defaultProviderRmCommandRunner(cmd, svc, "bad-provider")
		if err == nil || !strings.Contains(err.Error(), "remove failed") {
			t.Fatalf("err = %v, want contains remove failed", err)
		}
	})
}
