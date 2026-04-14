package controlplane

import (
	"context"
	"errors"
	"testing"
)

func TestDecideStopReasonPriority(t *testing.T) {
	t.Parallel()

	errSample := errors.New("boom")
	cases := []struct {
		name   string
		in     StopInput
		reason StopReason
	}{
		{
			name: "canceled_wins_over_max_loops",
			in: StopInput{
				ContextCanceled: true,
				MaxLoopsReached: true,
				RunError:        errSample,
			},
			reason: StopReasonCanceled,
		},
		{
			name: "max_loops_wins_over_error",
			in: StopInput{
				MaxLoopsReached: true,
				RunError:        errSample,
			},
			reason: StopReasonMaxLoops,
		},
		{
			name: "error_when_no_max_loop_flag",
			in: StopInput{
				RunError: errSample,
			},
			reason: StopReasonError,
		},
		{
			name: "success",
			in: StopInput{
				Success: true,
			},
			reason: StopReasonSuccess,
		},
		{
			name: "context_canceled_on_error_field",
			in: StopInput{
				RunError: context.Canceled,
			},
			reason: StopReasonCanceled,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _ := DecideStopReason(tc.in)
			if got != tc.reason {
				t.Fatalf("DecideStopReason() = %q, want %q", got, tc.reason)
			}
		})
	}
}

func TestDecideStopReasonDetails(t *testing.T) {
	t.Parallel()

	reason, detail := DecideStopReason(StopInput{MaxLoopsReached: true})
	if reason != StopReasonMaxLoops {
		t.Fatalf("reason = %q, want %q", reason, StopReasonMaxLoops)
	}
	if detail != "runtime: max loop reached" {
		t.Fatalf("detail = %q, want default max-loop detail", detail)
	}

	reason, detail = DecideStopReason(StopInput{})
	if reason != StopReasonError {
		t.Fatalf("reason = %q, want %q", reason, StopReasonError)
	}
	if detail != "runtime: stop reason undetermined" {
		t.Fatalf("detail = %q, want undetermined detail", detail)
	}
}
