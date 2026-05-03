package hooks

import (
	"context"
	"testing"
	"time"
)

func BenchmarkExecutorRunNoHooks(b *testing.B) {
	b.ReportAllocs()

	registry := NewRegistry()
	executor := NewExecutor(registry, nil, time.Second)
	input := HookContext{
		RunID:     "run-bench-no-hooks",
		SessionID: "session-bench-no-hooks",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = executor.Run(context.Background(), HookPointBeforeToolCall, input)
	}
}

func BenchmarkExecutorRunTenHooks(b *testing.B) {
	b.ReportAllocs()

	registry := NewRegistry()
	for i := 0; i < 10; i++ {
		spec := HookSpec{
			ID:       "bench-hook-" + string(rune('a'+i)),
			Point:    HookPointBeforeToolCall,
			Priority: 100 - i,
			Handler: func(context.Context, HookContext) HookResult {
				return HookResult{Status: HookResultPass}
			},
		}
		if err := registry.Register(spec); err != nil {
			b.Fatalf("register bench hook: %v", err)
		}
	}

	executor := NewExecutor(registry, nil, time.Second)
	input := HookContext{
		RunID:     "run-bench-ten-hooks",
		SessionID: "session-bench-ten-hooks",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = executor.Run(context.Background(), HookPointBeforeToolCall, input)
	}
}

func TestExecutorRunOverheadWithinThreshold(t *testing.T) {
	t.Parallel()

	const (
		iterations    = 3000
		hookCount     = 10
		maxMultiplier = 30
		maxExtraCost  = 300 * time.Microsecond
	)

	noHookRegistry := NewRegistry()
	noHookExecutor := NewExecutor(noHookRegistry, nil, time.Second)
	baseInput := HookContext{
		RunID:     "run-overhead-no-hooks",
		SessionID: "session-overhead-no-hooks",
	}

	withHookRegistry := NewRegistry()
	for i := 0; i < hookCount; i++ {
		spec := HookSpec{
			ID:       "overhead-hook-" + string(rune('a'+i)),
			Point:    HookPointBeforeToolCall,
			Priority: hookCount - i,
			Handler: func(context.Context, HookContext) HookResult {
				return HookResult{Status: HookResultPass}
			},
		}
		if err := withHookRegistry.Register(spec); err != nil {
			t.Fatalf("register hook %d: %v", i, err)
		}
	}
	withHookExecutor := NewExecutor(withHookRegistry, nil, time.Second)
	withHookInput := HookContext{
		RunID:     "run-overhead-with-hooks",
		SessionID: "session-overhead-with-hooks",
	}

	// warmup
	for i := 0; i < 200; i++ {
		_ = noHookExecutor.Run(context.Background(), HookPointBeforeToolCall, baseInput)
		_ = withHookExecutor.Run(context.Background(), HookPointBeforeToolCall, withHookInput)
	}

	start := time.Now()
	for i := 0; i < iterations; i++ {
		output := noHookExecutor.Run(context.Background(), HookPointBeforeToolCall, baseInput)
		if len(output.Results) != 0 || output.Blocked {
			t.Fatalf("unexpected no-hook output: %+v", output)
		}
	}
	noHookDuration := time.Since(start)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		output := withHookExecutor.Run(context.Background(), HookPointBeforeToolCall, withHookInput)
		if len(output.Results) != hookCount {
			t.Fatalf("with-hook result len = %d, want %d", len(output.Results), hookCount)
		}
		if output.Blocked {
			t.Fatalf("with-hook output should not be blocked: %+v", output)
		}
	}
	withHookDuration := time.Since(start)

	noHookAvg := noHookDuration / iterations
	withHookAvg := withHookDuration / iterations
	threshold := noHookAvg*time.Duration(maxMultiplier) + maxExtraCost
	if withHookAvg > threshold {
		t.Fatalf(
			"hook overhead regression: no-hook avg=%s, with-hook avg=%s, threshold=%s",
			noHookAvg,
			withHookAvg,
			threshold,
		)
	}
}
