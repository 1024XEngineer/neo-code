package tui

import "testing"

func TestFullHelpIncludesPasteImage(t *testing.T) {
	keys := newKeyMap()
	help := keys.FullHelp()
	for _, row := range help {
		for _, binding := range row {
			if binding.Help().Key == keys.PasteImage.Help().Key {
				return
			}
		}
	}
	t.Fatalf("expected full help to include paste image binding")
}

func TestFullHelpIncludesLogViewer(t *testing.T) {
	keys := newKeyMap()
	help := keys.FullHelp()
	foundLogViewer := false
	for _, row := range help {
		for _, binding := range row {
			if binding.Help().Key == keys.LogViewer.Help().Key {
				foundLogViewer = true
			}
		}
	}
	if !foundLogViewer {
		t.Fatalf("expected full help to include log viewer binding")
	}
}

func TestFullHelpIncludesOpenWorkspace(t *testing.T) {
	keys := newKeyMap()
	help := keys.FullHelp()
	for _, row := range help {
		for _, binding := range row {
			if binding.Help().Key == keys.OpenWorkspace.Help().Key {
				return
			}
		}
	}
	t.Fatalf("expected full help to include open workspace binding")
}
