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

func TestModelCommand(t *testing.T) {
	cmd := newModelCommand()
	if cmd.Use != "model" {
		t.Fatalf("cmd.Use = %q, want %q", cmd.Use, "model")
	}
}

func TestModelCommandBuildersUseDefaultResolverWhenNil(t *testing.T) {
	cmd := newModelCommandWithResolver(nil)
	if cmd.Use != "model" {
		t.Fatalf("cmd.Use = %q, want %q", cmd.Use, "model")
	}
	if len(cmd.Commands()) != 2 {
		t.Fatalf("len(model subcommands) = %d, want 2", len(cmd.Commands()))
	}

	ls := newModelLsCommand()
	if ls.Use != "ls" {
		t.Fatalf("ls.Use = %q, want %q", ls.Use, "ls")
	}

	set := newModelSetCommand()
	if set.Use != "set <model-id>" {
		t.Fatalf("set.Use = %q, want %q", set.Use, "set <model-id>")
	}
}

func TestModelLsCommand(t *testing.T) {
	svc := &mockSelectionService{}
	cmd := newModelLsCommandWithResolver(staticSelectionResolver(svc))
	if cmd.Use != "ls" {
		t.Fatalf("cmd.Use = %q, want %q", cmd.Use, "ls")
	}

	originalRunner := runModelLsCommand
	t.Cleanup(func() { runModelLsCommand = originalRunner })

	called := false
	runModelLsCommand = func(c *cobra.Command, gotSvc SelectionService) error {
		called = true
		if gotSvc != svc {
			t.Fatalf("injected service mismatch")
		}
		return errors.New("mock error")
	}

	cmd.SetArgs([]string{})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !called {
		t.Fatal("expected runModelLsCommand called")
	}
}

func TestModelSetCommand(t *testing.T) {
	svc := &mockSelectionService{}
	cmd := newModelSetCommandWithResolver(staticSelectionResolver(svc))
	if cmd.Use != "set <model-id>" {
		t.Fatalf("cmd.Use = %q, want %q", cmd.Use, "set <model-id>")
	}

	originalRunner := runModelSetCommand
	t.Cleanup(func() { runModelSetCommand = originalRunner })

	called := false
	runModelSetCommand = func(c *cobra.Command, gotSvc SelectionService, modelID string) error {
		called = true
		if gotSvc != svc {
			t.Fatalf("injected service mismatch")
		}
		if modelID != "gpt-5.4" {
			t.Fatalf("modelID = %q, want %q", modelID, "gpt-5.4")
		}
		return errors.New("mock error")
	}

	cmd.SetArgs([]string{"gpt-5.4"})
	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !called {
		t.Fatal("expected runModelSetCommand called")
	}
}

func TestModelCommandResolverFallbackAndResolveError(t *testing.T) {
	t.Run("fallback to default resolver when nil", func(t *testing.T) {
		ls := newModelLsCommandWithResolver(nil)
		if ls.Use != "ls" {
			t.Fatalf("ls.Use = %q, want ls", ls.Use)
		}

		set := newModelSetCommandWithResolver(nil)
		if set.Use != "set <model-id>" {
			t.Fatalf("set.Use = %q, want set <model-id>", set.Use)
		}
	})

	t.Run("ls resolve error", func(t *testing.T) {
		cmd := newModelLsCommandWithResolver(selectionServiceResolverFunc(func(*cobra.Command) (SelectionService, error) {
			return nil, errors.New("resolve failed")
		}))
		cmd.SetArgs([]string{})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("err = %v, want contains resolve failed", err)
		}
	})

	t.Run("set resolve error", func(t *testing.T) {
		cmd := newModelSetCommandWithResolver(selectionServiceResolverFunc(func(*cobra.Command) (SelectionService, error) {
			return nil, errors.New("resolve failed")
		}))
		cmd.SetArgs([]string{"gpt-4o"})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || !strings.Contains(err.Error(), "resolve failed") {
			t.Fatalf("err = %v, want contains resolve failed", err)
		}
	})
}

