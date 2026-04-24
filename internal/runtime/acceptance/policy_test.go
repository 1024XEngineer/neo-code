package acceptance

import (
	"testing"

	"neo-code/internal/config"
	"neo-code/internal/runtime/verify"
)

func TestResolveTaskType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		metadata map[string]any
		fallback string
		want     taskType
	}{
		{name: "metadata wins", metadata: map[string]any{"task_type": "fix_bug"}, fallback: "docs", want: taskTypeFixBug},
		{name: "fallback used", fallback: "refactor", want: taskTypeRefactor},
		{name: "unknown fallback", fallback: "other", want: taskTypeUnknown},
		{name: "unknown metadata", metadata: map[string]any{"task_type": 100}, fallback: "docs", want: taskTypeUnknown},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveTaskType(tc.metadata, tc.fallback); got != tc.want {
				t.Fatalf("resolveTaskType() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMappedVerifierNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		task taskType
		want []string
	}{
		{task: taskTypeCreateFile, want: []string{"todo_convergence", "file_exists", "content_match"}},
		{task: taskTypeDocs, want: []string{"todo_convergence", "file_exists", "content_match"}},
		{task: taskTypeConfig, want: []string{"todo_convergence", "file_exists", "content_match", "command_success"}},
		{task: taskTypeEditCode, want: []string{"todo_convergence", "git_diff", "build", "test", "typecheck"}},
		{task: taskTypeFixBug, want: []string{"todo_convergence", "git_diff", "test", "build", "typecheck"}},
		{task: taskTypeRefactor, want: []string{"todo_convergence", "git_diff", "build", "test", "lint", "typecheck"}},
		{task: taskTypeUnknown, want: []string{"todo_convergence"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.task), func(t *testing.T) {
			t.Parallel()
			got := mappedVerifierNames(tc.task)
			if len(got) != len(tc.want) {
				t.Fatalf("mappedVerifierNames() len = %d, want %d", len(got), len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("mappedVerifierNames()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDefaultPolicyResolveVerifiers(t *testing.T) {
	t.Parallel()

	cfg := config.StaticDefaults().Runtime.Verification
	cfg.Enabled = boolPtrPolicy(true)
	cfg.Verifiers["git_diff"] = config.VerifierConfig{Enabled: true}
	cfg.Verifiers["build"] = config.VerifierConfig{Enabled: true}
	cfg.Verifiers["test"] = config.VerifierConfig{Enabled: false}
	cfg.Verifiers["typecheck"] = config.VerifierConfig{Enabled: true}

	verifiers := (DefaultPolicy{}).ResolveVerifiers(verify.FinalVerifyInput{
		Metadata:           map[string]any{"task_type": "edit_code"},
		VerificationConfig: cfg,
	})

	if len(verifiers) != 4 {
		t.Fatalf("ResolveVerifiers() len = %d, want 4", len(verifiers))
	}

	names := make([]string, 0, len(verifiers))
	for _, item := range verifiers {
		names = append(names, item.Name())
	}
	wantNames := []string{"todo_convergence", "git_diff", "build", "typecheck"}
	for i := range wantNames {
		if names[i] != wantNames[i] {
			t.Fatalf("name[%d] = %q, want %q", i, names[i], wantNames[i])
		}
	}
}

func TestDefaultPolicyBuildVerifierAndEnabledFallback(t *testing.T) {
	t.Parallel()

	policy := DefaultPolicy{}
	if verifier := policy.buildVerifier("unknown"); verifier != nil {
		t.Fatalf("buildVerifier(unknown) = %T, want nil", verifier)
	}

	cfg := config.StaticDefaults().Runtime.Verification
	cfg.Enabled = boolPtrPolicy(true)
	delete(cfg.Verifiers, "todo_convergence")
	delete(cfg.Verifiers, "git_diff")
	if !isVerifierEnabled("todo_convergence", verify.FinalVerifyInput{VerificationConfig: cfg}) {
		t.Fatalf("todo_convergence should be enabled by default when config missing")
	}
	if isVerifierEnabled("git_diff", verify.FinalVerifyInput{VerificationConfig: cfg}) {
		t.Fatalf("git_diff should be disabled when config missing")
	}
}

func boolPtrPolicy(value bool) *bool {
	v := value
	return &v
}
