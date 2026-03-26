package tui

import "testing"

func TestShortHelpOmitsPasteButFullHelpRetainsIt(t *testing.T) {
	keys := defaultKeyMap()

	for _, binding := range keys.ShortHelp() {
		if binding.Help().Key == "ctrl+v" {
			t.Fatalf("expected compact help to omit paste")
		}
	}

	foundPaste := false
	for _, group := range keys.FullHelp() {
		for _, binding := range group {
			if binding.Help().Key == "ctrl+v" {
				foundPaste = true
			}
		}
	}

	if !foundPaste {
		t.Fatalf("expected full help to keep paste")
	}
}
