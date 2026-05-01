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

func toString(value any) string {
	text, _ := value.(string)
	return text
}
