package utils

import (
	"testing"

	tuistate "neo-code/internal/tui/state"
)

func TestPickerLabelFromMode(t *testing.T) {
	tests := []struct {
		mode tuistate.PickerMode
		want string
	}{
		{tuistate.PickerProvider, "provider"},
		{tuistate.PickerModel, "model"},
		{tuistate.PickerSession, "session"},
		{tuistate.PickerFile, "file"},
		{tuistate.PickerHelp, "help"},
		{tuistate.PickerMode(999), "none"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := PickerLabelFromMode(tt.mode); got != tt.want {
				t.Errorf("PickerLabelFromMode(%v) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestRequestedWorkdirForRun(t *testing.T) {
	tests := []struct {
		name           string
		currentWorkdir string
		want           string
	}{
		{"returns current workdir", "/home/user", "/home/user"},
		{"trims whitespace", "  /home/user  ", "/home/user"},
		{"empty stays empty", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequestedWorkdirForRun(tt.currentWorkdir); got != tt.want {
				t.Errorf("RequestedWorkdirForRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBusy(t *testing.T) {
	tests := []struct {
		name           string
		isAgentRunning bool
		isCompacting   bool
		want           bool
	}{
		{"both false", false, false, false},
		{"agent running", true, false, true},
		{"compacting", false, true, true},
		{"both true", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBusy(tt.isAgentRunning, tt.isCompacting); got != tt.want {
				t.Errorf("IsBusy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFocusLabelFromPanel(t *testing.T) {
	tests := []struct {
		name  string
		focus tuistate.Panel
		want  string
	}{
		{"sessions", tuistate.PanelSessions, "sessions"},
		{"transcript", tuistate.PanelTranscript, "transcript"},
		{"activity", tuistate.PanelActivity, "activity"},
		{"todo", tuistate.PanelTodo, "todo"},
		{"input falls to default", tuistate.PanelInput, "composer"},
		{"unknown", tuistate.Panel(999), "composer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FocusLabelFromPanel(tt.focus, "sessions", "transcript", "activity", "todo", "composer"); got != tt.want {
				t.Errorf("FocusLabelFromPanel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrimRunes(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		limit int
		want  string
	}{
		{"short text", "hello", 10, "hello"},
		{"exact limit", "hello", 5, "hello"},
		{"long text", "hello world", 8, "hello..."},
		{"limit too small", "hello", 2, "hello"},
		{"limit 3", "hello", 4, "h..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrimRunes(tt.text, tt.limit); got != tt.want {
				t.Errorf("TrimRunes(%q, %d) = %q, want %q", tt.text, tt.limit, got, tt.want)
			}
		})
	}
}

func TestTrimMiddle(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		limit int
		want  string
	}{
		{"short text", "hello", 10, "hello"},
		{"exact limit", "hello", 5, "hello"},
		{"long text", "abcdefghij", 8, "ab...hij"},
		{"limit too small", "hello", 5, "hello"},
		{"limit 6", "abcdefghij", 7, "ab...ij"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrimMiddle(tt.text, tt.limit); got != tt.want {
				t.Errorf("TrimMiddle(%q, %d) = %q, want %q", tt.text, tt.limit, got, tt.want)
			}
		})
	}
}

func TestFallback(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		fallbackValue string
		want          string
	}{
		{"empty value", "", "default", "default"},
		{"whitespace only", "   ", "default", "default"},
		{"normal value", "actual", "default", "actual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Fallback(tt.value, tt.fallbackValue); got != tt.want {
				t.Errorf("Fallback(%q, %q) = %q, want %q", tt.value, tt.fallbackValue, got, tt.want)
			}
		})
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		minValue int
		maxValue int
		want     int
	}{
		{"within range", 5, 0, 10, 5},
		{"below min", -1, 0, 10, 0},
		{"above max", 15, 0, 10, 10},
		{"at min", 0, 0, 10, 0},
		{"at max", 10, 0, 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clamp(tt.value, tt.minValue, tt.maxValue); got != tt.want {
				t.Errorf("Clamp(%d, %d, %d) = %d, want %d", tt.value, tt.minValue, tt.maxValue, got, tt.want)
			}
		})
	}
}
