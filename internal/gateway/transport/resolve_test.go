package transport

import "testing"

func TestResolveListenAddressUsesOverride(t *testing.T) {
	address, err := ResolveListenAddress("  custom-address  ")
	if err != nil {
		t.Fatalf("resolve listen address: %v", err)
	}
	if address != "custom-address" {
		t.Fatalf("resolved address = %q, want %q", address, "custom-address")
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
