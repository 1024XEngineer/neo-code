package ptyproxy

import (
	"strings"
	"testing"
)

func TestBuildShellInitScript(t *testing.T) {
	defaultScript := BuildShellInitScript("")
	if !strings.Contains(defaultScript, "neocode shell integration") {
		t.Fatalf("default script missing marker: %q", defaultScript)
	}
	if strings.HasPrefix(defaultScript, "# target shell:") {
		t.Fatalf("default script should not contain target header: %q", defaultScript)
	}

	annotated := BuildShellInitScript(" /bin/zsh ")
	if !strings.HasPrefix(annotated, "# target shell: /bin/zsh\n") {
		t.Fatalf("annotated script header = %q", annotated)
	}
	if !strings.Contains(annotated, "neocode shell integration") {
		t.Fatalf("annotated script missing marker: %q", annotated)
	}
}
