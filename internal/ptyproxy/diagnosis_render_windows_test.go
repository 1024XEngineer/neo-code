//go:build windows

package ptyproxy

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderDiagnosisInitialFeedbackWrapsCursorOnWindows(t *testing.T) {
	t.Setenv(DiagFastResponseDisableEnv, "")

	prepared := preparedDiagnosisRequest{
		SanitizedCommand:  "missingcmd",
		SanitizedErrorLog: "missingcmd: command not found",
	}
	output := &bytes.Buffer{}
	renderDiagnosisInitialFeedback(output, prepared, false)

	text := output.String()
	if !strings.HasPrefix(text, diagnosisCursorSaveVT) {
		t.Fatalf("feedback should start with cursor save, got %q", text)
	}
	if !strings.Contains(text, diagnosisCursorRestoreVT) {
		t.Fatalf("feedback should include cursor restore, got %q", text)
	}
	if !strings.Contains(text, "NeoCode Diagnosis") {
		t.Fatalf("feedback should include diagnosis header, got %q", text)
	}
}

func TestRenderDiagnosisWrapsCursorOnWindows(t *testing.T) {
	output := &bytes.Buffer{}
	renderDiagnosis(output, `{"confidence":0.91,"root_cause":"cached root","fix_commands":["echo fix"]}`, false)

	text := output.String()
	if !strings.HasPrefix(text, diagnosisCursorSaveVT) {
		t.Fatalf("diagnosis should start with cursor save, got %q", text)
	}
	if !strings.Contains(text, diagnosisCursorRestoreVT) {
		t.Fatalf("diagnosis should include cursor restore, got %q", text)
	}
	if !strings.Contains(text, "root cause: cached root") {
		t.Fatalf("diagnosis should include parsed root cause, got %q", text)
	}
}
