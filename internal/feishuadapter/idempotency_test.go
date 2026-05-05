package feishuadapter

import (
	"testing"
	"time"
)

func TestIdempotencyTryStartAndMarkDone(t *testing.T) {
	store := newIdempotencyStore(2 * time.Minute)
	now := time.Now().UTC()
	if !store.TryStart("k1", now) {
		t.Fatal("expected first TryStart to pass")
	}
	if store.TryStart("k1", now.Add(1*time.Second)) {
		t.Fatal("expected repeated TryStart within ttl to be blocked")
	}
	store.MarkDone("k1", now.Add(2*time.Second))
	if store.TryStart("k1", now.Add(3*time.Second)) {
		t.Fatal("expected done key to remain blocked within ttl")
	}
}

func TestIdempotencyMarkFailedAllowsRetry(t *testing.T) {
	store := newIdempotencyStore(2 * time.Minute)
	now := time.Now().UTC()
	if !store.TryStart("k2", now) {
		t.Fatal("expected first TryStart to pass")
	}
	store.MarkFailed("k2")
	if !store.TryStart("k2", now.Add(1*time.Second)) {
		t.Fatal("expected retry after MarkFailed to pass")
	}
}

func TestIdempotencyDefaultsAndCleanupBranches(t *testing.T) {
	store := newIdempotencyStore(0)
	if store.ttl != 10*time.Minute {
		t.Fatalf("ttl = %s, want default 10m", store.ttl)
	}
	if !store.TryStart("", time.Time{}) {
		t.Fatal("expected empty key to bypass dedupe")
	}
	store.MarkDone("", time.Time{})
	store.MarkFailed("")

	now := time.Now().UTC()
	store.items["expired"] = idempotencyItem{ExpireAt: now.Add(-time.Second), State: idempotencyStateDone}
	if !store.TryStart("expired", now) {
		t.Fatal("expected expired key to be cleaned and accepted")
	}
}
