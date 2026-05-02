package diagnose

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"neo-code/internal/tools"
)

func TestToolMetadata(t *testing.T) {
	tool := New()
	if tool.Name() != tools.ToolNameDiagnose {
		t.Fatalf("Name() = %q, want %q", tool.Name(), tools.ToolNameDiagnose)
	}
	if strings.TrimSpace(tool.Description()) == "" {
		t.Fatal("Description() should not be empty")
	}
	if tool.Schema() == nil {
		t.Fatal("Schema() should not be nil")
	}
	if tool.MicroCompactPolicy() != tools.MicroCompactPolicyPreserveHistory {
		t.Fatalf("MicroCompactPolicy() = %q, want %q", tool.MicroCompactPolicy(), tools.MicroCompactPolicyPreserveHistory)
	}
}

func TestToolExecuteFallbackWhenInvokerUnavailable(t *testing.T) {
	tool := New()
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`{
			"error_log":"fatal: example",
			"os_env":{"os":"linux","shell":"/bin/bash","cwd":"/repo"},
			"command_text":"go test ./...",
			"exit_code":1
		}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("result.IsError = true, want false: %+v", result)
	}
	if result.Name != tools.ToolNameDiagnose {
		t.Fatalf("result.Name = %q, want %q", result.Name, tools.ToolNameDiagnose)
	}

	var decoded diagnoseOutput
	if unmarshalErr := json.Unmarshal([]byte(result.Content), &decoded); unmarshalErr != nil {
		t.Fatalf("content should be valid diagnose JSON, got err = %v", unmarshalErr)
	}
	if strings.TrimSpace(decoded.RootCause) == "" {
		t.Fatalf("root_cause should not be empty: %#v", decoded)
	}
	if len(decoded.InvestigationCommands) == 0 {
		t.Fatalf("investigation_commands should not be empty: %#v", decoded)
	}
	if degraded, ok := result.Metadata["degraded"].(bool); !ok || !degraded {
		t.Fatalf("metadata.degraded = %#v, want true", result.Metadata["degraded"])
	}
}

func TestToolExecuteValidationError(t *testing.T) {
	tool := New()
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`{"error_log":" ","os_env":{}}`),
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "error_log is required") {
		t.Fatalf("error = %v, want contains %q", err, "error_log is required")
	}
}

func TestToolExecuteInvalidJSON(t *testing.T) {
	tool := New()
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`{`),
	})
	if err == nil {
		t.Fatal("expected json error, got nil")
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true; result = %+v", result)
	}
	if !strings.Contains(err.Error(), "invalid arguments") {
		t.Fatalf("error = %v, want contains %q", err, "invalid arguments")
	}
}

func TestToolExecuteEmptyArguments(t *testing.T) {
	tool := New()
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(``),
	})
	if err == nil {
		t.Fatal("expected error for empty arguments")
	}
	if !strings.Contains(err.Error(), "error_log is required") {
		t.Fatalf("error = %v, want contains %q", err, "error_log is required")
	}
}

func TestToolExecuteNullArguments(t *testing.T) {
	tool := New()
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`null`),
	})
	if err == nil {
		t.Fatal("expected error for null arguments")
	}
	if !strings.Contains(err.Error(), "error_log is required") {
		t.Fatalf("error = %v, want contains %q", err, "error_log is required")
	}
}

func TestToolExecuteMissingOSEnv(t *testing.T) {
	tool := New()
	_, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`{"error_log":"fatal error","os_env":{}}`),
	})
	if err == nil {
		t.Fatal("expected error for missing os_env")
	}
	if !strings.Contains(err.Error(), "os_env is required") {
		t.Fatalf("error = %v, want contains %q", err, "os_env is required")
	}
}

func TestToolExecuteContextCancelled(t *testing.T) {
	tool := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := tool.Execute(ctx, tools.ToolCallInput{
		Arguments: []byte(`{"error_log":"err","os_env":{"os":"linux"}}`),
	})
	if err == nil {
		t.Fatal("expected context error")
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true")
	}
}

func TestParseDiagnoseInputEmptyOrNull(t *testing.T) {
	_, err := parseDiagnoseInput([]byte(``))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	_, err = parseDiagnoseInput([]byte(`null`))
	if err == nil {
		t.Fatal("expected error for null input")
	}
}

func TestParseDiagnoseInputMissingErrorLog(t *testing.T) {
	_, err := parseDiagnoseInput([]byte(`{"error_log":" ","os_env":{"os":"linux"}}`))
	if err == nil {
		t.Fatal("expected error for whitespace error_log")
	}
}

func TestParseDiagnoseInputMissingOSEnv(t *testing.T) {
	_, err := parseDiagnoseInput([]byte(`{"error_log":"fatal error","os_env":{}}`))
	if err == nil {
		t.Fatal("expected error for empty os_env")
	}
}
