package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestSelectionServiceResolverFunc(t *testing.T) {
	var nilResolver selectionServiceResolverFunc
	if _, err := nilResolver.Resolve(&cobra.Command{}); err == nil || !strings.Contains(err.Error(), "is nil") {
		t.Fatalf("Resolve(nil resolver) err = %v, want contains \"is nil\"", err)
	}

	svc := &mockSelectionService{}
	resolver := selectionServiceResolverFunc(func(cmd *cobra.Command) (SelectionService, error) {
		if cmd == nil {
			t.Fatal("cmd should not be nil")
		}
		return svc, nil
	})
	got, err := resolver.Resolve(&cobra.Command{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != svc {
		t.Fatalf("Resolve() service mismatch: got %T", got)
	}
}

func TestRuntimeSelectionServiceResolver(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var resolver *runtimeSelectionServiceResolver
		if _, err := resolver.Resolve(&cobra.Command{}); err == nil || !strings.Contains(err.Error(), "is nil") {
			t.Fatalf("Resolve(nil receiver) err = %v, want contains \"is nil\"", err)
		}
	})

	t.Run("cache hit", func(t *testing.T) {
		cached := &mockSelectionService{}
		resolver := &runtimeSelectionServiceResolver{
			services: map[string]SelectionService{"": cached},
		}
		got, err := resolver.Resolve(nil)
		if err != nil {
			t.Fatalf("Resolve(cache hit) error = %v", err)
		}
		if got != cached {
			t.Fatalf("Resolve(cache hit) service mismatch")
		}
	})

	t.Run("build and cache by workdir", func(t *testing.T) {
		home := t.TempDir()
		workdir := t.TempDir()
		t.Setenv("HOME", home)

		cmd := &cobra.Command{}
		cmd.Flags().String("workdir", workdir, "")
		cmd.SetContext(context.Background())

		resolver := newRuntimeSelectionServiceResolver()
		first, err := resolver.Resolve(cmd)
		if err != nil {
			t.Fatalf("Resolve(first) error = %v", err)
		}
		if first == nil {
			t.Fatal("Resolve(first) service is nil")
		}

		second, err := resolver.Resolve(cmd)
		if err != nil {
			t.Fatalf("Resolve(second) error = %v", err)
		}
		if second != first {
			t.Fatal("expected same cached service instance for identical workdir")
		}
	})
}

func TestRuntimeSelectionServiceResolverCachesBuiltService(t *testing.T) {
	home := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("HOME", home)

	cmd := &cobra.Command{}
	cmd.Flags().String("workdir", workdir, "")

	resolver := &runtimeSelectionServiceResolver{services: map[string]SelectionService{}}
	got, err := resolver.Resolve(cmd)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got == nil {
		t.Fatal("Resolve() returned nil service")
	}
	cached, ok := resolver.services[workdir]
	if !ok || cached == nil {
		t.Fatalf("expected cache entry for workdir %q", workdir)
	}
	if cached != got {
		t.Fatal("expected returned service to be cached instance")
	}
}
