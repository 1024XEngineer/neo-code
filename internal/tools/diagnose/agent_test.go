package diagnose

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"neo-code/internal/subagent"
	"neo-code/internal/tools"
)

type stubDiagnoseSubAgentInvoker struct {
	runFunc func(ctx context.Context, input tools.SubAgentRunInput) (tools.SubAgentRunResult, error)
}

func (s stubDiagnoseSubAgentInvoker) Run(ctx context.Context, input tools.SubAgentRunInput) (tools.SubAgentRunResult, error) {
	if s.runFunc == nil {
		return tools.SubAgentRunResult{}, nil
	}
	return s.runFunc(ctx, input)
}

func TestDiagnoseToolSubAgentSuccess(t *testing.T) {
	tool := New()
	invoker := stubDiagnoseSubAgentInvoker{
		runFunc: func(_ context.Context, input tools.SubAgentRunInput) (tools.SubAgentRunResult, error) {
			if input.TaskID != diagnoseSubAgentTaskID {
				t.Fatalf("TaskID = %q, want %q", input.TaskID, diagnoseSubAgentTaskID)
			}
			return tools.SubAgentRunResult{
				State:      subagent.StateSucceeded,
				StopReason: subagent.StopReasonCompleted,
				StepCount:  2,
				Output: subagent.Output{
					Summary:     "missing dependency in go.mod",
					Findings:    []string{"confidence=0.83", "go.sum lacks package checksum"},
					Patches:     []string{"go get github.com/example/pkg@latest"},
					NextActions: []string{"go mod tidy", "go test ./..."},
				},
			}, nil
		},
	}

	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`{
			"error_log":"build failed",
			"os_env":{"os":"linux","shell":"/bin/bash","cwd":"/repo"},
			"command_text":"go test ./...",
			"exit_code":1
		}`),
		SubAgentInvoker: invoker,
		Workdir:         "/repo",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("result.IsError = true, want false")
	}
	var parsed diagnoseOutput
	if unmarshalErr := json.Unmarshal([]byte(result.Content), &parsed); unmarshalErr != nil {
		t.Fatalf("content should be diagnose JSON: %v", unmarshalErr)
	}
	if parsed.RootCause != "missing dependency in go.mod" {
		t.Fatalf("RootCause = %q, want %q", parsed.RootCause, "missing dependency in go.mod")
	}
	if parsed.Confidence != 0.83 {
		t.Fatalf("Confidence = %v, want 0.83", parsed.Confidence)
	}
	if len(parsed.FixCommands) == 0 || parsed.FixCommands[0] != "go get github.com/example/pkg@latest" {
		t.Fatalf("FixCommands = %#v", parsed.FixCommands)
	}
}

func TestDiagnoseToolSubAgentTimeoutFallback(t *testing.T) {
	tool := New()
	invoker := stubDiagnoseSubAgentInvoker{
		runFunc: func(_ context.Context, _ tools.SubAgentRunInput) (tools.SubAgentRunResult, error) {
			return tools.SubAgentRunResult{}, context.DeadlineExceeded
		},
	}

	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`{
			"error_log":"timeout while calling provider",
			"os_env":{"os":"linux","shell":"/bin/bash","cwd":"/repo"},
			"command_text":"curl https://example.com",
			"exit_code":28
		}`),
		SubAgentInvoker: invoker,
		Workdir:         "/repo",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil for graceful degrade", err)
	}
	if result.IsError {
		t.Fatalf("result.IsError = true, want graceful fallback")
	}
	var parsed diagnoseOutput
	if unmarshalErr := json.Unmarshal([]byte(result.Content), &parsed); unmarshalErr != nil {
		t.Fatalf("content should be diagnose JSON: %v", unmarshalErr)
	}
	if parsed.Confidence >= 0.5 {
		t.Fatalf("fallback confidence = %v, want low confidence", parsed.Confidence)
	}
	if len(parsed.InvestigationCommands) == 0 {
		t.Fatalf("fallback investigation should not be empty")
	}
}

