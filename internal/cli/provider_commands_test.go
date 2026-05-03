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
)

func TestProviderCommand(t *testing.T) {
	cmd := newProviderCommand()
	if cmd.Use != "provider" {
		t.Fatalf("cmd.Use = %q, want %q", cmd.Use, "provider")
	}
}

func TestProviderAddCommand(t *testing.T) {
	svc := &mockSelectionService{}
	cmd := newProviderAddCommandWithResolver(staticSelectionResolver(svc))
	if cmd.Use != "add <name>" {
		t.Fatalf("cmd.Use = %q, want %q", cmd.Use, "add <name>")
	}

	originalRunner := runProviderAddCommand
	t.Cleanup(func() { runProviderAddCommand = originalRunner })

	called := false
	runProviderAddCommand = func(c *cobra.Command, gotSvc SelectionService, name string, opts providerAddOptions) error {
		called = true
		if gotSvc != svc {
			t.Fatalf("injected service mismatch")
		}
		if name != "my-provider" {
			t.Fatalf("name = %q, want %q", name, "my-provider")
		}
		if opts.Driver != "openaicompat" {
			t.Fatalf("opts.Driver = %q, want %q", opts.Driver, "openaicompat")
		}
		return errors.New("mock error")
	}

	cmd.SetArgs([]string{"my-provider", "--driver", "openaicompat", "--url", "http://mock", "--api-key-env", "MOCK_KEY"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !called {
		t.Fatal("expected runProviderAddCommand called")
	}
}

func TestProviderLsCommand(t *testing.T) {
	svc := &mockSelectionService{}
	cmd := newProviderLsCommandWithResolver(staticSelectionResolver(svc))
	if cmd.Use != "ls" {
		t.Fatalf("cmd.Use = %q, want %q", cmd.Use, "ls")
	}

	originalRunner := runProviderLsCommand
	t.Cleanup(func() { runProviderLsCommand = originalRunner })

	called := false
	runProviderLsCommand = func(c *cobra.Command, gotSvc SelectionService) error {
		called = true
		if gotSvc != svc {
			t.Fatalf("injected service mismatch")
		}
		return errors.New("mock error")
	}

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !called {
		t.Fatal("expected runProviderLsCommand called")
	}
}

func TestProviderRmCommand(t *testing.T) {
	svc := &mockSelectionService{}
	cmd := newProviderRmCommandWithResolver(staticSelectionResolver(svc))
	if cmd.Use != "rm <name>" {
		t.Fatalf("cmd.Use = %q, want %q", cmd.Use, "rm <name>")
	}

	originalRunner := runProviderRmCommand
	t.Cleanup(func() { runProviderRmCommand = originalRunner })

	called := false
	runProviderRmCommand = func(c *cobra.Command, gotSvc SelectionService, name string) error {
		called = true
		if gotSvc != svc {
			t.Fatalf("injected service mismatch")
		}
		if name != "my-provider" {
			t.Fatalf("name = %q, want %q", name, "my-provider")
		}
		return errors.New("mock error")
	}

	cmd.SetArgs([]string{"my-provider"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !called {
		t.Fatal("expected runProviderRmCommand called")
	}
}

func TestDefaultProviderAddCommandRunner(t *testing.T) {
	tests := []struct {
		name        string
		envName     string
		envValue    string
		opts        providerAddOptions
		service     SelectionService
		wantErr     string
		wantOutput  string
		assertInput func(t *testing.T, input configstate.CreateCustomProviderInput)
	}{
		{
			name:    "missing api key env value",
			envName: "MOCK_KEY_EMPTY",
			opts: providerAddOptions{
				Driver:    "openaicompat",
				URL:       "http://mock",
				APIKeyEnv: "MOCK_KEY_EMPTY",
			},
			service: &mockSelectionService{},
			wantErr: "请先设置 $MOCK_KEY_EMPTY 环境变量",
		},
		{
			name:     "create provider success with default discovery path",
			envName:  "MOCK_KEY_OK",
			envValue: "sk-test",
			opts: providerAddOptions{
				Driver:    "openaicompat",
				URL:       "http://mock",
				APIKeyEnv: "MOCK_KEY_OK",
			},
			service: &mockSelectionService{
				createCustomProviderFn: func(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error) {
					return configstate.Selection{ProviderID: input.Name, ModelID: "m-1"}, nil
				},
			},
			wantOutput: "提供商 my-provider 添加成功，当前模型: m-1",
			assertInput: func(t *testing.T, input configstate.CreateCustomProviderInput) {
				t.Helper()
				if input.Driver != "openaicompat" {
					t.Fatalf("input.Driver = %q, want openaicompat", input.Driver)
				}
				if input.APIKey != "sk-test" {
					t.Fatalf("input.APIKey = %q, want sk-test", input.APIKey)
				}
				if input.DiscoveryEndpointPath != "/v1/models" {
					t.Fatalf("input.DiscoveryEndpointPath = %q, want /v1/models", input.DiscoveryEndpointPath)
				}
			},
		},
		{
			name:     "service error",
			envName:  "MOCK_KEY_ERR",
			envValue: "sk-test",
			opts: providerAddOptions{
				Driver:                "openaicompat",
				URL:                   "http://mock",
				APIKeyEnv:             "MOCK_KEY_ERR",
				DiscoveryEndpointPath: "/custom/models",
			},
			service: &mockSelectionService{
				createCustomProviderFn: func(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error) {
					return configstate.Selection{}, errors.New("conflict")
				},
			},
			wantErr: "conflict",
		},
		{
			name:     "timeout error",
			envName:  "MOCK_KEY_TIMEOUT",
			envValue: "sk-test",
			opts: providerAddOptions{
				Driver:    "openaicompat",
				URL:       "http://mock",
				APIKeyEnv: "MOCK_KEY_TIMEOUT",
			},
			service: &mockSelectionService{
				createCustomProviderFn: func(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error) {
					return configstate.Selection{}, context.DeadlineExceeded
				},
			},
			wantErr: "provider add 超时",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envName != "" {
				t.Setenv(tc.envName, tc.envValue)
			}
			cmd := &cobra.Command{}
			output := &bytes.Buffer{}
			cmd.SetOut(output)
			cmd.SetContext(context.Background())

			var capturedInput configstate.CreateCustomProviderInput
			svc := tc.service
			if mocked, ok := svc.(*mockSelectionService); ok && mocked.createCustomProviderFn != nil {
				originalFn := mocked.createCustomProviderFn
				mocked.createCustomProviderFn = func(ctx context.Context, input configstate.CreateCustomProviderInput) (configstate.Selection, error) {
					capturedInput = input
					return originalFn(ctx, input)
				}
			}

			err := defaultProviderAddCommandRunner(cmd, svc, "my-provider", tc.opts)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want contains %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("defaultProviderAddCommandRunner() error = %v", err)
			}
			if tc.assertInput != nil {
				tc.assertInput(t, capturedInput)
			}
			if !strings.Contains(output.String(), tc.wantOutput) {
				t.Fatalf("output = %q, want contains %q", output.String(), tc.wantOutput)
			}
		})
	}
}

func TestDefaultProviderLsAndRmCommandRunner(t *testing.T) {
	workdir := t.TempDir()
	cmd := &cobra.Command{}
	cmd.Flags().String("workdir", workdir, "")
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	cmd.SetContext(context.Background())

	svc := &mockSelectionService{
		listProviderOptionsFn: func(ctx context.Context) ([]configstate.ProviderOption, error) {
			return []configstate.ProviderOption{
				{
					ID:     "my-provider",
					Name:   "my-provider",
					Driver: "openaicompat",
					Source: string(config.ProviderSourceCustom),
				},
			}, nil
		},
	}

	if err := defaultProviderLsCommandRunner(cmd, svc); err != nil {
		t.Fatalf("defaultProviderLsCommandRunner() error = %v", err)
	}
	if !strings.Contains(output.String(), "my-provider") {
		t.Fatalf("output = %q, want contains my-provider", output.String())
	}

	var removed string
	svc = &mockSelectionService{
		removeCustomProviderFn: func(ctx context.Context, name string) error {
			removed = name
			return nil
		},
	}
	if err := defaultProviderRmCommandRunner(cmd, svc, "my-provider"); err != nil {
		t.Fatalf("defaultProviderRmCommandRunner() error = %v", err)
	}
	if removed != "my-provider" {
		t.Fatalf("removed = %q, want my-provider", removed)
	}
	if !strings.Contains(output.String(), "提供商 my-provider 已删除") {
		t.Fatalf("output = %q, want contains remove message", output.String())
	}
}
