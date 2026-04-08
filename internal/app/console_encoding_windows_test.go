//go:build windows

package app

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestPlatformSetConsoleCodePagesWithCurrentValues(t *testing.T) {
	output, err := windows.GetConsoleOutputCP()
	if err != nil {
		t.Fatalf("GetConsoleOutputCP() error = %v", err)
	}
	if err := platformSetConsoleOutputCodePage(output); err != nil {
		t.Fatalf("platformSetConsoleOutputCodePage() error = %v", err)
	}

	input, err := windows.GetConsoleCP()
	if err != nil {
		t.Fatalf("GetConsoleCP() error = %v", err)
	}
	if err := platformSetConsoleInputCodePage(input); err != nil {
		t.Fatalf("platformSetConsoleInputCodePage() error = %v", err)
	}
}
