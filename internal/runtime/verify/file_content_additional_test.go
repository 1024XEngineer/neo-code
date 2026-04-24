package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"neo-code/internal/config"
)

func TestFileExistsVerifierAdditionalBranches(t *testing.T) {
	t.Parallel()

	v := FileExistsVerifier{}

	t.Run("missing metadata optional and required", func(t *testing.T) {
		t.Parallel()
		optional, err := v.VerifyFinal(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("VerifyFinal() optional error = %v", err)
		}
		if optional.Status != VerificationPass {
			t.Fatalf("optional status = %q, want pass", optional.Status)
		}

		cfg := defaultVerificationConfigForTest()
		cfg.Verifiers[fileExistsVerifierName] = verifierConfigForTest(true)
		required, err := v.VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() required error = %v", err)
		}
		if required.Status != VerificationSoftBlock {
			t.Fatalf("required status = %q, want soft_block", required.Status)
		}
	})

	t.Run("missing empty dir denied branches", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		workdir := filepath.Join(root, "work")
		if err := os.MkdirAll(workdir, 0o755); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		dirPath := filepath.Join(workdir, "d")
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			t.Fatalf("MkdirAll(dir) error = %v", err)
		}
		emptyPath := filepath.Join(workdir, "empty.txt")
		if err := os.WriteFile(emptyPath, nil, 0o644); err != nil {
			t.Fatalf("WriteFile(empty) error = %v", err)
		}

		result, err := v.VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: workdir,
			Metadata: map[string]any{
				"expected_files": []string{"missing.txt", "empty.txt", "d", "../outside"},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want fail", result.Status)
		}
		if result.ErrorClass != ErrorClassPermissionDenied {
			t.Fatalf("error class = %q, want permission_denied", result.ErrorClass)
		}
	})

	t.Run("missing and empty without denied should be unknown fail", func(t *testing.T) {
		t.Parallel()
		workdir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workdir, "empty.txt"), nil, 0o644); err != nil {
			t.Fatalf("WriteFile(empty) error = %v", err)
		}
		result, err := v.VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: workdir,
			Metadata: map[string]any{
				"expected_files": []string{"missing.txt", "empty.txt"},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail || result.ErrorClass != ErrorClassUnknown {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("symlink escaping workdir should be denied", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		workdir := filepath.Join(root, "work")
		outside := filepath.Join(root, "outside")
		if err := os.MkdirAll(workdir, 0o755); err != nil {
			t.Fatalf("MkdirAll(workdir) error = %v", err)
		}
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatalf("MkdirAll(outside) error = %v", err)
		}
		outsideFile := filepath.Join(outside, "secret.txt")
		if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
			t.Fatalf("WriteFile(outside) error = %v", err)
		}
		if err := os.Symlink(outsideFile, filepath.Join(workdir, "link.txt")); err != nil {
			t.Skipf("symlink is not available on this platform: %v", err)
		}

		result, err := v.VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: workdir,
			Metadata: map[string]any{
				"expected_files": []string{"link.txt"},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail || result.ErrorClass != ErrorClassPermissionDenied {
			t.Fatalf("unexpected symlink denied result: %+v", result)
		}
	})
}

func TestContentMatchVerifierAdditionalBranches(t *testing.T) {
	t.Parallel()

	v := ContentMatchVerifier{}

	t.Run("missing metadata optional and required", func(t *testing.T) {
		t.Parallel()
		optional, err := v.VerifyFinal(context.Background(), FinalVerifyInput{})
		if err != nil {
			t.Fatalf("VerifyFinal() optional error = %v", err)
		}
		if optional.Status != VerificationPass {
			t.Fatalf("optional status = %q, want pass", optional.Status)
		}

		cfg := defaultVerificationConfigForTest()
		cfg.Verifiers[contentMatchVerifierName] = verifierConfigForTest(true)
		required, err := v.VerifyFinal(context.Background(), FinalVerifyInput{VerificationConfig: cfg})
		if err != nil {
			t.Fatalf("VerifyFinal() required error = %v", err)
		}
		if required.Status != VerificationSoftBlock {
			t.Fatalf("required status = %q, want soft_block", required.Status)
		}
	})

	t.Run("missing file and token mismatch", func(t *testing.T) {
		t.Parallel()
		workdir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workdir, "a.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		result, err := v.VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: workdir,
			Metadata: map[string]any{
				"content_match": map[string][]string{
					"a.txt":       []string{"hello", "world"},
					"missing.txt": []string{"x"},
				},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail {
			t.Fatalf("status = %q, want fail", result.Status)
		}
	})

	t.Run("collectMissingTokens compacts blanks", func(t *testing.T) {
		t.Parallel()
		missing := map[string][]string{}
		collectMissingTokens(missing, "a.txt", "hello", []string{" hello ", "", "world"})
		if len(missing["a.txt"]) != 1 || missing["a.txt"][0] != "world" {
			t.Fatalf("missing tokens = %#v", missing)
		}
	})

	t.Run("symlink escaping workdir should be denied", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		workdir := filepath.Join(root, "work")
		outside := filepath.Join(root, "outside")
		if err := os.MkdirAll(workdir, 0o755); err != nil {
			t.Fatalf("MkdirAll(workdir) error = %v", err)
		}
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatalf("MkdirAll(outside) error = %v", err)
		}
		outsideFile := filepath.Join(outside, "secret.txt")
		if err := os.WriteFile(outsideFile, []byte("secret-token"), 0o644); err != nil {
			t.Fatalf("WriteFile(outside) error = %v", err)
		}
		if err := os.Symlink(outsideFile, filepath.Join(workdir, "link.txt")); err != nil {
			t.Skipf("symlink is not available on this platform: %v", err)
		}

		result, err := v.VerifyFinal(context.Background(), FinalVerifyInput{
			Workdir: workdir,
			Metadata: map[string]any{
				"content_match": map[string][]string{
					"link.txt": []string{"secret-token"},
				},
			},
		})
		if err != nil {
			t.Fatalf("VerifyFinal() error = %v", err)
		}
		if result.Status != VerificationFail || result.ErrorClass != ErrorClassPermissionDenied {
			t.Fatalf("unexpected symlink denied result: %+v", result)
		}
	})
}

func defaultVerificationConfigForTest() config.VerificationConfig {
	cfg := config.VerificationConfig{}
	cfg.Verifiers = map[string]config.VerifierConfig{}
	return cfg
}

func verifierConfigForTest(required bool) config.VerifierConfig {
	return config.VerifierConfig{Required: required}
}
