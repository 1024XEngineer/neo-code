package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"neo-code/internal/config"
	configstate "neo-code/internal/config/state"
	providertypes "neo-code/internal/provider/types"
)

func TestCLICoverageGapBranches(t *testing.T) {
	t.Run("model ls provider lookup and display name branch", func(t *testing.T) {
		workdir := prepareModelSelectionConfig(t, "m-1")
		cmd := &cobra.Command{}
		cmd.Flags().String("workdir", workdir, "")
		cmd.SetContext(context.Background())
		out := &bytes.Buffer{}
		cmd.SetOut(out)

		svc := &mockSelectionService{
			listModelsSnapshotFn: func(context.Context) ([]providertypes.ModelDescriptor, error) {
				return []providertypes.ModelDescriptor{
					{ID: "m-1", Name: "Model One"},
				}, nil
			},
		}
		if err := defaultModelLsCommandRunner(cmd, svc); err != nil {
			t.Fatalf("defaultModelLsCommandRunner() error = %v", err)
		}
		if !strings.Contains(out.String(), "Model One") {
			t.Fatalf("output = %q, want contains model display name", out.String())
		}
	})

	t.Run("model ls selected provider missing", func(t *testing.T) {
		workdir := prepareModelSelectionConfig(t, "m-1")
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

		svc := &mockSelectionService{}
		err := defaultModelLsCommandRunner(cmd, svc)
		if err == nil {
			t.Fatal("expected selected provider missing error")
		}
	})

	t.Run("model set returns underlying non-model-not-found error", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		svc := &mockSelectionService{
			setCurrentModelFn: func(context.Context, string) (configstate.Selection, error) {
				return configstate.Selection{}, errors.New("set model failed")
			},
		}
		err := defaultModelSetCommandRunner(cmd, svc, "m-1")
		if err == nil || !strings.Contains(err.Error(), "set model failed") {
			t.Fatalf("err = %v, want contains set model failed", err)
		}
	})

	t.Run("provider ls formatting loop branch", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		svc := &mockSelectionService{
			listProviderOptionsFn: func(context.Context) ([]configstate.ProviderOption, error) {
				return []configstate.ProviderOption{
					{
						ID:     "p1",
						Name:   "p1",
						Driver: "openaicompat",
						Source: "builtin",
						Models: []providertypes.ModelDescriptor{{ID: "m1"}, {ID: "m2"}},
					},
				}, nil
			},
		}
		if err := defaultProviderLsCommandRunner(cmd, svc); err != nil {
			t.Fatalf("defaultProviderLsCommandRunner() error = %v", err)
		}
		if !strings.Contains(out.String(), "Models: 2") {
			t.Fatalf("output = %q, want contains model count", out.String())
		}
	})

	t.Run("use command whitespace model routes to SelectProvider", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		out := &bytes.Buffer{}
		cmd.SetOut(out)

		selectProviderCalled := false
		svc := &mockSelectionService{
			selectProviderFn: func(_ context.Context, provider string) (configstate.Selection, error) {
				selectProviderCalled = true
				return configstate.Selection{ProviderID: provider}, nil
			},
			selectProviderModelFn: func(context.Context, string, string) (configstate.Selection, error) {
				return configstate.Selection{}, errors.New("unexpected SelectProviderWithModel call")
			},
		}
		if err := defaultUseCommandRunner(cmd, svc, "openai", useCommandOptions{Model: "   "}); err != nil {
			t.Fatalf("defaultUseCommandRunner() error = %v", err)
		}
		if !selectProviderCalled {
			t.Fatal("expected SelectProvider to be called")
		}
	})

	t.Run("diag auto command struct literal branch", func(t *testing.T) {
		originalRunner := runDiagAutoCommand
		t.Cleanup(func() { runDiagAutoCommand = originalRunner })

		var captured diagAutoCommandOptions
		runDiagAutoCommand = func(_ context.Context, options diagAutoCommandOptions, _ io.Writer) error {
			captured = options
			return nil
		}

		cmd := newDiagAutoCommand()
		cmd.SetArgs([]string{"on", "--socket", "/tmp/diag.sock"})
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("ExecuteContext() error = %v", err)
		}
		if captured.SocketPath != "/tmp/diag.sock" || !captured.Enabled {
			t.Fatalf("captured = %+v, want socket=/tmp/diag.sock enabled=true", captured)
		}
	})

	t.Run("diag diagnose command socket flag registration", func(t *testing.T) {
		cmd := newDiagDiagnoseCommand()
		flag := cmd.Flags().Lookup("socket")
		if flag == nil {
			t.Fatal("expected --socket flag on diagnose subcommand")
		}
	})

	t.Run("diag auto runner send error branch", func(t *testing.T) {
		originalSend := sendAutoModeSignalFn
		t.Cleanup(func() { sendAutoModeSignalFn = originalSend })

		sendAutoModeSignalFn = func(context.Context, string, bool) error {
			return errors.New("send auto failed")
		}
		err := defaultDiagAutoCommandRunner(context.Background(), diagAutoCommandOptions{
			SocketPath: "/tmp/diag.sock",
			Enabled:    true,
		}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "send auto failed") {
			t.Fatalf("err = %v, want contains send auto failed", err)
		}
	})
}
