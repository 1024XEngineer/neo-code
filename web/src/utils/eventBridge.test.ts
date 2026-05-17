import { describe, it, expect, beforeEach, vi } from "vitest";
import { waitFor } from "@testing-library/react";
import { useChatStore } from "@/stores/useChatStore";
import { useGatewayStore } from "@/stores/useGatewayStore";
import { useSessionStore } from "@/stores/useSessionStore";
import { useRuntimeInsightStore } from "@/stores/useRuntimeInsightStore";
import { useUIStore } from "@/stores/useUIStore";
import { handleGatewayEvent, resetEventBridgeCursors } from "./eventBridge";
import { EventType } from "@/api/protocol";

function createMockGatewayAPI(overrides: Record<string, unknown> = {}) {
  return {
    listSessions: async () => ({ payload: { sessions: [] } }),
    loadSession: async () => ({ payload: { messages: [] } }),
    bindStream: async () => ({}),
    checkpointDiff: async () => ({
      payload: { checkpoint_id: "cp", files: {}, patch: "" },
    }),
    ...overrides,
  } as any;
}

beforeEach(() => {
  resetEventBridgeCursors();
  useChatStore.setState({
    messages: [],
    isGenerating: false,
    isCompacting: false,
    compactMode: "",
    compactMessage: "",
    streamingMessageId: "",
    permissionRequests: [],
    pendingUserQuestion: null,
    tokenUsage: null,
    phase: "",
    stopReason: "",
  } as any);
  useGatewayStore.setState({
    connectionState: "disconnected",
    currentRunId: "",
    token: "",
    authenticated: false,
  } as any);
  useRuntimeInsightStore.getState().reset();
  useSessionStore.setState({ currentSessionId: "" } as any);
  useUIStore.setState({
    toasts: [],
    fileChanges: [],
    isRestoringCheckpoint: false,
    checkpointRollbackUndo: null,
  } as any);
});

