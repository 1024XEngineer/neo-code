//go:build !windows

package ptyproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"neo-code/internal/tools"
)

func TestDiagnosisCoordinatorFingerprintAndAutoDedupe(t *testing.T) {
	first := fingerprintDiagnosisRequest(" go test ./... ", 1, "fatal error")
	if len(first) != 64 {
		t.Fatalf("fingerprint length = %d, want 64", len(first))
	}
	if got := fingerprintDiagnosisRequest("go test ./...", 1, "fatal error"); got != first {
		t.Fatalf("fingerprint should trim stable inputs, got %q want %q", got, first)
	}
	if got := fingerprintDiagnosisRequest("go test ./...", 2, "fatal error"); got == first {
		t.Fatal("fingerprint should change when exit code changes")
	}

	now := time.Unix(100, 0)
	coordinator := newDiagnosisCoordinator()
	coordinator.now = func() time.Time { return now }
	if coordinator.shouldDropAuto(first) {
		t.Fatal("first auto diagnosis should not be dropped")
	}
	if !coordinator.shouldDropAuto(first) {
		t.Fatal("same auto diagnosis should be dropped within dedupe window")
	}
	now = now.Add(diagnosisAutoDedupeTTL + time.Millisecond)
	if coordinator.shouldDropAuto(first) {
		t.Fatal("auto diagnosis should be allowed after dedupe window")
	}
}

func TestDiagnosisCoordinatorRunJoinsInflightAndCaches(t *testing.T) {
	t.Setenv(DiagCacheDisableEnv, "")

	coordinator := newDiagnosisCoordinator()
	var executeCalls int32
	release := make(chan struct{})
	started := make(chan struct{})
	var startedOnce sync.Once

	firstDone := make(chan diagnosisOutcome, 1)
	go func() {
		firstDone <- coordinator.run(context.Background(), "fp-join", func() (tools.ToolResult, error) {
			atomic.AddInt32(&executeCalls, 1)
			startedOnce.Do(func() { close(started) })
			<-release
			return tools.ToolResult{Content: "joined-result"}, nil
		})
	}()
	<-started

	secondDone := make(chan diagnosisOutcome, 1)
	go func() {
		secondDone <- coordinator.run(context.Background(), "fp-join", func() (tools.ToolResult, error) {
			atomic.AddInt32(&executeCalls, 1)
			return tools.ToolResult{Content: "unexpected-second-execute"}, nil
		})
	}()

	select {
	case outcome := <-secondDone:
		t.Fatalf("joined diagnosis returned before in-flight work completed: %#v", outcome)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)

	first := <-firstDone
	second := <-secondDone
	if first.Err != nil || second.Err != nil {
		t.Fatalf("joined outcomes should be successful: first=%v second=%v", first.Err, second.Err)
	}
	if first.Result.Content != "joined-result" || second.Result.Content != "joined-result" {
		t.Fatalf("joined contents = %q/%q, want joined-result", first.Result.Content, second.Result.Content)
	}
	if got := atomic.LoadInt32(&executeCalls); got != 1 {
		t.Fatalf("execute calls = %d, want 1", got)
	}

	cached := coordinator.run(context.Background(), "fp-join", func() (tools.ToolResult, error) {
		atomic.AddInt32(&executeCalls, 1)
		return tools.ToolResult{}, errors.New("should not execute cached request")
	})
	if cached.Err != nil || cached.Result.Content != "joined-result" {
		t.Fatalf("cached outcome = %#v, want joined-result", cached)
	}
	if got := atomic.LoadInt32(&executeCalls); got != 1 {
		t.Fatalf("execute calls after cache hit = %d, want 1", got)
	}
}

