package acceptance

import (
	"testing"

	"neo-code/internal/runtime/controlplane"
)

func TestTerminalStatusFromAcceptance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status AcceptanceStatus
		want   controlplane.TerminalStatus
	}{
		{status: AcceptanceAccepted, want: controlplane.TerminalStatusCompleted},
		{status: AcceptanceFailed, want: controlplane.TerminalStatusFailed},
		{status: AcceptanceIncomplete, want: controlplane.TerminalStatusIncomplete},
		{status: AcceptanceContinue, want: controlplane.TerminalStatusContinue},
		{status: AcceptanceStatus("other"), want: controlplane.TerminalStatusContinue},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.status), func(t *testing.T) {
			t.Parallel()
			if got := TerminalStatusFromAcceptance(tc.status); got != tc.want {
				t.Fatalf("TerminalStatusFromAcceptance(%q) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}
