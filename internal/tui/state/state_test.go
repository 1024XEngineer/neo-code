package state

import "testing"

func TestPanelAndPickerConstants(t *testing.T) {
	if PanelSessions != 0 || PanelTranscript != 1 || PanelActivity != 2 || PanelInput != 3 {
		t.Fatalf("unexpected panel constants: %d %d %d %d", PanelSessions, PanelTranscript, PanelActivity, PanelInput)
	}
	if PickerNone != 0 || PickerProvider != 1 || PickerModel != 2 || PickerFile != 3 {
		t.Fatalf("unexpected picker constants: %d %d %d %d", PickerNone, PickerProvider, PickerModel, PickerFile)
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
