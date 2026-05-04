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

func TestUseCommandBuilderUsesDefaultResolverWhenNil(t *testing.T) {
	cmd := newUseCommand()
	if cmd.Use != "use <provider>" {
		t.Fatalf("cmd.Use = %q, want %q", cmd.Use, "use <provider>")
	}

	injected := newUseCommandWithResolver(nil)
	if injected.Use != "use <provider>" {
		t.Fatalf("injected.Use = %q, want %q", injected.Use, "use <provider>")
	}
}

func TestDefaultUseCommandRunner(t *testing.T) {
	tests := []struct {
		name                    string
		provider                string
		model                   string
		selectProvider          func(context.Context, string) (configstate.Selection, error)
		selectProviderWithModel func(context.Context, string, string) (configstate.Selection, error)
		wantErr                 string
		wantOutput              []string
		wantSelectWithModel     bool
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
			selectProviderWithModel: func(_ context.Context, provider string, model string) (configstate.Selection, error) {
				return configstate.Selection{ProviderID: provider, ModelID: model}, nil
			},
			wantSelectWithModel: true,
			wantOutput:          []string{"已全局切换到供应商: openai", "已设置模型: gpt-4o"},
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
			selectProviderWithModel: func(_ context.Context, provider string, model string) (configstate.Selection, error) {
				return configstate.Selection{}, configstate.ErrModelNotFound
			},
			wantSelectWithModel: true,
			wantErr:             `provider "openai" has no model "missing-model"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output := &bytes.Buffer{}
			cmd := &cobra.Command{}
			cmd.SetOut(output)
			cmd.SetContext(context.Background())

			selectWithModelCalled := false
			svc := &mockSelectionService{
				selectProviderFn: tc.selectProvider,
				selectProviderModelFn: func(ctx context.Context, providerName string, modelID string) (configstate.Selection, error) {
					selectWithModelCalled = true
					if tc.selectProviderWithModel != nil {
						return tc.selectProviderWithModel(ctx, providerName, modelID)
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
			if selectWithModelCalled != tc.wantSelectWithModel {
				t.Fatalf("selectWithModelCalled = %v, want %v", selectWithModelCalled, tc.wantSelectWithModel)
			}
			for _, fragment := range tc.wantOutput {
				if !strings.Contains(output.String(), fragment) {
					t.Fatalf("output = %q, want contains %q", output.String(), fragment)
				}
			}
		})
	}
}

func TestDefaultUseCommandRunnerExtraBranches(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	if err := defaultUseCommandRunner(cmd, nil, "openai", useCommandOptions{}); err == nil || !strings.Contains(err.Error(), "selection service") {
		t.Fatalf("defaultUseCommandRunner(nil) err = %v", err)
	}

	output := &bytes.Buffer{}
	cmd.SetOut(output)
	svc := &mockSelectionService{
		selectProviderFn: func(context.Context, string) (configstate.Selection, error) {
			return configstate.Selection{ProviderID: "", ModelID: ""}, nil
		},
	}
	if err := defaultUseCommandRunner(cmd, svc, "custom-provider", useCommandOptions{}); err != nil {
		t.Fatalf("defaultUseCommandRunner() error = %v", err)
	}
	if !strings.Contains(output.String(), "custom-provider") {
		t.Fatalf("output = %q, want fallback provider name", output.String())
	}

	output.Reset()
	svc = &mockSelectionService{
		selectProviderModelFn: func(context.Context, string, string) (configstate.Selection, error) {
			return configstate.Selection{ProviderID: "openai", ModelID: ""}, nil
		},
	}
	if err := defaultUseCommandRunner(cmd, svc, "openai", useCommandOptions{Model: "gpt-4o"}); err != nil {
		t.Fatalf("defaultUseCommandRunner(with model) error = %v", err)
	}
	if !strings.Contains(output.String(), "gpt-4o") {
		t.Fatalf("output = %q, want fallback model id", output.String())
	}
}

func TestUseCommandExecuteWithoutModelInvokesRunner(t *testing.T) {
	svc := &mockSelectionService{}
	cmd := newUseCommandWithResolver(staticSelectionResolver(svc))

	originalRunner := runUseCommand
	t.Cleanup(func() { runUseCommand = originalRunner })

	called := false
	runUseCommand = func(c *cobra.Command, gotSvc SelectionService, name string, opts useCommandOptions) error {
		called = true
		if gotSvc != svc {
			t.Fatalf("injected service mismatch")
		}
		if name != "only-provider" {
			t.Fatalf("name = %q, want only-provider", name)
		}
		if opts.Model != "" {
			t.Fatalf("opts.Model = %q, want empty", opts.Model)
		}
		return errors.New("mock error")
	}

	cmd.SetArgs([]string{"only-provider"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "mock error") {
		t.Fatalf("err = %v, want mock error", err)
	}
	if !called {
		t.Fatal("expected runUseCommand called")
	}
}

func TestUseCommandResolverErrorAndWhitespaceModel(t *testing.T) {
	t.Run("resolver returns error", func(t *testing.T) {
		cmd := newUseCommandWithResolver(selectionServiceResolverFunc(func(*cobra.Command) (SelectionService, error) {
			return nil, errors.New("resolve failed")
		}))
		cmd.SetArgs([]string{"openai"})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("err = %v, want resolve failed", err)
		}
	})

	t.Run("whitespace model falls back to SelectProvider", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetContext(context.Background())

		selectProviderCalled := false
		svc := &mockSelectionService{
			selectProviderFn: func(_ context.Context, providerName string) (configstate.Selection, error) {
				selectProviderCalled = true
				return configstate.Selection{ProviderID: providerName}, nil
			},
		}
		err := defaultUseCommandRunner(cmd, svc, "openai", useCommandOptions{Model: "   "})
		if err != nil {
			t.Fatalf("defaultUseCommandRunner() error = %v", err)
		}
		if !selectProviderCalled {
			t.Fatal("expected SelectProvider path to be called")
		}
	})
}
