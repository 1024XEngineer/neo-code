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

func TestDecodeDiagnosisJSON(t *testing.T) {
	if _, ok := decodeDiagnosisJSON(`{"confidence":0.9}`); ok {
		t.Fatal("expected decodeDiagnosisJSON() to reject missing root_cause")
	}
	if _, ok := decodeDiagnosisJSON(`not-json`); ok {
		t.Fatal("expected decodeDiagnosisJSON() to reject invalid json")
	}

	decoded, ok := decodeDiagnosisJSON(`{"confidence":1.2,"root_cause":"proxy mismatch","fix_commands":["  export HTTPS_PROXY=http://127.0.0.1:7890  "],"investigation_commands":["curl -v https://example.com"]}`)
	if !ok {
		t.Fatal("expected decodeDiagnosisJSON() success")
	}
	if decoded.Confidence != 1 {
		t.Fatalf("decoded.Confidence = %v, want 1", decoded.Confidence)
	}
	if decoded.RootCause != "proxy mismatch" {
		t.Fatalf("decoded.RootCause = %q, want proxy mismatch", decoded.RootCause)
	}
	if len(decoded.FixCommands) != 1 || decoded.FixCommands[0] != "export HTTPS_PROXY=http://127.0.0.1:7890" {
		t.Fatalf("decoded.FixCommands = %#v", decoded.FixCommands)
	}
}

func TestConfidenceAndNormalizationHelpers(t *testing.T) {
	if got := parseConfidence([]string{"confidence=0.72"}); got != 0.72 {
		t.Fatalf("parseConfidence() = %v, want 0.72", got)
	}
	if got := parseConfidence([]string{"confidence=3.14"}); got != 1 {
		t.Fatalf("parseConfidence(clamp high) = %v, want 1", got)
	}
	if got := parseConfidence([]string{"confidence=bad"}); got != 0 {
		t.Fatalf("parseConfidence(invalid) = %v, want 0", got)
	}

	if got := clampConfidence(-2); got != 0 {
		t.Fatalf("clampConfidence(-2) = %v, want 0", got)
	}
	if got := clampConfidence(2); got != 1 {
		t.Fatalf("clampConfidence(2) = %v, want 1", got)
	}

	normalized := normalizeDiagnosisOutput(diagnoseOutput{
		Confidence:            -1,
		RootCause:             "   ",
		FixCommands:           []string{" go mod tidy ", "go mod tidy", " "},
		InvestigationCommands: nil,
	})
	if normalized.Confidence != 0 {
		t.Fatalf("normalized.Confidence = %v, want 0", normalized.Confidence)
	}
	if !strings.Contains(normalized.RootCause, "未获得有效根因") {
		t.Fatalf("normalized.RootCause = %q", normalized.RootCause)
	}
	if len(normalized.FixCommands) != 1 || normalized.FixCommands[0] != "go mod tidy" {
		t.Fatalf("normalized.FixCommands = %#v", normalized.FixCommands)
	}
	if normalized.InvestigationCommands == nil {
		t.Fatal("normalized.InvestigationCommands should be an empty list, not nil")
	}
}

func TestWorkdirAndTruncateHelpers(t *testing.T) {
	if got := resolveDiagnoseWorkdir("/repo", map[string]string{"cwd": "/tmp"}); got != "/repo" {
		t.Fatalf("resolveDiagnoseWorkdir(callWorkdir) = %q, want /repo", got)
	}
	if got := resolveDiagnoseWorkdir(" ", map[string]string{"cwd": " /tmp/work "}); got != "/tmp/work" {
		t.Fatalf("resolveDiagnoseWorkdir(os env) = %q, want /tmp/work", got)
	}
	if got := resolveDiagnoseWorkdir("", nil); got != "" {
		t.Fatalf("resolveDiagnoseWorkdir(empty) = %q, want empty", got)
	}

	if got := normalizePathList(" "); got != nil {
		t.Fatalf("normalizePathList(empty) = %#v, want nil", got)
	}
	if got := normalizePathList(" /repo "); len(got) != 1 || got[0] != "/repo" {
		t.Fatalf("normalizePathList() = %#v, want [/repo]", got)
	}

	if got := truncateRunes("abc", 0); got != "" {
		t.Fatalf("truncateRunes(max=0) = %q, want empty", got)
	}
	if got := truncateRunes("  你好世界  ", 2); !strings.HasPrefix(got, "你好") || !strings.Contains(got, "[truncated]") {
		t.Fatalf("truncateRunes() = %q, want truncated prefix", got)
	}
}
