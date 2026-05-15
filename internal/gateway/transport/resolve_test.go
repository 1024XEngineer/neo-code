package transport

import (
	"runtime"
	"testing"
)

func TestResolveListenAddressUsesOverride(t *testing.T) {
	override := "custom-address"
	want := "custom-address"
	if runtime.GOOS == "windows" {
		override = `\\.\pipe\custom-address`
		want = `\\.\pipe\custom-address`
	}

	address, err := ResolveListenAddress("  " + override + "  ")
	if err != nil {
		t.Fatalf("resolve listen address: %v", err)
	}
	if address != want {
		t.Fatalf("resolved address = %q, want %q", address, want)
	}
}

func TestResolveListenAddressUsesDefaultWhenOverrideEmpty(t *testing.T) {
	address, err := ResolveListenAddress("   ")
	if err != nil {
		t.Fatalf("resolve listen address: %v", err)
	}
	if address == "" {
		t.Fatal("resolved default address should not be empty")
	}
}
