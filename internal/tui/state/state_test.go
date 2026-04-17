package state

import "testing"

func TestPanelAndPickerConstants(t *testing.T) {
	if PanelSessions != 0 || PanelTranscript != 1 || PanelActivity != 2 || PanelTodo != 3 || PanelInput != 4 {
		t.Fatalf("unexpected panel constants: %d %d %d %d %d", PanelSessions, PanelTranscript, PanelActivity, PanelTodo, PanelInput)
	}
	if PickerNone != 0 || PickerProvider != 1 || PickerModel != 2 || PickerSession != 3 || PickerFile != 4 || PickerHelp != 5 || PickerProviderAdd != 6 {
		t.Fatalf(
			"unexpected picker constants: %d %d %d %d %d %d %d",
			PickerNone,
			PickerProvider,
			PickerModel,
			PickerSession,
			PickerFile,
			PickerHelp,
			PickerProviderAdd,
		)
	}
}

func TestUIStateCarriesFocusAndPicker(t *testing.T) {
	s := UIState{
		Focus:        PanelInput,
		ActivePicker: PickerModel,
	}
	if s.Focus != PanelInput {
		t.Fatalf("expected focus panel input, got %v", s.Focus)
	}
	if s.ActivePicker != PickerModel {
		t.Fatalf("expected model picker, got %v", s.ActivePicker)
	}
}
