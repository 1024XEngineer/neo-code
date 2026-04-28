package urlscheme

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizeURLSchemeExecutablePath(t *testing.T) {
	t.Run("reject empty", func(t *testing.T) {
		_, err := normalizeURLSchemeExecutablePath("  ")
		if err == nil {
			t.Fatal("expected empty path error")
		}
	})

	t.Run("reject relative", func(t *testing.T) {
		_, err := normalizeURLSchemeExecutablePath("neocode")
		if err == nil {
			t.Fatal("expected relative path error")
		}
	})

	t.Run("accept absolute", func(t *testing.T) {
		absolutePath := filepath.Join(t.TempDir(), "neocode")
		normalizedPath, err := normalizeURLSchemeExecutablePath(absolutePath)
		if err != nil {
			t.Fatalf("normalizeURLSchemeExecutablePath() error = %v", err)
		}
		if normalizedPath != absolutePath {
			t.Fatalf("normalized path = %q, want %q", normalizedPath, absolutePath)
		}
	})
}

func TestMapURLSchemeCommandError(t *testing.T) {
	notFoundErr := mapURLSchemeCommandError("xdg-mime", exec.ErrNotFound)
	var dispatchErr *DispatchError
	if !errors.As(notFoundErr, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", notFoundErr)
	}
	if dispatchErr.Code != ErrorCodeNotSupported {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeNotSupported)
	}

	internalErr := mapURLSchemeCommandError("xdg-mime", errors.New("boom"))
	if !errors.As(internalErr, &dispatchErr) {
		t.Fatalf("error type = %T, want *DispatchError", internalErr)
	}
	if dispatchErr.Code != ErrorCodeInternal {
		t.Fatalf("error code = %q, want %q", dispatchErr.Code, ErrorCodeInternal)
	}
}
