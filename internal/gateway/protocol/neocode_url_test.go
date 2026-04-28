package protocol

import "testing"

func TestIsSupportedWakeAction(t *testing.T) {
	tests := []struct {
		name   string
		action string
		want   bool
	}{
		{name: "review", action: "review", want: true},
		{name: "run", action: "run", want: true},
		{name: "uppercase run", action: " RUN ", want: true},
		{name: "unsupported", action: "open", want: false},
		{name: "empty", action: " ", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSupportedWakeAction(tt.action); got != tt.want {
				t.Fatalf("IsSupportedWakeAction(%q) = %v, want %v", tt.action, got, tt.want)
			}
		})
	}
}
