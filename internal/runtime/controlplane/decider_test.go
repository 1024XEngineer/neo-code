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
		name       string
		in         StopInput
		wantReason StopReason
	}{
		{
			name: "user_interrupt_wins_over_fatal",
			in: StopInput{
				UserInterrupted: true,
				FatalError:      errSample,
			},
			wantReason: StopReasonUserInterrupt,
		},
		{
			name: "user_interrupt_wins_over_max_turns",
			in: StopInput{
				UserInterrupted: true,
				MaxTurnsReached: true,
				MaxTurnsLimit:   40,
			},
			wantReason: StopReasonUserInterrupt,
		},
		{
			name: "fatal_wins_over_max_turns",
			in: StopInput{
				MaxTurnsReached: true,
				MaxTurnsLimit:   40,
				FatalError:      errSample,
			},
			wantReason: StopReasonFatalError,
		},
		{
			name: "fatal_error_wins_over_completed",
			in: StopInput{
				FatalError: errSample,
				Completed:  true,
			},
			wantReason: StopReasonFatalError,
		},
		{
			name: "completed",
			in: StopInput{
				Completed: true,
			},
			wantReason: StopReasonCompleted,
		},
		{
			name: "context_canceled_maps_to_user_interrupt",
			in: StopInput{
				FatalError: context.Canceled,
			},
			wantReason: StopReasonUserInterrupt,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, _ := DecideStopReason(tc.in)
			if got != tc.wantReason {
				t.Fatalf("DecideStopReason() = %q, want %q", got, tc.wantReason)
			}
		})
	}
}

func TestDecideStopReasonDetails(t *testing.T) {
	t.Parallel()

	reason, detail := DecideStopReason(StopInput{
		MaxTurnsReached: true,
		MaxTurnsLimit:   40,
	})
	if reason != StopReasonMaxTurnExceeded {
		t.Fatalf("reason = %q, want %q", reason, StopReasonMaxTurnExceeded)
	}
	if detail != "runtime: max turn limit reached (40)" {
		t.Fatalf("detail = %q, want max turn detail", detail)
	}

	reason, detail = DecideStopReason(StopInput{})
	if reason != StopReasonFatalError {
		t.Fatalf("reason = %q, want %q", reason, StopReasonFatalError)
	}
	if detail != "runtime: stop reason undetermined" {
		t.Fatalf("detail = %q, want undetermined detail", detail)
	}

	reason, detail = DecideStopReason(StopInput{
		PreDecidedReason: StopReasonCompatibilityFallback,
		PreDecidedDetail: "  fallback  ",
	})
	if reason != StopReasonCompatibilityFallback || detail != "fallback" {
		t.Fatalf("pre-decided mismatch, got (%q, %q)", reason, detail)
	}

	reason, detail = DecideStopReason(StopInput{MaxTurnsReached: true})
	if reason != StopReasonMaxTurnExceeded || detail != "" {
		t.Fatalf("max-turn no-limit mismatch, got (%q, %q)", reason, detail)
	}

	reason, detail = DecideStopReason(StopInput{RetryExhausted: true})
	if reason != StopReasonRetryExhausted || detail != "" {
		t.Fatalf("retry exhausted mismatch, got (%q, %q)", reason, detail)
	}

	reason, detail = DecideStopReason(StopInput{VerificationFailed: true})
	if reason != StopReasonVerificationFailed || detail != "" {
		t.Fatalf("verification failed mismatch, got (%q, %q)", reason, detail)
	}
}
