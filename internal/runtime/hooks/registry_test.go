package hooks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestRegistryRegisterAndResolve(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	handler := func(ctx context.Context, input HookContext) HookResult {
		_ = ctx
		_ = input
		return HookResult{Status: HookResultPass}
	}

	if err := registry.Register(HookSpec{
		ID:       "hook-a",
		Point:    HookPointBeforeToolCall,
		Priority: 10,
		Handler:  handler,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(HookSpec{
		ID:       "hook-b",
		Point:    HookPointBeforeToolCall,
		Priority: 20,
		Handler:  handler,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(HookSpec{
		ID:       "hook-c",
		Point:    HookPointBeforeToolCall,
		Priority: 10,
		Handler:  handler,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	resolved := registry.Resolve(HookPointBeforeToolCall)
	if len(resolved) != 3 {
		t.Fatalf("Resolve() len = %d, want 3", len(resolved))
	}

	gotOrder := []string{resolved[0].ID, resolved[1].ID, resolved[2].ID}
	wantOrder := []string{"hook-b", "hook-a", "hook-c"}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("Resolve() order[%d] = %q, want %q", i, gotOrder[i], wantOrder[i])
		}
	}
}

func TestRegistryRegisterDuplicateID(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	spec := HookSpec{
		ID:      "hook-a",
		Point:   HookPointBeforeToolCall,
		Handler: func(context.Context, HookContext) HookResult { return HookResult{Status: HookResultPass} },
	}
	if err := registry.Register(spec); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	err := registry.Register(spec)
	if !errors.Is(err, ErrHookAlreadyExists) {
		t.Fatalf("Register() error = %v, want ErrHookAlreadyExists", err)
	}
}

func TestRegistryRegisterRejectsUnsupportedPoint(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	err := registry.Register(HookSpec{
		ID:      "hook-a",
		Point:   HookPoint("unknown_point"),
		Handler: func(context.Context, HookContext) HookResult { return HookResult{Status: HookResultPass} },
	})
	if !errors.Is(err, ErrInvalidHookSpec) {
		t.Fatalf("Register() error = %v, want ErrInvalidHookSpec", err)
	}
}

func TestRegistryRemove(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	if err := registry.Register(HookSpec{
		ID:      "hook-a",
		Point:   HookPointBeforeToolCall,
		Handler: func(context.Context, HookContext) HookResult { return HookResult{Status: HookResultPass} },
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := registry.Remove("hook-a"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if got := len(registry.Resolve(HookPointBeforeToolCall)); got != 0 {
		t.Fatalf("Resolve() len = %d, want 0", got)
	}

	err := registry.Remove("hook-a")
	if !errors.Is(err, ErrHookNotFound) {
		t.Fatalf("Remove() error = %v, want ErrHookNotFound", err)
	}
}

func TestRegistryResolveByPoint(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	if err := registry.Register(HookSpec{
		ID:      "hook-a",
		Point:   HookPointBeforeToolCall,
		Handler: func(context.Context, HookContext) HookResult { return HookResult{Status: HookResultPass} },
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(HookSpec{
		ID:      "hook-b",
		Point:   HookPointAfterToolResult,
		Handler: func(context.Context, HookContext) HookResult { return HookResult{Status: HookResultPass} },
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if got := len(registry.Resolve(HookPointBeforeToolCall)); got != 1 {
		t.Fatalf("Resolve(before_tool_call) len = %d, want 1", got)
	}
	if got := len(registry.Resolve(HookPointAfterToolResult)); got != 1 {
		t.Fatalf("Resolve(after_tool_result) len = %d, want 1", got)
	}
}

func TestRegistryConcurrentRegisterAndResolve(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	const workers = 32
	const total = 64

	var registerWG sync.WaitGroup
	registerWG.Add(total)
	errCh := make(chan error, total)
	for i := range total {
		go func(i int) {
			defer registerWG.Done()
			err := registry.Register(HookSpec{
				ID:      fmt.Sprintf("hook-%d", i),
				Point:   HookPointBeforeToolCall,
				Handler: func(context.Context, HookContext) HookResult { return HookResult{Status: HookResultPass} },
			})
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	var resolveWG sync.WaitGroup
	resolveWG.Add(workers)
	for range workers {
		go func() {
			defer resolveWG.Done()
			for range 20 {
				_ = registry.Resolve(HookPointBeforeToolCall)
			}
		}()
	}

	registerWG.Wait()
	resolveWG.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("Register() concurrent error = %v", err)
	}
	if got := len(registry.Resolve(HookPointBeforeToolCall)); got != total {
		t.Fatalf("Resolve() len = %d, want %d", got, total)
	}
}
