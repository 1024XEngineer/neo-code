package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
)

func TestBuiltinSlashCommands(t *testing.T) {
	if len(builtinSlashCommands) == 0 {
		t.Error("builtinSlashCommands should not be empty")
	}

	found := false
	for _, cmd := range builtinSlashCommands {
		if cmd.Usage == slashUsageHelp {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find /help command")
	}
}

func TestNewSelectionPicker(t *testing.T) {
	items := []list.Item{
		selectionItem{id: "1", name: "Item 1", description: "Desc 1"},
	}
	picker := newSelectionPicker(items)
	_ = picker
}

func TestNewSelectionPickerItems(t *testing.T) {
	items := []selectionItem{
		{id: "1", name: "Item 1", description: "Desc 1"},
	}
	picker := newSelectionPickerItems(items)
	_ = picker
}

func TestNewCommandMenuModel(t *testing.T) {
	uiStyles := newStyles()
	delegate := commandMenuDelegate{styles: uiStyles}
	if delegate.Height() == 0 {
		t.Error("delegate should have height")
	}
}

func TestStatusConstants(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"statusReady", statusReady},
		{"statusThinking", statusThinking},
		{"statusCanceling", statusCanceling},
		{"statusCanceled", statusCanceled},
		{"statusRunningTool", statusRunningTool},
		{"statusToolFinished", statusToolFinished},
		{"statusToolError", statusToolError},
		{"statusError", statusError},
		{"statusDraft", statusDraft},
		{"statusRunning", statusRunning},
		{"statusApplyingCommand", statusApplyingCommand},
		{"statusRebuildingWorkspace", statusRebuildingWorkspace},
		{"statusRunningCommand", statusRunningCommand},
		{"statusCommandDone", statusCommandDone},
		{"statusCompacting", statusCompacting},
		{"statusChooseProvider", statusChooseProvider},
		{"statusChooseModel", statusChooseModel},
		{"statusBrowseFile", statusBrowseFile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value == "" {
				t.Error("status constant should not be empty")
			}
		})
	}
}

func TestFocusLabels(t *testing.T) {
	if focusLabelSessions == "" {
		t.Error("focusLabelSessions should not be empty")
	}
	if focusLabelTranscript == "" {
		t.Error("focusLabelTranscript should not be empty")
	}
	if focusLabelActivity == "" {
		t.Error("focusLabelActivity should not be empty")
	}
	if focusLabelComposer == "" {
		t.Error("focusLabelComposer should not be empty")
	}
}

func TestMessageTags(t *testing.T) {
	if messageTagUser == "" {
		t.Error("messageTagUser should not be empty")
	}
	if messageTagAgent == "" {
		t.Error("messageTagAgent should not be empty")
	}
	if messageTagTool == "" {
		t.Error("messageTagTool should not be empty")
	}
}

func TestRoleConstants(t *testing.T) {
	if roleUser == "" {
		t.Error("roleUser should not be empty")
	}
	if roleAssistant == "" {
		t.Error("roleAssistant should not be empty")
	}
	if roleTool == "" {
		t.Error("roleTool should not be empty")
	}
}

func TestCopyCodeButton(t *testing.T) {
	if copyCodeButton == "" {
		t.Error("copyCodeButton should not be empty")
	}
}

func TestStatusCodeCopied(t *testing.T) {
	if statusCodeCopied == "" {
		t.Error("statusCodeCopied should not be empty")
	}
}

func TestStatusCodeCopyError(t *testing.T) {
	if statusCodeCopyError == "" {
		t.Error("statusCodeCopyError should not be empty")
	}
}

func TestMaxActivityEntries(t *testing.T) {
	if maxActivityEntries == 0 {
		t.Error("maxActivityEntries should not be zero")
	}
}
