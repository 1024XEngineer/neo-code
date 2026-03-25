package core

import (
	"strings"
	"testing"

	"go-llm-demo/internal/tui/services"
	"go-llm-demo/internal/tui/state"
)

func TestCaptureToolCallFromAssistantTextSupportsMixedOutput(t *testing.T) {
	content := "<think>\nI should inspect the workspace first.\n</think>\n我先查看当前工作区结构，确认放置 Go 程序的位置，避免覆盖现有文件。\n{\"tool\":\"list\",\"params\":{\"path\":\".\"}}"

	got, ok := captureToolCallFromAssistantText(content)
	if !ok {
		t.Fatal("expected tool call capture to succeed")
	}
	if got.Call.Tool != "list" {
		t.Fatalf("expected list tool, got %q", got.Call.Tool)
	}
	if path, _ := got.Call.Params["path"].(string); path != "." {
		t.Fatalf("expected path '.', got %+v", got.Call.Params)
	}
	if strings.Contains(got.CleanedResponse, "<think>") {
		t.Fatalf("expected think block to be removed, got %q", got.CleanedResponse)
	}
	if strings.Contains(got.CleanedResponse, `{"tool"`) {
		t.Fatalf("expected tool JSON to be removed from cleaned response, got %q", got.CleanedResponse)
	}
	if !strings.Contains(got.CleanedResponse, "我先查看当前工作区结构") {
		t.Fatalf("expected explanatory text to remain, got %q", got.CleanedResponse)
	}
}

func TestCaptureToolCallFromAssistantTextSupportsFencedJSON(t *testing.T) {
	content := "```json\n{\"tool\":\"read\",\"params\":{\"filePath\":\"README.md\"}}\n```"

	got, ok := captureToolCallFromAssistantText(content)
	if !ok {
		t.Fatal("expected fenced tool call to be captured")
	}
	if got.Call.Tool != "read" {
		t.Fatalf("expected read tool, got %q", got.Call.Tool)
	}
	if filePath, _ := got.Call.Params["filePath"].(string); filePath != "README.md" {
		t.Fatalf("expected filePath README.md, got %+v", got.Call.Params)
	}
	if got.CleanedResponse != "" {
		t.Fatalf("expected fenced JSON cleanup to remove empty wrapper, got %q", got.CleanedResponse)
	}
}

func TestCaptureToolCallFromAssistantTextRejectsTrailingNaturalLanguage(t *testing.T) {
	content := "{\"tool\":\"list\",\"params\":{\"path\":\".\"}}\n然后我会继续解释目录结构。"

	if _, ok := captureToolCallFromAssistantText(content); ok {
		t.Fatal("expected trailing natural language to prevent tool capture")
	}
}

func TestStreamDoneMsgExecutesToolCallFromMixedAssistantText(t *testing.T) {
	client := &fakeChatClient{}
	m := newTestModel(t, client)
	m.chat.Generating = true
	m.chat.Messages = []state.Message{{
		Role:      "assistant",
		Content:   "<think>\ninternal reasoning\n</think>\n我先看一下目录结构。\n{\"tool\":\"list\",\"params\":{\"path\":\".\"}}",
		Streaming: true,
	}}

	expected := &services.ToolResult{ToolName: "list", Success: true, Output: "ok"}
	executeToolCall = func(call services.ToolCall) *services.ToolResult {
		if call.Tool != "list" {
			t.Fatalf("expected list tool, got %q", call.Tool)
		}
		if path, _ := call.Params["path"].(string); path != "." {
			t.Fatalf("expected normalized path '.', got %+v", call.Params)
		}
		return expected
	}

	updated, cmd := m.Update(StreamDoneMsg{})
	got := updated.(Model)

	if !got.chat.ToolExecuting {
		t.Fatal("expected tool execution flag to be set")
	}
	if cmd == nil {
		t.Fatal("expected tool execution command")
	}
	if len(got.chat.Messages) != 2 {
		t.Fatalf("expected cleaned assistant message and tool status, got %d messages", len(got.chat.Messages))
	}
	if got.chat.Messages[0].Role != "assistant" {
		t.Fatalf("expected assistant message to remain first, got %+v", got.chat.Messages[0])
	}
	if strings.Contains(got.chat.Messages[0].Content, "<think>") {
		t.Fatalf("expected think block to be stripped, got %q", got.chat.Messages[0].Content)
	}
	if strings.Contains(got.chat.Messages[0].Content, `{"tool"`) {
		t.Fatalf("expected tool JSON to be stripped, got %q", got.chat.Messages[0].Content)
	}
	if !strings.Contains(got.chat.Messages[0].Content, "我先看一下目录结构") {
		t.Fatalf("expected explanatory text to remain, got %q", got.chat.Messages[0].Content)
	}
	if !strings.HasPrefix(got.chat.Messages[1].Content, toolStatusPrefix) {
		t.Fatalf("expected transient tool status, got %q", got.chat.Messages[1].Content)
	}

	msg := cmd()
	resultMsg, ok := msg.(ToolResultMsg)
	if !ok {
		t.Fatalf("expected ToolResultMsg, got %T", msg)
	}
	if resultMsg.Result != expected {
		t.Fatalf("expected tool result to round-trip, got %+v", resultMsg.Result)
	}
}
