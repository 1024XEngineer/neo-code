package ptyproxy

import "testing"

func TestShouldTriggerAutoDiagnosis(t *testing.T) {
	tests := []struct {
		name        string
		exitCode    int
		commandText string
		outputText  string
		want        bool
	}{
		{
			name:       "success exit skipped",
			exitCode:   0,
			outputText: "fatal: file not found",
			want:       false,
		},
		{
			name:       "ctrl c skipped",
			exitCode:   130,
			outputText: "error timeout",
			want:       false,
		},
		{
			name:       "sigkill skipped",
			exitCode:   137,
			outputText: "killed",
			want:       false,
		},
		{
			name:        "command exemption grep",
			exitCode:    1,
			commandText: "grep foo file.txt",
			outputText:  "foo not found",
			want:        false,
		},
		{
			name:        "command exemption find",
			exitCode:    1,
			commandText: "find . -name x",
			outputText:  "find: nothing",
			want:        false,
		},
		{
			name:        "command exemption neocode",
			exitCode:    1,
			commandText: "./neocode diag",
			outputText:  "network timeout",
			want:        false,
		},
		{
			name:        "command exemption wrapped neocode diag",
			exitCode:    1,
			commandText: "timeout 10s ./neocode diag",
			outputText:  "network timeout",
			want:        false,
		},
		{
			name:        "skip on neocode diagnosis output",
			exitCode:    1,
			commandText: "echo failed",
			outputText:  "[NeoCode Diagnosis] timeout",
			want:        false,
		},
		{
			name:        "output too short skipped",
			exitCode:    1,
			commandText: "go test ./...",
			outputText:  "failed",
			want:        false,
		},
		{
			name:        "keyword hit triggers",
			exitCode:    2,
			commandText: "go test ./...",
			outputText:  "fatal: module not found in current directory",
			want:        true,
		},
		{
			name:        "long output without keyword still triggers",
			exitCode:    1,
			commandText: "make build",
			outputText:  "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			want:        true,
		},
	}

	for _, tc := range tests {
		got := ShouldTriggerAutoDiagnosis(tc.exitCode, tc.commandText, tc.outputText)
		if got != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsCommandExempted(t *testing.T) {
	if !isCommandExempted("  TEST foo  ") {
		t.Fatal("TEST should be exempted")
	}
	if isCommandExempted("go test ./...") {
		t.Fatal("go should not be exempted")
	}
}

func TestFilterHelperBranches(t *testing.T) {
	if isCommandExempted("   ") {
		t.Fatal("empty command should not be exempted")
	}
	if hasMeaningfulOutput("tiny") {
		t.Fatal("short output should not be meaningful")
	}
	if !hasMeaningfulOutput("permission denied while opening file") {
		t.Fatal("keyword-based output should be meaningful")
	}
}