describe("eventBridge", () => {
  it("AgentChunk adds assistant message and appends text", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.AgentChunk,
        payload: {
          payload: {
            runtime_event_type: EventType.AgentChunk,
            payload: "hello",
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const msgs = useChatStore.getState().messages;
    expect(msgs).toHaveLength(1);
    expect(msgs[0].role).toBe("assistant");
    expect(msgs[0].content).toBe("hello");
  });

  it("AgentChunk appends to existing streaming message", () => {
    const api = createMockGatewayAPI();
    const store = useChatStore.getState();
    store.addMessage({
      id: "s1",
      role: "assistant",
      content: "He",
      type: "text",
      timestamp: 1,
    });
    store.setStreamingMessageId("s1");

    handleGatewayEvent(
      {
        type: EventType.AgentChunk,
        payload: {
          payload: { runtime_event_type: EventType.AgentChunk, payload: "llo" },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().messages[0].content).toBe("Hello");
  });

  it("AgentDone finalizes message from parts array", () => {
    const api = createMockGatewayAPI();
    const store = useChatStore.getState();
    store.addMessage({
      id: "s1",
      role: "assistant",
      content: "He",
      type: "text",
      timestamp: 1,
    });
    store.setStreamingMessageId("s1");
    store.setGenerating(true);

    handleGatewayEvent(
      {
        type: EventType.AgentDone,
        payload: {
          payload: {
            runtime_event_type: EventType.AgentDone,
            payload: { parts: [{ text: "Hello world" }] },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().messages[0].content).toBe("Hello world");
    expect(useChatStore.getState().isGenerating).toBe(false);
    expect(useChatStore.getState().streamingMessageId).toBe("");
  });

  it("AgentDone falls back to content field when parts missing", () => {
    const api = createMockGatewayAPI();
    const store = useChatStore.getState();
    store.addMessage({
      id: "s1",
      role: "assistant",
      content: "",
      type: "text",
      timestamp: 1,
    });
    store.setStreamingMessageId("s1");
    store.setGenerating(true);

    handleGatewayEvent(
      {
        type: EventType.AgentDone,
        payload: {
          payload: {
            runtime_event_type: EventType.AgentDone,
            payload: { content: "fallback" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().messages[0].content).toBe("fallback");
  });

  it("InputNormalizes sets currentSessionId and currentRunId", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.InputNormalized,
        payload: {
          payload: {
            runtime_event_type: EventType.InputNormalized,
            payload: { session_id: "sess-1", run_id: "run-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useSessionStore.getState().currentSessionId).toBe("sess-1");
    expect(useGatewayStore.getState().currentRunId).toBe("run-1");
  });

  it("UserQuestionRequested stores pending user question", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.UserQuestionRequested,
        payload: {
          payload: {
            runtime_event_type: EventType.UserQuestionRequested,
            payload: {
              request_id: "ask-1",
              question_id: "q-1",
              title: "Pick one",
              description: "Choose an option",
              kind: "single_choice",
              options: [{ label: "A" }, { label: "B" }],
              required: true,
              allow_skip: true,
              max_choices: 1,
              timeout_sec: 120,
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const pending = useChatStore.getState().pendingUserQuestion;
    expect(pending?.request_id).toBe("ask-1");
    expect(pending?.kind).toBe("single_choice");
    expect(pending?.allow_skip).toBe(true);
  });

  it("UserQuestionResolved events clear pending user question by request id", () => {
    const api = createMockGatewayAPI();
    useChatStore.getState().setPendingUserQuestion({
      request_id: "ask-2",
      question_id: "q-2",
      title: "Title",
      description: "",
      kind: "text",
      required: true,
      allow_skip: false,
    });

    handleGatewayEvent(
      {
        type: EventType.UserQuestionAnswered,
        payload: {
          payload: {
            runtime_event_type: EventType.UserQuestionAnswered,
            payload: { request_id: "ask-2", status: "answered" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().pendingUserQuestion).toBeNull();
  });

  it("ToolStart adds a tool call message", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "read_file",
              id: "tc1",
              arguments: '{"path":"/a"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const msgs = useChatStore.getState().messages;
    expect(msgs).toHaveLength(1);
    expect(msgs[0].type).toBe("tool_call");
    expect(msgs[0].toolName).toBe("read_file");
  });

  it("ToolStart file placeholders are pending before tool diff arrives", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc-pending",
              arguments: '{"path":"pending.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "pending.txt");
    expect(change?.status).toBe("pending");
  });

  it("ToolStart file placeholders clear stale rollback undo state", () => {
    const api = createMockGatewayAPI();
    useUIStore.setState({
      checkpointRollbackUndo: {
        sessionId: "sess-1",
        checkpointId: "cp-rollback",
        guardCheckpointId: "guard-rollback",
        paths: ["old.txt"],
        status: "idle",
      },
    } as any);

    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc-new-change",
              arguments: '{"path":"new-change.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useUIStore.getState().checkpointRollbackUndo).toBeNull();
    expect(
      useUIStore
        .getState()
        .fileChanges.some((entry) => entry.path === "new-change.txt"),
    ).toBe(true);
  });

  it("ToolResult updates an existing tool call message", () => {
    const api = createMockGatewayAPI();
    // 先触发 ToolStart 创建工具消息
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "read_file",
              id: "tc1",
              arguments: '{"path":"/a"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    // 再触发 ToolResult 更新结果
    handleGatewayEvent(
      {
        type: EventType.ToolResult,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolResult,
            payload: {
              tool_call_id: "tc1",
              content: "file contents",
              is_error: false,
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const msgs = useChatStore.getState().messages;
    expect(msgs).toHaveLength(1);
    expect(msgs[0].role).toBe("tool");
    expect(msgs[0].toolResult).toBe("file contents");
    expect(msgs[0].toolStatus).toBe("done");
  });

  it("ToolResult falls back to settling the latest running tool message when id is missing", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: { name: "read_file", id: "tc-fallback", arguments: "{}" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    handleGatewayEvent(
      {
        type: EventType.ToolResult,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolResult,
            payload: { content: "ok without id" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const msgs = useChatStore.getState().messages;
    expect(msgs[0].toolStatus).toBe("done");
    expect(msgs[0].toolResult).toBe("ok without id");
  });

  it("AgentDone settles any dangling running tool call to done", () => {
    const api = createMockGatewayAPI();
    const chatStore = useChatStore.getState();
    chatStore.addMessage({
      id: "tool1",
      role: "tool",
      type: "tool_call",
      content: "",
      toolName: "bash",
      toolCallId: "tc1",
      toolStatus: "running",
      timestamp: Date.now(),
    });

    handleGatewayEvent(
      {
        type: EventType.AgentDone,
        payload: {
          payload: {
            runtime_event_type: EventType.AgentDone,
            payload: { content: "done" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().messages[0].toolStatus).toBe("done");
  });

  it("StopReasonDecided keeps running tool calls unresolved for non-fatal reasons", () => {
    const api = createMockGatewayAPI();
    useChatStore.getState().addMessage({
      id: "tool-running-nonfatal",
      role: "tool",
      type: "tool_call",
      content: "",
      toolName: "bash",
      toolCallId: "tc-nonfatal",
      toolStatus: "running",
      timestamp: Date.now(),
    });

    handleGatewayEvent(
      {
        type: EventType.StopReasonDecided,
        payload: {
          payload: {
            runtime_event_type: EventType.StopReasonDecided,
            payload: { reason: "user_interrupt" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().messages[0].toolStatus).toBe("running");
  });

  it("StopReasonDecided marks running tool calls as error on fatal_error", () => {
    const api = createMockGatewayAPI();
    useChatStore.getState().addMessage({
      id: "tool-running-fatal",
      role: "tool",
      type: "tool_call",
      content: "",
      toolName: "bash",
      toolCallId: "tc-fatal",
      toolStatus: "running",
      timestamp: Date.now(),
    });

    handleGatewayEvent(
      {
        type: EventType.StopReasonDecided,
        payload: {
          payload: {
            runtime_event_type: EventType.StopReasonDecided,
            payload: { reason: "fatal_error", detail: "fatal" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().messages[0].toolStatus).toBe("error");
  });

  it("RunCanceled does not convert running tool calls to done", () => {
    const api = createMockGatewayAPI();
    useChatStore.getState().addMessage({
      id: "tool-running-canceled",
      role: "tool",
      type: "tool_call",
      content: "",
      toolName: "bash",
      toolCallId: "tc-canceled",
      toolStatus: "running",
      timestamp: Date.now(),
    });

    handleGatewayEvent(
      {
        type: EventType.RunCanceled,
        payload: {
          payload: { runtime_event_type: EventType.RunCanceled, payload: {} },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().messages[0].toolStatus).toBe("running");
  });

  it("BudgetChecked updates runtime insight budget state", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.BudgetChecked,
        payload: {
          payload: {
            runtime_event_type: EventType.BudgetChecked,
            payload: {
              attempt_seq: 1,
              request_hash: "h1",
              action: "allow",
              estimated_input_tokens: 80,
              prompt_budget: 100,
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useRuntimeInsightStore.getState().budgetChecked?.action).toBe(
      "allow",
    );
    expect(useRuntimeInsightStore.getState().budgetUsageRatio).toBe(0.8);
  });

  it("CompactStart sets persistent compact state without a toast", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.CompactStart,
        payload: {
          payload: {
            runtime_event_type: EventType.CompactStart,
            payload: "manual",
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().isCompacting).toBe(true);
    expect(useChatStore.getState().compactMode).toBe("manual");
    expect(useChatStore.getState().compactMessage).toBe(
      "Compacting context...",
    );
    expect(useUIStore.getState().toasts).toHaveLength(0);
  });

  it("CompactStart uses proactive mode copy without a toast", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.CompactStart,
        payload: {
          payload: {
            runtime_event_type: EventType.CompactStart,
            payload: { trigger_mode: "proactive" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().isCompacting).toBe(true);
    expect(useChatStore.getState().compactMode).toBe("proactive");
    expect(useChatStore.getState().compactMessage).toBe(
      "Context is near the limit. Auto-compacting...",
    );
    expect(useUIStore.getState().toasts).toHaveLength(0);
  });

  it("CompactApplied clears compact state and shows completion toast", () => {
    const api = createMockGatewayAPI();
    useChatStore
      .getState()
      .startCompacting("manual", "Compacting context...");

    handleGatewayEvent(
      {
        type: EventType.CompactApplied,
        payload: {
          payload: {
            runtime_event_type: EventType.CompactApplied,
            payload: { applied: true },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().isCompacting).toBe(false);
    expect(useUIStore.getState().toasts.at(-1)?.message).toBe(
      "Context compacted",
    );
  });

  it("CompactApplied for automatic modes clears compact state without completion toast", () => {
    const api = createMockGatewayAPI();
    useChatStore
      .getState()
      .startCompacting("proactive", "Context is near the limit. Auto-compacting...");

    handleGatewayEvent(
      {
        type: EventType.CompactApplied,
        payload: {
          payload: {
            runtime_event_type: EventType.CompactApplied,
            payload: { TriggerMode: "proactive", Applied: true },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().isCompacting).toBe(false);
    expect(useUIStore.getState().toasts).toHaveLength(0);
  });

  it("CompactError clears compact state and uses payload message", () => {
    const api = createMockGatewayAPI();
    useChatStore
      .getState()
      .startCompacting("manual", "Compacting context...");

    handleGatewayEvent(
      {
        type: EventType.CompactError,
        payload: {
          payload: {
            runtime_event_type: EventType.CompactError,
            payload: { message: "compact timed out" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().isCompacting).toBe(false);
    expect(useUIStore.getState().toasts.at(-1)?.message).toBe(
      "compact timed out",
    );
  });

  it("CompactError for automatic modes includes automatic compact context", () => {
    const api = createMockGatewayAPI();
    useChatStore
      .getState()
      .startCompacting("reactive", "Model reported context too long. Compacting and retrying...");

    handleGatewayEvent(
      {
        type: EventType.CompactError,
        payload: {
          payload: {
            runtime_event_type: EventType.CompactError,
            payload: { TriggerMode: "reactive", Message: "context still too long" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useChatStore.getState().isCompacting).toBe(false);
    expect(useUIStore.getState().toasts.at(-1)?.message).toBe(
      "Auto context compaction failed: context still too long",
    );
  });

  it("VerificationStageFinished upserts verifier status", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.VerificationStageFinished,
        payload: {
          payload: {
            runtime_event_type: EventType.VerificationStageFinished,
            payload: { name: "test", status: "passed", summary: "ok" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(
      useRuntimeInsightStore.getState().verificationStages.test.status,
    ).toBe("passed");
  });

  it("AcceptanceDecided stores acceptance decision", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.AcceptanceDecided,
        payload: {
          payload: {
            runtime_event_type: EventType.AcceptanceDecided,
            payload: { status: "accepted", user_visible_summary: "done" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useRuntimeInsightStore.getState().acceptanceDecision?.status).toBe(
      "accepted",
    );
  });

  it("TodoSnapshotUpdated stores todo snapshot", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.TodoSnapshotUpdated,
        payload: {
          payload: {
            runtime_event_type: EventType.TodoSnapshotUpdated,
            payload: {
              action: "snapshot",
              items: [
                {
                  id: "t1",
                  content: "x",
                  status: "blocked",
                  required: true,
                  blocked_reason: "wait",
                  revision: 1,
                },
              ],
              summary: {
                total: 1,
                required_total: 1,
                required_completed: 0,
                required_failed: 0,
                required_open: 1,
              },
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(
      useRuntimeInsightStore.getState().todoSnapshot?.items?.[0].blocked_reason,
    ).toBe("wait");
  });

  it("TodoSnapshotUpdated does NOT clear TodoConflict", () => {
    const api = createMockGatewayAPI();
    // First, trigger a conflict
    handleGatewayEvent(
      {
        type: EventType.TodoConflict,
        payload: {
          payload: {
            runtime_event_type: EventType.TodoConflict,
            payload: {
              action: "update",
              reason: "revision_conflict",
              items: [
                {
                  id: "t1",
                  content: "x",
                  status: "in_progress",
                  required: true,
                  revision: 1,
                },
              ],
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe(
      "revision_conflict",
    );

    // Then, snapshot update should preserve conflict
    handleGatewayEvent(
      {
        type: EventType.TodoSnapshotUpdated,
        payload: {
          payload: {
            runtime_event_type: EventType.TodoSnapshotUpdated,
            payload: {
              action: "snapshot",
              items: [
                {
                  id: "t1",
                  content: "x",
                  status: "in_progress",
                  required: true,
                  revision: 1,
                },
              ],
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe(
      "revision_conflict",
    );
    expect(useRuntimeInsightStore.getState().todoSnapshot?.items?.[0].id).toBe(
      "t1",
    );
  });

  it("TodoUpdated clears TodoConflict", () => {
    const api = createMockGatewayAPI();
    // Set conflict first
    handleGatewayEvent(
      {
        type: EventType.TodoConflict,
        payload: {
          payload: {
            runtime_event_type: EventType.TodoConflict,
            payload: { action: "update", reason: "revision_conflict" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    expect(useRuntimeInsightStore.getState().todoConflict).not.toBeNull();

    // Successful update should clear conflict
    handleGatewayEvent(
      {
        type: EventType.TodoUpdated,
        payload: {
          payload: {
            runtime_event_type: EventType.TodoUpdated,
            payload: {
              action: "update",
              items: [
                {
                  id: "t1",
                  content: "x",
                  status: "completed",
                  required: true,
                  revision: 2,
                },
              ],
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useRuntimeInsightStore.getState().todoConflict).toBeNull();
    expect(useRuntimeInsightStore.getState().todoSnapshot?.items?.[0].id).toBe(
      "t1",
    );
  });

  it("TodoConflict revision_conflict does NOT show toast", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.TodoConflict,
        payload: {
          payload: {
            runtime_event_type: EventType.TodoConflict,
            payload: { action: "update", reason: "revision_conflict" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe(
      "revision_conflict",
    );
    expect(useUIStore.getState().toasts).toHaveLength(0);
  });

  it("TodoConflict invalid_transition does NOT show toast", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.TodoConflict,
        payload: {
          payload: {
            runtime_event_type: EventType.TodoConflict,
            payload: { action: "update", reason: "invalid_transition" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe(
      "invalid_transition",
    );
    expect(useUIStore.getState().toasts).toHaveLength(0);
  });

  it("TodoConflict invalid_arguments shows info toast", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.TodoConflict,
        payload: {
          payload: {
            runtime_event_type: EventType.TodoConflict,
            payload: { action: "update", reason: "invalid_arguments" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe(
      "invalid_arguments",
    );
    expect(useUIStore.getState().toasts).toHaveLength(1);
    expect(useUIStore.getState().toasts[0].type).toBe("info");
    expect(useUIStore.getState().toasts[0].message).toContain(
      "invalid_arguments",
    );
  });

  it("Checkpoint events are stored in runtime insight state", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp1",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "abc",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(useRuntimeInsightStore.getState().checkpointEvents[0]).toMatchObject(
      { checkpoint_id: "cp1" },
    );
  });

  it("CheckpointRestored reloads state for current session and clears stale file changes first", async () => {
    const loadSession = vi.fn(async () => ({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [{ role: "assistant", content: "after restore" }],
      },
    }));
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);
    useUIStore.setState({
      fileChanges: [
        {
          id: "fc-1",
          path: "stale.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
        },
      ],
    } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp1",
              session_id: "sess-1",
              guard_checkpoint_id: "guard-1",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    expect(loadSession).toHaveBeenCalledWith("sess-1");
    expect(useUIStore.getState().fileChanges).toHaveLength(0);
    expect(useUIStore.getState().checkpointRollbackUndo).toBeNull();
  });

  it("baseline CheckpointRestored removes only restored file changes without reloading the session", async () => {
    const loadSession = vi.fn(async () => ({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [{ role: "assistant", content: "after restore" }],
      },
    }));
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);
    useUIStore.setState({
      isRestoringCheckpoint: true,
      fileChanges: [
        {
          id: "fc-1",
          path: "src/a.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
        },
        {
          id: "fc-2",
          path: "src/b.txt",
          status: "modified",
          additions: 2,
          deletions: 1,
        },
      ],
    } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp1",
              session_id: "sess-1",
              guard_checkpoint_id: "guard-baseline-1",
              mode: "baseline",
              paths: ["./src/a.txt"],
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();

    expect(loadSession).not.toHaveBeenCalled();
    expect(useUIStore.getState().isRestoringCheckpoint).toBe(false);
    expect(useUIStore.getState().checkpointRollbackUndo).toBeNull();
    expect(
      useUIStore.getState().fileChanges.map((entry) => entry.path),
    ).toEqual(["src/b.txt"]);
  });

  it("baseline CheckpointRestored removes all paths from rollback all events", async () => {
    const loadSession = vi.fn();
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);
    useUIStore.setState({
      isRestoringCheckpoint: true,
      fileChanges: [
        {
          id: "fc-1",
          path: "src/a.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
        },
        {
          id: "fc-2",
          path: "src/b.txt",
          status: "added",
          additions: 2,
          deletions: 0,
        },
      ],
    } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp1",
              session_id: "sess-1",
              guard_checkpoint_id: "guard-baseline-all",
              mode: "baseline",
              paths: ["src/a.txt", "src/b.txt"],
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();

    expect(loadSession).not.toHaveBeenCalled();
    expect(useUIStore.getState().fileChanges).toHaveLength(0);
    expect(useUIStore.getState().checkpointRollbackUndo).toMatchObject({
      sessionId: "sess-1",
      checkpointId: "cp1",
      guardCheckpointId: "guard-baseline-all",
      paths: ["src/a.txt", "src/b.txt"],
      status: "idle",
    });
  });

  it("baseline CheckpointRestored invalidates in-flight run-scoped file change refreshes", async () => {
    let resolveDiff: ((value: unknown) => void) | undefined;
    const checkpointDiff = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveDiff = resolve;
        }),
    );
    const loadSession = vi.fn();
    const api = createMockGatewayAPI({ checkpointDiff, loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);
    useGatewayStore.setState({ currentRunId: "run-1" } as any);
    useUIStore.setState({
      fileChanges: [
        {
          id: "fc-1",
          path: "src/a.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
        },
      ],
    } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-end",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "end_of_turn",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    expect(checkpointDiff).toHaveBeenCalled();

    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp-end",
              session_id: "sess-1",
              guard_checkpoint_id: "",
              mode: "baseline",
              paths: ["src/a.txt"],
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    resolveDiff?.({
      payload: {
        checkpoint_id: "cp-end",
        files: { modified: ["src/a.txt"] },
        patch: "--- a/src/a.txt\n+++ b/src/a.txt\n@@ -1 +1 @@\n-old\n+new\n",
      },
    });
    await Promise.resolve();
    await Promise.resolve();

    expect(loadSession).not.toHaveBeenCalled();
    expect(useUIStore.getState().fileChanges).toHaveLength(0);
  });

  it("CheckpointRestored invalidates in-flight run-scoped file change refreshes", async () => {
    let resolveDiff: ((value: unknown) => void) | undefined;
    const checkpointDiff = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveDiff = resolve;
        }),
    );
    const loadSession = vi.fn(async () => ({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [{ role: "assistant", content: "after restore" }],
      },
    }));
    const api = createMockGatewayAPI({ checkpointDiff, loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);
    useGatewayStore.setState({ currentRunId: "run-1" } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-end",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "end_of_turn",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    expect(checkpointDiff).toHaveBeenCalled();

    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp-restored",
              session_id: "sess-1",
              guard_checkpoint_id: "guard-1",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-restore",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    resolveDiff?.({
      payload: {
        checkpoint_id: "cp-end",
        files: { modified: ["stale.txt"] },
        patch: "--- a/stale.txt\n+++ b/stale.txt\n@@ -1 +1 @@\n-old\n+new\n",
      },
    });
    await Promise.resolve();
    await Promise.resolve();

    expect(useUIStore.getState().fileChanges).toHaveLength(0);
  });

  it("CheckpointRestored does not reload when event session differs from current session", async () => {
    const loadSession = vi.fn(async () => ({
      payload: { id: "sess-other", messages: [] },
    }));
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-current" } as any);
    useUIStore.setState({
      fileChanges: [
        {
          id: "fc-1",
          path: "keep.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
        },
      ],
    } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp1",
              session_id: "sess-other",
              guard_checkpoint_id: "guard-1",
            },
          },
        },
        session_id: "sess-other",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    expect(loadSession).not.toHaveBeenCalled();
    expect(useUIStore.getState().fileChanges).toHaveLength(1);
  });

  it("CheckpointUndoRestore reloads current session with the same restore-sync flow", async () => {
    const loadSession = vi.fn(async () => ({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [{ role: "assistant", content: "after undo restore" }],
      },
    }));
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);
    useUIStore.setState({
      fileChanges: [
        {
          id: "fc-1",
          path: "stale.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
        },
      ],
    } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointUndoRestore,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointUndoRestore,
            payload: { session_id: "sess-1", guard_checkpoint_id: "guard-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    expect(loadSession).toHaveBeenCalledWith("sess-1");
    expect(useUIStore.getState().fileChanges).toHaveLength(0);
    expect(useUIStore.getState().checkpointRollbackUndo).toBeNull();
    expect(useUIStore.getState().toasts.at(-1)).toMatchObject({
      message: "Checkpoint restore undone",
      type: "success",
    });
  });

  it("CheckpointUndoRestore clears rollback undo state without reloading session", async () => {
    const loadSession = vi.fn();
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);
    useUIStore.setState({
      checkpointRollbackUndo: {
        sessionId: "sess-1",
        checkpointId: "cp-rollback",
        guardCheckpointId: "guard-rollback",
        paths: ["src/a.txt"],
        status: "undoing",
      },
      fileChanges: [
        {
          id: "fc-1",
          path: "src/a.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
        },
      ],
    } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointUndoRestore,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointUndoRestore,
            payload: {
              session_id: "sess-1",
              guard_checkpoint_id: "guard-rollback",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();

    expect(loadSession).not.toHaveBeenCalled();
    expect(useUIStore.getState().checkpointRollbackUndo).toBeNull();
    expect(useUIStore.getState().fileChanges).toHaveLength(0);
    expect(useUIStore.getState().toasts.at(-1)).toMatchObject({
      message: "Rollback undo completed",
      type: "success",
    });
  });

  it("CheckpointUndoRestore reloads session when rollback undo guard id is stale", async () => {
    const loadSession = vi.fn(async () => ({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [{ role: "assistant", content: "after normal undo" }],
      },
    }));
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);
    useUIStore.setState({
      checkpointRollbackUndo: {
        sessionId: "sess-1",
        checkpointId: "cp-rollback",
        guardCheckpointId: "guard-stale",
        paths: ["src/a.txt"],
        status: "idle",
      },
      fileChanges: [
        {
          id: "fc-1",
          path: "src/a.txt",
          status: "modified",
          additions: 1,
          deletions: 0,
        },
      ],
    } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointUndoRestore,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointUndoRestore,
            payload: {
              session_id: "sess-1",
              guard_checkpoint_id: "guard-normal",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    expect(loadSession).toHaveBeenCalledWith("sess-1");
    expect(useUIStore.getState().checkpointRollbackUndo).toBeNull();
    expect(useUIStore.getState().fileChanges).toHaveLength(0);
    expect(useUIStore.getState().toasts.at(-1)).toMatchObject({
      message: "Checkpoint restore undone",
      type: "success",
    });
  });

  it("VerificationStarted creates a verification ChatMessage", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.VerificationStarted,
        payload: {
          payload: {
            runtime_event_type: EventType.VerificationStarted,
            payload: { completion_passed: true },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const verifyMsg = useChatStore
      .getState()
      .messages.find((m) => m.type === "verification");
    expect(verifyMsg).toBeDefined();
    expect(verifyMsg?.verificationData?.status).toBe("running");
    expect(useRuntimeInsightStore.getState().verificationHistory).toHaveLength(
      1,
    );
  });

  it("VerificationStageFinished updates the verification message", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.VerificationStarted,
        payload: {
          payload: {
            runtime_event_type: EventType.VerificationStarted,
            payload: { completion_passed: true },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.VerificationStageFinished,
        payload: {
          payload: {
            runtime_event_type: EventType.VerificationStageFinished,
            payload: { name: "lint", status: "passed", summary: "all good" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const verifyMsg = useChatStore
      .getState()
      .messages.find((m) => m.type === "verification");
    expect(verifyMsg?.verificationData?.stages.lint.status).toBe("passed");
    expect(verifyMsg?.verificationData?.stages.lint.summary).toBe("all good");
  });

  it("VerificationFinished updates history and chat message", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.VerificationStarted,
        payload: {
          payload: {
            runtime_event_type: EventType.VerificationStarted,
            payload: { completion_passed: true },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.VerificationFinished,
        payload: {
          payload: {
            runtime_event_type: EventType.VerificationFinished,
            payload: { acceptance_status: "accepted" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const verifyMsg = useChatStore
      .getState()
      .messages.find((m) => m.type === "verification");
    expect(verifyMsg?.verificationData?.status).toBe("finished");
    expect(
      useRuntimeInsightStore.getState().verificationHistory[0].status,
    ).toBe("finished");
  });

  it("AcceptanceDecided creates an acceptance ChatMessage", () => {
    const api = createMockGatewayAPI();
    handleGatewayEvent(
      {
        type: EventType.AcceptanceDecided,
        payload: {
          payload: {
            runtime_event_type: EventType.AcceptanceDecided,
            payload: { status: "accepted", user_visible_summary: "looks good" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const msg = useChatStore
      .getState()
      .messages.find((m) => m.type === "acceptance");
    expect(msg).toBeDefined();
    expect(msg?.acceptanceData?.status).toBe("accepted");
    expect(msg?.acceptanceData?.user_visible_summary).toBe("looks good");
    expect(useRuntimeInsightStore.getState().acceptanceDecision?.status).toBe(
      "accepted",
    );
  });

  it("CheckpointCreated only records runtime insight and does not decorate completed tool calls", () => {
    const api = createMockGatewayAPI();

    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: { name: "write_file", id: "tc1", arguments: "{}" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolResult,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolResult,
            payload: { tool_call_id: "tc1", content: "ok" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp1",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "abc",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const toolMsg = useChatStore
      .getState()
      .messages.find((m) => m.type === "tool_call");
    expect((toolMsg as any)?.checkpointId).toBeUndefined();
    expect((toolMsg as any)?.checkpointStatus).toBeUndefined();
    expect(useRuntimeInsightStore.getState().checkpointEvents[0]).toMatchObject({
      checkpoint_id: "cp1",
      reason: "pre_write",
    });
  });

  it("CheckpointCreated with pre_restore_guard does not override latest rollback baseline", () => {
    const api = createMockGatewayAPI();

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-base",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "abc",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-guard",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "abc",
              reason: "pre_restore_guard",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"baseline.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const baselineChange = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "baseline.txt");
    expect(baselineChange?.checkpoint_id).toBe("cp-base");
  });

  it("clearMessages resets eventBridge cursors so new session does not inherit prior checkpoint", () => {
    const api = createMockGatewayAPI();

    // 会话 A:tool_call + checkpoint,使 _latestCheckpointId=cp_old
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tcA1",
              arguments: '{"path":"a.txt"}',
            },
          },
        },
        session_id: "sess-A",
        run_id: "run-A",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolResult,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolResult,
            payload: { tool_call_id: "tcA1", content: "ok" },
          },
        },
        session_id: "sess-A",
        run_id: "run-A",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp_old",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "abc",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-A",
        run_id: "run-A",
      },
      api,
    );

    // 同一会话内的下一次写文件应继承 cp_old(确认游标确实已被设置)
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tcA2",
              arguments: '{"path":"a2.txt"}',
            },
          },
        },
        session_id: "sess-A",
        run_id: "run-A",
      },
      api,
    );
    const inheritedChange = useUIStore
      .getState()
      .fileChanges.find((c) => c.path === "a2.txt");
    expect(inheritedChange?.checkpoint_id).toBe("cp_old");

    // 模拟切换会话:clearMessages 是 switchSession/createSession/prepareNewChat 的统一入口
    useUIStore.getState().clearFileChanges();
    useChatStore.getState().clearMessages();

    // 会话 B:再触发一次写文件,但不再有 CheckpointCreated 事件
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tcB",
              arguments: '{"path":"b.txt"}',
            },
          },
        },
        session_id: "sess-B",
        run_id: "run-B",
      },
      api,
    );

    const newChange = useUIStore
      .getState()
      .fileChanges.find((c) => c.path === "b.txt");
    expect(newChange).toBeDefined();
    expect(newChange?.checkpoint_id).toBeUndefined();
  });

  it("replaces transient tool diffs with run-scoped checkpoint diff on end-of-turn checkpoint", async () => {
    const checkpointDiff = vi.fn(async () => ({
      payload: {
        checkpoint_id: "cp2",
        files: { modified: ["a.txt"] },
        patch:
          "--- a/a.txt\n+++ b/a.txt\n@@ -1,3 +1,3 @@\n line 1\n-A\n+C\n line 3\n@@ -10,3 +10,3 @@\n line 10\n-B\n+D\n line 12\n",
      },
    }));
    const api = createMockGatewayAPI({ checkpointDiff });

    handleGatewayEvent(
      {
        type: EventType.InputNormalized,
        payload: {
          payload: {
            runtime_event_type: EventType.InputNormalized,
            payload: { session_id: "sess-1", run_id: "run-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"a.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolDiff,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolDiff,
            payload: {
              tool_call_id: "tc1",
              tool_name: "filesystem_write_file",
              file_path: "a.txt",
              diff: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-A\n+B\n",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolDiff,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolDiff,
            payload: {
              tool_call_id: "tc2",
              tool_name: "filesystem_write_file",
              file_path: "a.txt",
              diff: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-B\n+C\n",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(
      useUIStore
        .getState()
        .fileChanges[0]?.hunks?.[0]?.lines.map((line) => line.content),
    ).toEqual(["@@ -1 +1 @@", "B", "C"]);

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp2",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "end_of_turn",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    expect(checkpointDiff).toHaveBeenCalledWith({
      session_id: "sess-1",
      run_id: "run-1",
      checkpoint_id: "cp2",
      scope: "run",
    });
    const changes = useUIStore.getState().fileChanges;
    expect(changes).toHaveLength(1);
    expect(changes[0]).toMatchObject({
      path: "a.txt",
      status: "modified",
      additions: 2,
      deletions: 2,
    });
    expect(changes[0].hunks).toHaveLength(2);
    expect(changes[0].hunks?.[0]?.lines.map((line) => line.content)).toEqual([
      "@@ -1,3 +1,3 @@",
      "line 1",
      "A",
      "C",
      "line 3",
    ]);
    expect(changes[0].hunks?.[1]?.lines.map((line) => line.content)).toEqual([
      "@@ -10,3 +10,3 @@",
      "line 10",
      "B",
      "D",
      "line 12",
    ]);
  });

  it("run-scoped checkpoint diff clears stale rollback undo when new changes are returned", async () => {
    const checkpointDiff = vi.fn(async () => ({
      payload: {
        checkpoint_id: "cp-new",
        files: { modified: ["fresh.txt"] },
        patch: "--- a/fresh.txt\n+++ b/fresh.txt\n@@ -1 +1 @@\n-old\n+new\n",
      },
    }));
    const api = createMockGatewayAPI({ checkpointDiff });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);
    useGatewayStore.setState({ currentRunId: "run-1" } as any);
    useUIStore.setState({
      checkpointRollbackUndo: {
        sessionId: "sess-1",
        checkpointId: "cp-rollback",
        guardCheckpointId: "guard-rollback",
        paths: ["old.txt"],
        status: "idle",
      },
    } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-new",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "end_of_turn",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    expect(useUIStore.getState().checkpointRollbackUndo).toBeNull();
    expect(
      useUIStore.getState().fileChanges.map((entry) => entry.path),
    ).toEqual(["fresh.txt"]);
  });

  it("run-scoped checkpoint diff keeps rollback undo when no changes are returned", async () => {
    const checkpointDiff = vi.fn(async () => ({
      payload: {
        checkpoint_id: "cp-empty",
        files: {},
        patch: "",
      },
    }));
    const api = createMockGatewayAPI({ checkpointDiff });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);
    useGatewayStore.setState({ currentRunId: "run-1" } as any);
    useUIStore.setState({
      checkpointRollbackUndo: {
        sessionId: "sess-1",
        checkpointId: "cp-rollback",
        guardCheckpointId: "guard-rollback",
        paths: ["old.txt"],
        status: "idle",
      },
    } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-empty",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "end_of_turn",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    expect(useUIStore.getState().checkpointRollbackUndo).toMatchObject({
      checkpointId: "cp-rollback",
      guardCheckpointId: "guard-rollback",
    });
    expect(useUIStore.getState().fileChanges).toHaveLength(0);
  });

  it("does not let run diff prev_checkpoint_id overwrite per-file rollback checkpoint", async () => {
    const checkpointDiff = vi.fn(async () => ({
      payload: {
        checkpoint_id: "cp-latest",
        prev_checkpoint_id: "cp-authoritative",
        files: { modified: ["a.txt"] },
        patch: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n",
      },
    }));
    const api = createMockGatewayAPI({ checkpointDiff });

    handleGatewayEvent(
      {
        type: EventType.InputNormalized,
        payload: {
          payload: {
            runtime_event_type: EventType.InputNormalized,
            payload: { session_id: "sess-1", run_id: "run-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-base",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"a.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-latest",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "end_of_turn",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "a.txt");
    expect(change?.checkpoint_id).toBe("cp-base");
  });

  it("uses checkpoint diff file_entries rollback checkpoint id when provided", async () => {
    const checkpointDiff = vi.fn(async () => ({
      payload: {
        checkpoint_id: "cp-latest",
        files: { modified: ["a.txt"] },
        file_entries: [
          {
            path: "a.txt",
            kind: "modified",
            rollback_checkpoint_id: "cp-backend-baseline",
            can_rollback: true,
          },
        ],
        patch: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n",
      },
    }));
    const api = createMockGatewayAPI({ checkpointDiff });

    handleGatewayEvent(
      {
        type: EventType.InputNormalized,
        payload: {
          payload: {
            runtime_event_type: EventType.InputNormalized,
            payload: { session_id: "sess-1", run_id: "run-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-base",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"a.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-latest",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "end_of_turn",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "a.txt");
    expect(change?.rollback_checkpoint_id).toBe("cp-backend-baseline");
    expect(change?.checkpoint_id).toBe("cp-backend-baseline");
  });

  it("shows warning toast when run diff carries warning text", async () => {
    const checkpointDiff = vi.fn(async () => ({
      payload: {
        checkpoint_id: "cp-latest",
        files: { modified: ["a.txt"] },
        patch: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n",
        warning: "run baseline checkpoint is missing",
      },
    }));
    const api = createMockGatewayAPI({ checkpointDiff });

    handleGatewayEvent(
      {
        type: EventType.InputNormalized,
        payload: {
          payload: {
            runtime_event_type: EventType.InputNormalized,
            payload: { session_id: "sess-1", run_id: "run-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"a.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-latest",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "end_of_turn",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    expect(
      useUIStore
        .getState()
        .toasts.some((toast) =>
          toast.message.includes("run baseline checkpoint is missing"),
        ),
    ).toBe(true);
  });

  it("keeps first-touch checkpoint for a file after run-scoped diff replacement", async () => {
    const checkpointDiff = vi.fn(async () => ({
      payload: {
        checkpoint_id: "cp-latest",
        files: { modified: ["a.txt"] },
        patch: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n",
      },
    }));
    const api = createMockGatewayAPI({ checkpointDiff });

    handleGatewayEvent(
      {
        type: EventType.InputNormalized,
        payload: {
          payload: {
            runtime_event_type: EventType.InputNormalized,
            payload: { session_id: "sess-1", run_id: "run-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-base",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"a.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolDiff,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolDiff,
            payload: {
              tool_call_id: "tc1",
              tool_name: "filesystem_write_file",
              file_path: "a.txt",
              diff: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-latest",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "end_of_turn",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "a.txt");
    expect(change?.checkpoint_id).toBe("cp-base");
  });

  it("does not overwrite first-touch checkpoint when the same file is edited multiple times", () => {
    const api = createMockGatewayAPI();

    handleGatewayEvent(
      {
        type: EventType.InputNormalized,
        payload: {
          payload: {
            runtime_event_type: EventType.InputNormalized,
            payload: { session_id: "sess-1", run_id: "run-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-base",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"a.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolDiff,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolDiff,
            payload: {
              tool_call_id: "tc1",
              tool_name: "filesystem_write_file",
              file_path: "a.txt",
              diff: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+mid\n",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-next",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolDiff,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolDiff,
            payload: {
              tool_call_id: "tc2",
              tool_name: "filesystem_write_file",
              file_path: "a.txt",
              diff: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-mid\n+new\n",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "a.txt");
    expect(change?.checkpoint_id).toBe("cp-base");
  });

  it("stores hunk structure for transient tool diffs before aggregate checkpoint diff arrives", () => {
    const api = createMockGatewayAPI();

    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"a.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolDiff,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolDiff,
            payload: {
              tool_call_id: "tc1",
              tool_name: "filesystem_write_file",
              file_path: "a.txt",
              diff: "--- a/a.txt\n+++ b/a.txt\n@@ -1,3 +1,3 @@\n line 1\n-old\n+new\n line 3\n@@ -10,2 +10,3 @@\n line 10\n+line 11\n line 12\n",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "a.txt");
    expect(change?.hunks).toHaveLength(2);
    expect(change?.hunks?.[0]?.lines.map((line) => line.type)).toEqual([
      "header",
      "context",
      "del",
      "add",
      "context",
    ]);
    expect(change?.hunks?.[1]?.lines.map((line) => line.content)).toEqual([
      "@@ -10,2 +10,3 @@",
      "line 10",
      "line 11",
      "line 12",
    ]);
  });

  it("uses backend kind for multi-file tool diffs instead of was_new fallback", () => {
    const api = createMockGatewayAPI();

    handleGatewayEvent(
      {
        type: EventType.ToolDiff,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolDiff,
            payload: {
              tool_call_id: "tc-move",
              tool_name: "filesystem_move_file",
              file_path: "old.txt",
              files: [
                { path: "old.txt", kind: "deleted" },
                { path: "new.txt", kind: "added" },
              ],
              diffs: [
                {
                  path: "old.txt",
                  kind: "deleted",
                  was_new: false,
                  diff: "--- a/old.txt\n+++ b/old.txt\n-old\n",
                },
                {
                  path: "new.txt",
                  kind: "added",
                  was_new: false,
                  diff: "--- a/new.txt\n+++ b/new.txt\n+new\n",
                },
              ],
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const changes = useUIStore.getState().fileChanges;
    expect(changes.find((entry) => entry.path === "old.txt")?.status).toBe(
      "deleted",
    );
    expect(changes.find((entry) => entry.path === "new.txt")?.status).toBe(
      "added",
    );
  });

  it("tracks bash side-effect changes using backend kind", () => {
    const api = createMockGatewayAPI();

    handleGatewayEvent(
      {
        type: EventType.BashSideEffect,
        payload: {
          payload: {
            runtime_event_type: EventType.BashSideEffect,
            payload: {
              tool_call_id: "bash-1",
              command: "echo hi > generated.txt",
              changes: [{ path: "generated.txt", kind: "added" }],
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "generated.txt");
    expect(change?.status).toBe("added");
  });

  it("keeps transient tool diffs visible when backend sends simplified diff without @@ header", () => {
    const api = createMockGatewayAPI();

    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"a.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolDiff,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolDiff,
            payload: {
              tool_call_id: "tc1",
              tool_name: "filesystem_write_file",
              file_path: "a.txt",
              diff: "--- a/a.txt\n+++ b/a.txt\n-old\n+new\n",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "a.txt");
    expect(change).toMatchObject({ additions: 1, deletions: 1 });
    expect(change?.hunks).toHaveLength(1);
    expect(change?.hunks?.[0]?.lines.map((line) => line.content)).toEqual([
      "old",
      "new",
    ]);
  });

  it("preserves run-scoped file entries even when patch metadata is absent", async () => {
    const checkpointDiff = vi.fn(async () => ({
      payload: {
        checkpoint_id: "cp2",
        files: {
          added: ["c.txt"],
          modified: ["a.txt", "b.txt"],
          deleted: ["d.txt"],
        },
        patch: "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n",
      },
    }));
    const api = createMockGatewayAPI({ checkpointDiff });

    handleGatewayEvent(
      {
        type: EventType.InputNormalized,
        payload: {
          payload: {
            runtime_event_type: EventType.InputNormalized,
            payload: { session_id: "sess-1", run_id: "run-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"b.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    expect(
      useUIStore.getState().fileChanges.find((entry) => entry.path === "b.txt"),
    ).toBeDefined();

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp2",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "end_of_turn",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    const changes = useUIStore.getState().fileChanges;
    expect(changes).toHaveLength(4);
    expect(changes.map((entry) => entry.path)).toEqual([
      "a.txt",
      "b.txt",
      "c.txt",
      "d.txt",
    ]);
    expect(changes.find((entry) => entry.path === "a.txt")).toMatchObject({
      status: "modified",
      additions: 1,
      deletions: 1,
    });
    expect(changes.find((entry) => entry.path === "b.txt")).toMatchObject({
      status: "modified",
      additions: 0,
      deletions: 0,
      diff: undefined,
      hunks: undefined,
    });
    expect(changes.find((entry) => entry.path === "c.txt")).toMatchObject({
      status: "added",
      additions: 0,
      deletions: 0,
    });
    expect(changes.find((entry) => entry.path === "d.txt")).toMatchObject({
      status: "deleted",
      additions: 0,
      deletions: 0,
    });
  });

  it("rebinds post-restore first-touch baseline to restored checkpoint", async () => {
    const loadSession = vi.fn(async () => ({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [],
      },
    }));
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);

    handleGatewayEvent(
      {
        type: EventType.InputNormalized,
        payload: {
          payload: {
            runtime_event_type: EventType.InputNormalized,
            payload: { session_id: "sess-1", run_id: "run-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-old",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc1",
              arguments: '{"path":"a.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );

    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp-restored",
              session_id: "sess-1",
              guard_checkpoint_id: "guard-1",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    useUIStore.getState().clearFileChanges();
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc2",
              arguments: '{"path":"fresh.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-2",
      },
      api,
    );
    const fresh = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "fresh.txt");
    expect(fresh?.checkpoint_id).toBe("cp-restored");
  });

  it("keeps restored pending baseline even when InputNormalized is missing before next run", async () => {
    const loadSession = vi.fn(async () => ({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [],
      },
    }));
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp-restored",
              session_id: "sess-1",
              guard_checkpoint_id: "guard-1",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-restore",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    // 注意: 故意不发送 InputNormalized
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc-next",
              arguments: '{"path":"no-input-normalized.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-next",
      },
      api,
    );

    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "no-input-normalized.txt");
    expect(change?.checkpoint_id).toBe("cp-restored");
  });

  it("rebinds post-undo first-touch baseline to guard checkpoint", async () => {
    const loadSession = vi.fn(async () => ({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [],
      },
    }));
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointUndoRestore,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointUndoRestore,
            payload: { session_id: "sess-1", guard_checkpoint_id: "guard-1" },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    useUIStore.getState().clearFileChanges();
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc3",
              arguments: '{"path":"undo-fresh.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-2",
      },
      api,
    );
    const fresh = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "undo-fresh.txt");
    expect(fresh?.checkpoint_id).toBe("guard-1");
  });

  it("resets first-touch cache when run_id changes for the same file path", () => {
    const api = createMockGatewayAPI();

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-old",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc-old",
              arguments: '{"path":"same.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    let change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "same.txt");
    expect(change?.checkpoint_id).toBe("cp-old");

    useUIStore.getState().clearFileChanges();
    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-new",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "pre_write",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-2",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc-new",
              arguments: '{"path":"same.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-2",
      },
      api,
    );
    change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "same.txt");
    expect(change?.checkpoint_id).toBe("cp-new");
  });

  it("does not let pre_restore_guard overwrite pending restore baseline for the next run", async () => {
    const loadSession = vi.fn(async () => ({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [],
      },
    }));
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp-restored",
              session_id: "sess-1",
              guard_checkpoint_id: "guard-1",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-restore",
      },
      api,
    );
    await Promise.resolve();
    await Promise.resolve();

    handleGatewayEvent(
      {
        type: EventType.CheckpointCreated,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointCreated,
            payload: {
              checkpoint_id: "cp-guard",
              code_checkpoint_ref: "c",
              session_checkpoint_ref: "s",
              commit_hash: "",
              reason: "pre_restore_guard",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-restore",
      },
      api,
    );

    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc-next",
              arguments: '{"path":"pending-protected.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-next",
      },
      api,
    );
    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "pending-protected.txt");
    expect(change?.checkpoint_id).toBe("cp-restored");
  });

  it("applies only the latest restore reload when restore events arrive back-to-back", async () => {
    let resolveFirst: ((value: unknown) => void) | undefined;
    let resolveSecond: ((value: unknown) => void) | undefined;
    const loadSession = vi.fn(() => {
      if (!resolveFirst) {
        return new Promise((resolve) => {
          resolveFirst = resolve;
        });
      }
      return new Promise((resolve) => {
        resolveSecond = resolve;
      });
    });
    const api = createMockGatewayAPI({ loadSession });
    useSessionStore.setState({ currentSessionId: "sess-1" } as any);

    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp-old",
              session_id: "sess-1",
              guard_checkpoint_id: "guard-1",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-1",
      },
      api,
    );
    handleGatewayEvent(
      {
        type: EventType.CheckpointRestored,
        payload: {
          payload: {
            runtime_event_type: EventType.CheckpointRestored,
            payload: {
              checkpoint_id: "cp-new",
              session_id: "sess-1",
              guard_checkpoint_id: "guard-2",
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-2",
      },
      api,
    );

    resolveSecond?.({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [{ role: "assistant", content: "newest" }],
      },
    });
    await Promise.resolve();
    await Promise.resolve();

    resolveFirst?.({
      payload: {
        id: "sess-1",
        agent_mode: "build",
        messages: [{ role: "assistant", content: "stale" }],
      },
    });
    await Promise.resolve();
    await Promise.resolve();

    await waitFor(() => {
      expect(useUIStore.getState().isRestoringCheckpoint).toBe(false);
    });

    handleGatewayEvent(
      {
        type: EventType.ToolStart,
        payload: {
          payload: {
            runtime_event_type: EventType.ToolStart,
            payload: {
              name: "filesystem_write_file",
              id: "tc-after-restore",
              arguments: '{"path":"after-double-restore.txt"}',
            },
          },
        },
        session_id: "sess-1",
        run_id: "run-3",
      },
      api,
    );
    const change = useUIStore
      .getState()
      .fileChanges.find((entry) => entry.path === "after-double-restore.txt");
    expect(change?.checkpoint_id).toBe("cp-new");
    expect(useUIStore.getState().checkpointRollbackUndo).toBeNull();

    const textMessages = useChatStore
      .getState()
      .messages.filter((m) => m.type === "text");
    expect(textMessages.map((m) => m.content)).toContain("newest");
    expect(textMessages.map((m) => m.content)).not.toContain("stale");
  });
});
