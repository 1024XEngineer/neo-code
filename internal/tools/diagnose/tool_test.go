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

func TestToolExecuteSuccess(t *testing.T) {
	tool := New()
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`{
			"error_log":"fatal: example",
			"os_env":{"os":"linux","shell":"/bin/bash"},
			"command_text":"go test ./...",
			"exit_code":1
		}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("result.IsError = true, want false; result = %+v", result)
	}
	if result.Name != tools.ToolNameDiagnose {
		t.Fatalf("result.Name = %q, want %q", result.Name, tools.ToolNameDiagnose)
	}

	var decoded map[string]any
	if unmarshalErr := json.Unmarshal([]byte(result.Content), &decoded); unmarshalErr != nil {
		t.Fatalf("content should be valid JSON, got err = %v", unmarshalErr)
	}
	if strings.TrimSpace(toString(decoded["root_cause"])) == "" {
		t.Fatalf("root_cause should not be empty: %v", decoded)
	}
	if mock, _ := result.Metadata["mock"].(bool); !mock {
		t.Fatalf("metadata.mock = %#v, want true", result.Metadata["mock"])
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

func TestToolExecuteWithCommandText(t *testing.T) {
	tool := New()
	result, err := tool.Execute(context.Background(), tools.ToolCallInput{
		Arguments: []byte(`{"error_log":"err","os_env":{"os":"linux"},"command_text":"go test"}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("result.IsError = true, want false")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(result.Content), &decoded); err != nil {
		t.Fatalf("content should be valid JSON: %v", err)
	}
	investigation, ok := decoded["investigation_commands"].([]any)
	if !ok || len(investigation) == 0 {
		t.Fatalf("expected non-empty investigation_commands when command_text is set")
	}
	// "go test" should be appended to investigation commands
	found := false
	for _, cmd := range investigation {
		if cmd == "go test" {
			found = true
		}
	}
	if !found {
		t.Fatalf("investigation_commands = %v, should contain 'go test'", investigation)
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

func toString(value any) string {
	text, _ := value.(string)
	return text
}