func TestDiagnosisCoordinatorCacheTTLCapacityAndErrorPolicy(t *testing.T) {
	t.Setenv(DiagCacheDisableEnv, "")

	now := time.Unix(200, 0)
	coordinator := newDiagnosisCoordinator()
	coordinator.now = func() time.Time { return now }

	coordinator.mu.Lock()
	coordinator.storeCacheLocked("ttl", diagnosisOutcome{Result: tools.ToolResult{Content: "fresh"}})
	coordinator.mu.Unlock()
	if cached, ok := coordinator.cached("ttl"); !ok || cached.Result.Content != "fresh" {
		t.Fatalf("cached ttl entry = %#v ok=%v, want fresh", cached, ok)
	}
	now = now.Add(diagnosisCacheTTL + time.Nanosecond)
	if _, ok := coordinator.cached("ttl"); ok {
		t.Fatal("expired cache entry should not be returned")
	}

	coordinator.mu.Lock()
	for i := 0; i < diagnosisCacheMaxEntries+1; i++ {
		coordinator.storeCacheLocked(fmt.Sprintf("fp-%02d", i), diagnosisOutcome{Result: tools.ToolResult{Content: "ok"}})
	}
	if len(coordinator.cache) > diagnosisCacheMaxEntries {
		t.Fatalf("cache size = %d, want <= %d", len(coordinator.cache), diagnosisCacheMaxEntries)
	}
	if _, ok := coordinator.cache["fp-00"]; ok {
		t.Fatal("oldest cache entry should be evicted")
	}
	coordinator.mu.Unlock()

	var calls int32
	for i := 0; i < 2; i++ {
		outcome := coordinator.run(context.Background(), "fp-error", func() (tools.ToolResult, error) {
			atomic.AddInt32(&calls, 1)
			return tools.ToolResult{}, errors.New("transport failed")
		})
		if outcome.Err == nil {
			t.Fatal("expected execution error")
		}
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("error calls = %d, want 2 because errors are not cached", got)
	}
	if _, ok := coordinator.cached("fp-error"); ok {
		t.Fatal("error outcome should not be cached")
	}
}

func TestDiagnosisCoordinatorFastFeedback(t *testing.T) {
	t.Setenv(DiagFastResponseDisableEnv, "")

	prepared := preparedDiagnosisRequest{
		SanitizedCommand:  "missingcmd",
		SanitizedErrorLog: "missingcmd: command not found",
	}
	manual := &bytes.Buffer{}
	renderDiagnosisInitialFeedback(manual, prepared, false)
	if text := manual.String(); !strings.Contains(text, "NeoCode Diagnosis") || !strings.Contains(text, "0.55") {
		t.Fatalf("manual feedback = %q, want diagnosis header and capped confidence", text)
	}

	auto := &bytes.Buffer{}
	renderDiagnosisInitialFeedback(auto, prepared, true)
	if text := auto.String(); !strings.Contains(text, "NeoCode Diagnosis") || !strings.Contains(text, "0.55") {
		t.Fatalf("auto feedback = %q, want quick hint for known pattern", text)
	}

	autoWithoutHint := &bytes.Buffer{}
	renderDiagnosisInitialFeedback(autoWithoutHint, preparedDiagnosisRequest{SanitizedErrorLog: "random failure"}, true)
	if autoWithoutHint.Len() != 0 {
		t.Fatalf("auto feedback without hint = %q, want empty", autoWithoutHint.String())
	}

	t.Setenv(DiagFastResponseDisableEnv, "1")
	disabled := &bytes.Buffer{}
	renderDiagnosisInitialFeedback(disabled, prepared, false)
	if disabled.Len() != 0 {
		t.Fatalf("disabled feedback = %q, want empty", disabled.String())
	}
}

func TestRunSingleDiagnosisWithCoordinatorUsesCachedResult(t *testing.T) {
	t.Setenv(DiagCacheDisableEnv, "")
	t.Setenv(DiagFastResponseDisableEnv, "")

	buffer := NewUTF8RingBuffer(1024)
	_, _ = buffer.Write([]byte("fallback log"))
	options := ManualShellOptions{Workdir: t.TempDir(), Shell: "/bin/bash"}
	trigger := diagnoseTrigger{CommandText: "go test ./...", ExitCode: 1, OutputText: "fatal"}
	prepared, err := prepareDiagnoseRequest(buffer, options, "/tmp/diag.sock", trigger)
	if err != nil {
		t.Fatalf("prepareDiagnoseRequest() error = %v", err)
	}

	coordinator := newDiagnosisCoordinator()
	coordinator.mu.Lock()
	coordinator.storeCacheLocked(prepared.Fingerprint, diagnosisOutcome{
		Result: tools.ToolResult{
			Content: `{"confidence":0.91,"root_cause":"cached root","fix_commands":["echo fix"],"investigation_commands":["echo inv"]}`,
		},
	})
	coordinator.mu.Unlock()

	output := &bytes.Buffer{}
	err = runSingleDiagnosisWithCoordinator(
		context.Background(),
		coordinator,
		nil,
		output,
		buffer,
		options,
		"/tmp/diag.sock",
		trigger,
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("runSingleDiagnosisWithCoordinator() error = %v", err)
	}
	if text := output.String(); !strings.Contains(text, "cached root") || !strings.Contains(text, "NeoCode Diagnosis") {
		t.Fatalf("output = %q, want cached diagnosis result", text)
	}
}
