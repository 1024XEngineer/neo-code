//go:build !windows

package config

import "testing"

func TestPersistUserEnvVarNoopOnNonWindows(t *testing.T) {
	if err := PersistUserEnvVar("NEOCODE_TEST_KEY", "value"); err != nil {
		t.Fatalf("PersistUserEnvVar() error = %v", err)
	}
}

func TestDeleteUserEnvVarNoopOnNonWindows(t *testing.T) {
	if err := DeleteUserEnvVar("NEOCODE_TEST_KEY"); err != nil {
		t.Fatalf("DeleteUserEnvVar() error = %v", err)
	}
}

func TestLookupUserEnvVarNoopOnNonWindows(t *testing.T) {
	value, exists, err := LookupUserEnvVar("NEOCODE_TEST_KEY")
	if err != nil {
		t.Fatalf("LookupUserEnvVar() error = %v", err)
	}
	if exists {
		t.Fatalf("LookupUserEnvVar() exists = true, want false")
	}
	if value != "" {
		t.Fatalf("LookupUserEnvVar() value = %q, want empty", value)
	}
}
