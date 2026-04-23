package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"neo-code/internal/gateway"
	"neo-code/internal/gateway/protocol"
)

func TestBuildMethodExamples(t *testing.T) {
	examples, err := buildMethodExamples()
	if err != nil {
		t.Fatalf("buildMethodExamples() error = %v", err)
	}

	if len(examples) != 11 {
		t.Fatalf("len(examples) = %d, want 11", len(examples))
	}

	if examples[0].Method != protocol.MethodGatewayAuthenticate {
		t.Fatalf("examples[0].Method = %q, want %q", examples[0].Method, protocol.MethodGatewayAuthenticate)
	}

	if examples[len(examples)-1].Method != protocol.MethodGatewayEvent {
		t.Fatalf("last method = %q, want %q", examples[len(examples)-1].Method, protocol.MethodGatewayEvent)
	}
}

func TestRenderGeneratedBlock(t *testing.T) {
	block, err := renderGeneratedBlock()
	if err != nil {
		t.Fatalf("renderGeneratedBlock() error = %v", err)
	}

	mustContain := []string{
		"### " + protocol.MethodGatewayRun,
		"### " + protocol.MethodGatewayBindStream,
		"### " + protocol.MethodGatewayEvent,
		"```json",
		"Notes：",
	}
	for _, item := range mustContain {
		if !strings.Contains(block, item) {
			t.Fatalf("rendered block missing %q", item)
		}
	}
}

func TestReplaceGeneratedBlockSuccess(t *testing.T) {
	doc := strings.Join([]string{
		"intro",
		endMarker,
		"noise",
		beginMarker,
		"old",
		endMarker,
		"tail",
	}, "\n")

	replaced, err := replaceGeneratedBlock(doc, "NEW-CONTENT")
	if err != nil {
		t.Fatalf("replaceGeneratedBlock() error = %v", err)
	}

	expected := strings.Join([]string{
		"intro",
		endMarker,
		"noise",
		beginMarker,
		"NEW-CONTENT",
		endMarker,
		"tail",
	}, "\n")
	if replaced != expected {
		t.Fatalf("replaceGeneratedBlock()\n got: %q\nwant: %q", replaced, expected)
	}
}

func TestReplaceGeneratedBlockErrors(t *testing.T) {
	if _, err := replaceGeneratedBlock("content only", "x"); err == nil || !strings.Contains(err.Error(), "起始标记") {
		t.Fatalf("missing begin marker should return start marker error, got %v", err)
	}

	docWithoutEnd := beginMarker + "\nold\n"
	if _, err := replaceGeneratedBlock(docWithoutEnd, "x"); err == nil || !strings.Contains(err.Error(), "结束标记") {
		t.Fatalf("missing end marker should return end marker error, got %v", err)
	}
}

func TestMainUpdatesAndNoop(t *testing.T) {
	tmp := t.TempDir()
	docPath := filepath.Join(tmp, targetDocPath)
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	initialDoc := strings.Join([]string{
		"# API",
		beginMarker,
		"old content",
		endMarker,
	}, "\n")
	if err := os.WriteFile(docPath, []byte(initialDoc), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWD)
	}()

	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	main()

	first, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("ReadFile() after first main error = %v", err)
	}
	if !strings.Contains(string(first), "### "+protocol.MethodGatewayRun) {
		t.Fatalf("generated doc does not contain %q section", protocol.MethodGatewayRun)
	}

	main()

	second, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("ReadFile() after second main error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("second main() should be noop update")
	}
}

func TestHelpers(t *testing.T) {
	request := buildRequest("  req-1  ", protocol.MethodGatewayPing, map[string]string{"k": "v"})
	if string(request.ID) != `"req-1"` {
		t.Fatalf("request.ID = %s, want %s", string(request.ID), `"req-1"`)
	}
	if request.Method != protocol.MethodGatewayPing {
		t.Fatalf("request.Method = %q, want %q", request.Method, protocol.MethodGatewayPing)
	}

	requestNoParams := buildRequest("req-2", protocol.MethodGatewayListSessions, nil)
	if len(requestNoParams.Params) != 0 {
		t.Fatalf("requestNoParams.Params should be empty")
	}

	success := buildSuccessResponse("req-3", gateway.MessageFrame{
		Type:   gateway.FrameTypeAck,
		Action: gateway.FrameActionPing,
	})
	if success.Error != nil {
		t.Fatalf("success response should not carry error")
	}

	failure := buildFailureResponse("req-4", -32000, "  bad  ", "  gateway.bad  ")
	if failure.Error == nil {
		t.Fatalf("failure response should carry error")
	}
	if failure.Error.Message != "bad" {
		t.Fatalf("failure message = %q, want %q", failure.Error.Message, "bad")
	}

	raw := marshalRawJSON(map[string]string{"hello": "world"})
	decoded := map[string]string{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["hello"] != "world" {
		t.Fatalf("decoded[hello] = %q, want %q", decoded["hello"], "world")
	}

	pretty := mustPrettyJSON(map[string]int{"a": 1})
	if !strings.Contains(pretty, "\n") {
		t.Fatalf("mustPrettyJSON() should include indentation")
	}

	parsed := mustParseTime("2026-04-22T09:00:00Z")
	if parsed.IsZero() {
		t.Fatalf("mustParseTime() returned zero value")
	}

	mustPanic(t, func() {
		_ = mustParseTime("  ")
	})
	mustPanic(t, func() {
		_ = mustParseTime("not-a-time")
	})

	mustPanic(t, func() {
		_ = marshalRawJSON(func() {})
	})

	mustPanic(t, func() {
		_ = mustPrettyJSON(func() {})
	})

	mustPanic(t, func() {
		_ = buildSuccessResponse("req-5", gateway.MessageFrame{
			Type:    gateway.FrameTypeAck,
			Action:  gateway.FrameActionPing,
			Payload: func() {},
		})
	})
}

func TestExitWithError(t *testing.T) {
	if os.Getenv("TEST_EXIT_WITH_ERROR") == "1" {
		exitWithError("boom", os.ErrNotExist)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestExitWithError")
	cmd.Env = append(os.Environ(), "TEST_EXIT_WITH_ERROR=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("exitWithError should terminate process with non-zero exit code")
	}

	content := string(output)
	if !strings.Contains(content, "boom") {
		t.Fatalf("stderr should contain message, got %q", content)
	}
	if !strings.Contains(content, os.ErrNotExist.Error()) {
		t.Fatalf("stderr should contain wrapped error, got %q", content)
	}
}

func mustPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic, got nil")
		}
	}()
	fn()
}
