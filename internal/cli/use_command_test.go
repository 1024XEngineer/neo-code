package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	configstate "neo-code/internal/config/state"
)

func TestUseCommand(t *testing.T) {
	svc := &mockSelectionService{}
	cmd := newUseCommandWithResolver(staticSelectionResolver(svc))
	if cmd.Use != "use <provider>" {
		t.Fatalf("cmd.Use = %q, want %q", cmd.Use, "use <provider>")
	}

	originalRunner := runUseCommand
	t.Cleanup(func() { runUseCommand = originalRunner })

	called := false
	runUseCommand = func(c *cobra.Command, gotSvc SelectionService, name string, opts useCommandOptions) error {
		called = true
		if gotSvc != svc {
			t.Fatalf("injected service mismatch")
		}
		if name != "my-provider" {
			t.Fatalf("name = %q, want %q", name, "my-provider")
		}
		if opts.Model != "gpt-5.4" {
			t.Fatalf("opts.Model = %q, want %q", opts.Model, "gpt-5.4")
		}
		return errors.New("mock error")
	}

	cmd.SetArgs([]string{"my-provider", "--model", "gpt-5.4"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !called {
		t.Fatal("expected runUseCommand called")
	}
}

func TestDefaultUseCommandRunner(t *testing.T) {
	tests := []struct {
		name           string
		provider       string
		model          string
		selectProvider func(context.Context, string) (configstate.Selection, error)
		setCurrent     func(context.Context, string) (configstate.Selection, error)
		wantErr        string
		wantOutput     []string
		wantSetCalled  bool
	}{
		{
			name:     "switch provider only",
			provider: "openai",
			selectProvider: func(_ context.Context, provider string) (configstate.Selection, error) {
				return configstate.Selection{ProviderID: provider, ModelID: "gpt-5.4"}, nil
			},
			wantOutput: []string{"已全局切换到供应商: openai"},
		},
		{
			name:     "switch provider and model",
			provider: "openai",
			model:    "gpt-4o",
			selectProvider: func(_ context.Context, provider string) (configstate.Selection, error) {
				return configstate.Selection{ProviderID: provider, ModelID: "gpt-5.4"}, nil
			},
			setCurrent: func(_ context.Context, model string) (configstate.Selection, error) {
				return configstate.Selection{ProviderID: "openai", ModelID: model}, nil
			},
			wantSetCalled: true,
			wantOutput:    []string{"已全局切换到供应商: openai", "已设置模型: gpt-4o"},
		},
		{
			name:     "select provider error",
			provider: "missing",
			selectProvider: func(_ context.Context, provider string) (configstate.Selection, error) {
				return configstate.Selection{}, errors.New("provider not found")
			},
			wantErr: "provider not found",
		},
		{
			name:     "model not found",
			provider: "openai",
			model:    "missing-model",
			selectProvider: func(_ context.Context, provider string) (configstate.Selection, error) {
				return configstate.Selection{ProviderID: provider, ModelID: "gpt-5.4"}, nil
			},
			setCurrent: func(_ context.Context, model string) (configstate.Selection, error) {
				return configstate.Selection{}, configstate.ErrModelNotFound
			},
			wantSetCalled: true,
			wantErr:       `provider "openai" has no model "missing-model"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output := &bytes.Buffer{}
			cmd := &cobra.Command{}
			cmd.SetOut(output)
			cmd.SetContext(context.Background())

			setCalled := false
			svc := &mockSelectionService{
				selectProviderFn: tc.selectProvider,
				setCurrentModelFn: func(ctx context.Context, modelID string) (configstate.Selection, error) {
					setCalled = true
					if tc.setCurrent != nil {
						return tc.setCurrent(ctx, modelID)
					}
					return configstate.Selection{}, nil
				},
			}

			err := defaultUseCommandRunner(cmd, svc, tc.provider, useCommandOptions{Model: tc.model})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want contains %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("defaultUseCommandRunner() error = %v", err)
			}
			if setCalled != tc.wantSetCalled {
				t.Fatalf("setCalled = %v, want %v", setCalled, tc.wantSetCalled)
			}
			for _, fragment := range tc.wantOutput {
				if !strings.Contains(output.String(), fragment) {
					t.Fatalf("output = %q, want contains %q", output.String(), fragment)
				}
			}
		})
	}
}