func TestDiagnoseToolSubAgentDirtyOutputFallback(t *testing.T) {
	tool := New()
	invoker := stubDiagnoseSubAgentInvoker{
		runFunc: func(_ context.Context, _ tools.SubAgentRunInput) (tools.SubAgentRunResult, error) {
			return tools.SubAgentRunResult{
				State:      subagent.StateSucceeded,
				StopReason: subagent.StopReasonCompleted,
				Output: subagent.Output{
					Summary: "   ",
				},
			}, nil
		},
	}

	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`{
			"error_log":"garbled provider output",
			"os_env":{"os":"linux","shell":"/bin/bash"},
			"command_text":"make build",
			"exit_code":2
		}`),
		SubAgentInvoker: invoker,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil for graceful degrade", err)
	}
	var parsed diagnoseOutput
	if unmarshalErr := json.Unmarshal([]byte(result.Content), &parsed); unmarshalErr != nil {
		t.Fatalf("content should be diagnose JSON: %v", unmarshalErr)
	}
	if parsed.RootCause == "" {
		t.Fatal("fallback root_cause should not be empty")
	}
}

func TestDiagnoseToolSubAgentFailedStateFallback(t *testing.T) {
	tool := New()
	invoker := stubDiagnoseSubAgentInvoker{
		runFunc: func(_ context.Context, _ tools.SubAgentRunInput) (tools.SubAgentRunResult, error) {
			return tools.SubAgentRunResult{
				State:      subagent.StateFailed,
				StopReason: subagent.StopReasonError,
				Error:      "provider disconnected",
			}, nil
		},
	}

	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`{
			"error_log":"provider disconnected",
			"os_env":{"os":"linux","shell":"/bin/bash"},
			"command_text":"npm run build",
			"exit_code":1
		}`),
		SubAgentInvoker: invoker,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v, want graceful fallback", err)
	}
	var parsed diagnoseOutput
	if unmarshalErr := json.Unmarshal([]byte(result.Content), &parsed); unmarshalErr != nil {
		t.Fatalf("content should be diagnose JSON: %v", unmarshalErr)
	}
	if parsed.RootCause == "" {
		t.Fatal("fallback root_cause should not be empty")
	}
}

func TestParseSubAgentDiagnosisRejectsEmptySummary(t *testing.T) {
	_, err := parseSubAgentDiagnosis(subagent.Output{Summary: ""})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !errors.Is(err, errors.New("empty summary")) && err.Error() != "empty summary" {
		t.Fatalf("err = %v", err)
	}
}

func TestParseSubAgentDiagnosisFromSummaryJSON(t *testing.T) {
	parsed, err := parseSubAgentDiagnosis(subagent.Output{
		Summary: `{"confidence":1.4,"root_cause":"disk full","fix_commands":["  rm -rf /tmp/cache  "],"investigation_commands":[]}`,
	})
	if err != nil {
		t.Fatalf("parseSubAgentDiagnosis() error = %v", err)
	}
	if parsed.RootCause != "disk full" {
		t.Fatalf("RootCause = %q, want disk full", parsed.RootCause)
	}
	if parsed.Confidence != 1 {
		t.Fatalf("Confidence = %v, want 1", parsed.Confidence)
	}
	if len(parsed.FixCommands) != 1 || parsed.FixCommands[0] != "rm -rf /tmp/cache" {
		t.Fatalf("FixCommands = %#v", parsed.FixCommands)
	}
}

func TestParseSubAgentDiagnosisFallbacks(t *testing.T) {
	parsed, err := parseSubAgentDiagnosis(subagent.Output{
		Summary:  "network timeout",
		Findings: []string{"  run: ping 1.1.1.1  ", "run: ping 1.1.1.1"},
		Patches:  []string{"  export HTTPS_PROXY=http://127.0.0.1:7890  "},
	})
	if err != nil {
		t.Fatalf("parseSubAgentDiagnosis() error = %v", err)
	}
	if !strings.Contains(parsed.RootCause, "network timeout") {
		t.Fatalf("RootCause = %q", parsed.RootCause)
	}
	if parsed.Confidence != 0.56 {
		t.Fatalf("Confidence = %v, want 0.56 fallback", parsed.Confidence)
	}
	if len(parsed.InvestigationCommands) != 1 || parsed.InvestigationCommands[0] != "run: ping 1.1.1.1" {
		t.Fatalf("InvestigationCommands = %#v", parsed.InvestigationCommands)
	}
	if len(parsed.FixCommands) != 1 || parsed.FixCommands[0] != "export HTTPS_PROXY=http://127.0.0.1:7890" {
		t.Fatalf("FixCommands = %#v", parsed.FixCommands)
	}
}
