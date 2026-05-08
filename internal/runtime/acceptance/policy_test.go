package acceptance

import (
	"testing"

	"neo-code/internal/runtime/verify"
	agentsession "neo-code/internal/session"
)

func TestMappedVerifierNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		profile agentsession.VerificationProfile
		want    []string
	}{
		{profile: agentsession.VerificationProfileTaskOnly, want: []string{"todo_convergence"}},
		{profile: agentsession.VerificationProfileCreateFile, want: []string{"todo_convergence", "file_exists", "content_match"}},
		{profile: agentsession.VerificationProfileConfig, want: []string{"todo_convergence", "file_exists", "content_match", "command_success"}},
		{profile: agentsession.VerificationProfileEditCode, want: []string{"todo_convergence", "build", "test", "typecheck"}},
		{profile: agentsession.VerificationProfileRefactor, want: []string{"todo_convergence", "build", "test", "lint", "typecheck"}},
	}

	for _, tc := range cases {
		got := mappedVerifierNames(tc.profile)
		if len(got) != len(tc.want) {
			t.Fatalf("%s len = %d, want %d", tc.profile, len(got), len(tc.want))
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("%s[%d] = %q, want %q", tc.profile, i, got[i], tc.want[i])
			}
		}
	}
}

func TestDefaultPolicyResolveVerifiers(t *testing.T) {
	t.Parallel()

	verifiers, err := (DefaultPolicy{}).ResolveVerifiers(verify.FinalVerifyInput{
		TaskState: verify.TaskStateSnapshot{VerificationProfile: string(agentsession.VerificationProfileEditCode)},
	})
	if err != nil {
		t.Fatalf("ResolveVerifiers() error = %v", err)
	}
	if len(verifiers) != 4 {
		t.Fatalf("ResolveVerifiers() len = %d, want 4", len(verifiers))
	}
	if verifiers[0].Name() != "todo_convergence" || verifiers[1].Name() != "build" {
		t.Fatalf("unexpected verifier order: %s, %s", verifiers[0].Name(), verifiers[1].Name())
	}
}

func TestDefaultPolicyResolveVerifiersRejectsInvalidProfile(t *testing.T) {
	t.Parallel()

	_, err := (DefaultPolicy{}).ResolveVerifiers(verify.FinalVerifyInput{
		TaskState: verify.TaskStateSnapshot{VerificationProfile: "unknown"},
	})
	if err == nil {
		t.Fatal("expected invalid profile error")
	}
}

func TestDefaultPolicyResolveVerifiersRejectsMissingProfile(t *testing.T) {
	t.Parallel()

	_, err := (DefaultPolicy{}).ResolveVerifiers(verify.FinalVerifyInput{})
	if err == nil {
		t.Fatal("expected missing profile error")
	}
}