func TestDefaultModelLsCommandRunner(t *testing.T) {
	tests := []struct {
		name           string
		currentModel   string
		snapshotModels []providertypes.ModelDescriptor
		snapshotErr    error
		listModels     []providertypes.ModelDescriptor
		listErr        error
		wantOutput     []string
		wantErr        string
		wantListCalled bool
	}{
		{
			name:         "snapshot models with current marker",
			currentModel: "gpt-5.4",
			snapshotModels: []providertypes.ModelDescriptor{
				{ID: "gpt-5.4", Name: "GPT-5.4"},
				{ID: "gpt-4o"},
			},
			wantOutput: []string{
				"供应商: openai",
				"* gpt-5.4 (GPT-5.4)",
				"gpt-4o",
			},
		},
		{
			name:           "fallback to list models when snapshot empty",
			currentModel:   "gpt-4o",
			snapshotModels: []providertypes.ModelDescriptor{},
			listModels: []providertypes.ModelDescriptor{
				{ID: "gpt-4o", Name: "GPT-4o"},
			},
			wantOutput:     []string{"* gpt-4o (GPT-4o)"},
			wantListCalled: true,
		},
		{
			name:           "both snapshot and list empty",
			currentModel:   "gpt-4o",
			snapshotModels: []providertypes.ModelDescriptor{},
			listModels:     []providertypes.ModelDescriptor{},
			wantOutput:     []string{"无可用模型，该供应商使用动态发现"},
			wantListCalled: true,
		},
		{
			name:         "snapshot error",
			currentModel: "gpt-4o",
			snapshotErr:  errors.New("snapshot unavailable"),
			wantErr:      "snapshot unavailable",
		},
		{
			name:           "list error after snapshot miss",
			currentModel:   "gpt-4o",
			snapshotModels: []providertypes.ModelDescriptor{},
			listErr:        errors.New("catalog unavailable"),
			wantErr:        "catalog unavailable",
			wantListCalled: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workdir := prepareModelSelectionConfig(t, tc.currentModel)
			cmd := &cobra.Command{}
			cmd.Flags().String("workdir", workdir, "")
			output := &bytes.Buffer{}
			cmd.SetOut(output)
			cmd.SetContext(context.Background())

			listCalled := false
			svc := &mockSelectionService{
				listModelsSnapshotFn: func(context.Context) ([]providertypes.ModelDescriptor, error) {
					return tc.snapshotModels, tc.snapshotErr
				},
				listModelsFn: func(context.Context) ([]providertypes.ModelDescriptor, error) {
					listCalled = true
					return tc.listModels, tc.listErr
				},
			}

			err := defaultModelLsCommandRunner(cmd, svc)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want contains %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("defaultModelLsCommandRunner() error = %v", err)
			}
			if listCalled != tc.wantListCalled {
				t.Fatalf("listCalled = %v, want %v", listCalled, tc.wantListCalled)
			}
			for _, fragment := range tc.wantOutput {
				if !strings.Contains(output.String(), fragment) {
					t.Fatalf("output = %q, want contains %q", output.String(), fragment)
				}
			}
		})
	}
}

func TestDefaultModelSetCommandRunner(t *testing.T) {
	workdir := prepareModelSelectionConfig(t, "gpt-5.4")

	tests := []struct {
		name       string
		modelID    string
		service    SelectionService
		wantErr    string
		wantOutput string
	}{
		{
			name:    "switch model success",
			modelID: "gpt-4o",
			service: &mockSelectionService{
				setCurrentModelFn: func(_ context.Context, modelID string) (configstate.Selection, error) {
					return configstate.Selection{ProviderID: "openai", ModelID: modelID}, nil
				},
			},
			wantOutput: "已切换模型: gpt-4o",
		},
		{
			name:    "empty model id",
			modelID: "  ",
			service: &mockSelectionService{
				setCurrentModelFn: func(_ context.Context, modelID string) (configstate.Selection, error) {
					return configstate.Selection{}, nil
				},
			},
			wantErr: "模型 ID 不能为空",
		},
		{
			name:    "model not found",
			modelID: "missing",
			service: &mockSelectionService{
				setCurrentModelFn: func(_ context.Context, modelID string) (configstate.Selection, error) {
					return configstate.Selection{}, configstate.ErrModelNotFound
				},
			},
			wantErr: `provider "openai" has no model "missing"`,
		},
		{
			name:    "service error",
			modelID: "gpt-4o",
			service: &mockSelectionService{
				setCurrentModelFn: func(_ context.Context, modelID string) (configstate.Selection, error) {
					return configstate.Selection{}, errors.New("catalog down")
				},
			},
			wantErr: "catalog down",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().String("workdir", workdir, "")
			output := &bytes.Buffer{}
			cmd.SetOut(output)
			cmd.SetContext(context.Background())

			err := defaultModelSetCommandRunner(cmd, tc.service, tc.modelID)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want contains %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("defaultModelSetCommandRunner() error = %v", err)
			}
			if !strings.Contains(output.String(), tc.wantOutput) {
				t.Fatalf("output = %q, want contains %q", output.String(), tc.wantOutput)
			}
		})
	}
}

func TestDisplayCurrentModel(t *testing.T) {
	if got := displayCurrentModel(""); !strings.Contains(got, "未设置") {
		t.Fatalf("displayCurrentModel(\"\") = %q, want contains 未设置", got)
	}
	if got := displayCurrentModel("gpt-5.4"); got != "gpt-5.4" {
		t.Fatalf("displayCurrentModel() = %q, want %q", got, "gpt-5.4")
	}
}

