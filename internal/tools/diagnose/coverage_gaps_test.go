package diagnose

import "testing"

func TestDiagnoseCoverageGapFallbackAndPathListBranches(t *testing.T) {
	output := buildFallbackDiagnosis(diagnoseInput{
		ErrorLog:    "fatal: sample",
		CommandText: "",
		OSEnv:       map[string]string{"os": "linux"},
		ExitCode:    1,
	}, "")
	if len(output.InvestigationCommands) < 2 {
		t.Fatalf("InvestigationCommands = %#v, want default commands", output.InvestigationCommands)
	}
	if output.InvestigationCommands[0] != "pwd" {
		t.Fatalf("first investigation command = %q, want pwd", output.InvestigationCommands[0])
	}

	if got := normalizePathList("   "); got != nil {
		t.Fatalf("normalizePathList(empty) = %#v, want nil", got)
	}
	got := normalizePathList(" /repo ")
	if len(got) != 1 || got[0] != "/repo" {
		t.Fatalf("normalizePathList(non-empty) = %#v, want [/repo]", got)
	}
}
