package verify

import "testing"

func TestVerifierNamesAndConstructors(t *testing.T) {
	t.Parallel()

	if got := (FileExistsVerifier{}).Name(); got != fileExistsVerifierName {
		t.Fatalf("FileExistsVerifier.Name() = %q, want %q", got, fileExistsVerifierName)
	}
	if got := (ContentMatchVerifier{}).Name(); got != contentMatchVerifierName {
		t.Fatalf("ContentMatchVerifier.Name() = %q, want %q", got, contentMatchVerifierName)
	}
	if got := (TodoConvergenceVerifier{}).Name(); got != todoConvergenceVerifierName {
		t.Fatalf("TodoConvergenceVerifier.Name() = %q, want %q", got, todoConvergenceVerifierName)
	}

	if got := NewBuildVerifier(nil).Name(); got != buildVerifierName {
		t.Fatalf("NewBuildVerifier().Name() = %q, want %q", got, buildVerifierName)
	}
	if got := NewTestVerifier(nil).Name(); got != testVerifierName {
		t.Fatalf("NewTestVerifier().Name() = %q, want %q", got, testVerifierName)
	}
	if got := NewLintVerifier(nil).Name(); got != lintVerifierName {
		t.Fatalf("NewLintVerifier().Name() = %q, want %q", got, lintVerifierName)
	}
	if got := NewTypecheckVerifier(nil).Name(); got != typecheckVerifierName {
		t.Fatalf("NewTypecheckVerifier().Name() = %q, want %q", got, typecheckVerifierName)
	}
}