func TestModelCommandRunnerErrorBranches(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	if err := defaultModelLsCommandRunner(cmd, nil); err == nil || !strings.Contains(err.Error(), "selection service") {
		t.Fatalf("defaultModelLsCommandRunner(nil) err = %v", err)
	}
	if err := defaultModelSetCommandRunner(cmd, nil, "gpt-4o"); err == nil || !strings.Contains(err.Error(), "selection service") {
		t.Fatalf("defaultModelSetCommandRunner(nil) err = %v", err)
	}
}

func TestDefaultModelLsCommandRunnerSelectionConfigErrors(t *testing.T) {
	t.Run("no selected provider", func(t *testing.T) {
		workdir := prepareModelSelectionConfig(t, "gpt-5.4")
		loader := config.NewLoader(workdir, config.StaticDefaults())
		manager := config.NewManager(loader)
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("manager.Load() error = %v", err)
		}
		if err := manager.Update(context.Background(), func(cfg *config.Config) error {
			cfg.SelectedProvider = ""
			cfg.CurrentModel = ""
			return nil
		}); err != nil {
			t.Fatalf("manager.Update() error = %v", err)
		}

		cmd := &cobra.Command{}
		cmd.Flags().String("workdir", workdir, "")
		cmd.SetContext(context.Background())
		err := defaultModelLsCommandRunner(cmd, &mockSelectionService{})
		if err == nil || !strings.Contains(err.Error(), "neocode use") {
			t.Fatalf("err = %v, want contains neocode use", err)
		}
	})

	t.Run("selected provider missing in config", func(t *testing.T) {
		workdir := prepareModelSelectionConfig(t, "gpt-5.4")
		loader := config.NewLoader(workdir, config.StaticDefaults())
		manager := config.NewManager(loader)
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("manager.Load() error = %v", err)
		}
		if err := manager.Update(context.Background(), func(cfg *config.Config) error {
			cfg.SelectedProvider = "missing-provider"
			return nil
		}); err != nil {
			t.Fatalf("manager.Update() error = %v", err)
		}

		cmd := &cobra.Command{}
		cmd.Flags().String("workdir", workdir, "")
		cmd.SetContext(context.Background())
		err := defaultModelLsCommandRunner(cmd, &mockSelectionService{})
		if err == nil || !strings.Contains(err.Error(), "missing-provider") {
			t.Fatalf("err = %v, want contains missing-provider", err)
		}
	})
}

func TestDefaultModelSetCommandRunnerAdditionalBranches(t *testing.T) {
	t.Run("model not found and selected provider empty", func(t *testing.T) {
		workdir := prepareModelSelectionConfig(t, "gpt-5.4")
		loader := config.NewLoader(workdir, config.StaticDefaults())
		manager := config.NewManager(loader)
		if _, err := manager.Load(context.Background()); err != nil {
			t.Fatalf("manager.Load() error = %v", err)
		}
		if err := manager.Update(context.Background(), func(cfg *config.Config) error {
			cfg.SelectedProvider = ""
			return nil
		}); err != nil {
			t.Fatalf("manager.Update() error = %v", err)
		}

		cmd := &cobra.Command{}
		cmd.Flags().String("workdir", workdir, "")
		cmd.SetContext(context.Background())
		svc := &mockSelectionService{
			setCurrentModelFn: func(context.Context, string) (configstate.Selection, error) {
				return configstate.Selection{}, configstate.ErrModelNotFound
			},
		}
		err := defaultModelSetCommandRunner(cmd, svc, "missing-model")
		if err == nil || !strings.Contains(err.Error(), `provider has no model "missing-model"`) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("set current model success with empty selection model", func(t *testing.T) {
		workdir := prepareModelSelectionConfig(t, "gpt-5.4")
		cmd := &cobra.Command{}
		cmd.Flags().String("workdir", workdir, "")
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetContext(context.Background())

		svc := &mockSelectionService{
			setCurrentModelFn: func(_ context.Context, _ string) (configstate.Selection, error) {
				return configstate.Selection{ProviderID: "openai", ModelID: ""}, nil
			},
		}
		if err := defaultModelSetCommandRunner(cmd, svc, "gpt-4o"); err != nil {
			t.Fatalf("defaultModelSetCommandRunner() error = %v", err)
		}
		if !strings.Contains(out.String(), "gpt-4o") {
			t.Fatalf("output = %q, want contains gpt-4o", out.String())
		}
	})
}

func prepareModelSelectionConfig(t *testing.T, currentModel string) string {
	t.Helper()
	workdir := t.TempDir()
	loader := config.NewLoader(workdir, config.StaticDefaults())
	manager := config.NewManager(loader)
	if _, err := manager.Load(context.Background()); err != nil {
		t.Fatalf("manager.Load() error = %v", err)
	}
	if err := manager.Update(context.Background(), func(cfg *config.Config) error {
		cfg.SelectedProvider = "openai"
		cfg.CurrentModel = strings.TrimSpace(currentModel)
		return nil
	}); err != nil {
		t.Fatalf("manager.Update() error = %v", err)
	}
	return workdir
}
