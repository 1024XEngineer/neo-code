import {
  EventType,
  type AcceptanceDecidedPayload,
  type BashSideEffectPayload,
  type BudgetCheckedPayload,
  type BudgetEstimateFailedPayload,
  type CheckpointCreatedPayload,
  type CheckpointDiffResultPayload,
  type CheckpointRestoredPayload,
  type CheckpointUndoRestorePayload,
  type CheckpointWarningPayload,
  type LedgerReconciledPayload,
  type MessageFrame,
  type PermissionRequestPayload,
  type PendingUserQuestionSnapshot,
  type TodoEventPayload,
  type TokenUsage,
  type VerificationCompletedPayload,
  type VerificationFailedPayload,
  type VerificationFinishedPayload,
  type VerificationStageFinishedPayload,
  type VerificationStartedPayload,
  type ToolDiffPayload,
} from "@/api/protocol";
import { type GatewayAPI } from "@/api/gateway";
import { useChatStore } from "@/stores/useChatStore";
import { useUIStore } from "@/stores/useUIStore";
import { useGatewayStore } from "@/stores/useGatewayStore";
import {
  beginCheckpointRestoreReloadSeq,
  reloadSessionAfterCheckpointRestore,
  useSessionStore,
} from "@/stores/useSessionStore";
import { useRuntimeInsightStore } from "@/stores/useRuntimeInsightStore";
import { useWorkspaceStore } from "@/stores/useWorkspaceStore";
import {
  parseSingleFileDiff,
  parseUnifiedPatch,
  type ParsedFileDiff,
} from "@/utils/patchParser";

type PayloadRecord = Record<string, unknown> | undefined;

// 模块级缓存:最新 verification 消息 ID 与最近完成的 tool_call ID
// 用于避免每次 verification stage / checkpoint 事件都全量扫描 messages 数组
let _latestVerificationMsgId: string | undefined;
let _latestDoneToolCallId: string | undefined;

// 模块级缓存最新的 checkpoint_id，用于工具占位条目关联后续端到端 diff。
let _latestCheckpointId: string | undefined;
let _latestRunDiffRequestId = 0;
let _latestRestoreSyncRequestId = 0;
// 文件首次触碰时的回退基线 checkpoint：key=标准化路径，value=checkpoint_id。
let _firstTouchRollbackCheckpointByPath = new Map<string, string>();
// restore/undo 后“下一轮”回退基线锚点，仅由 restore/undo 事件写入。
let _pendingNextRunRollbackCheckpointId: string | undefined;
// 当前用于回退基线绑定的 run 边界（按 frame.run_id 检测）。
let _currentRollbackRunId: string | undefined;
// 标记 pending 基线已应用到哪个 run；切到下一 run 时自动失效。
let _pendingRollbackAppliedRunId: string | undefined;
const CHECKPOINT_REASON_PRE_RESTORE_GUARD = "pre_restore_guard";

/** 重置模块级游标 —— 在截断聊天历史 / 切换会话等场景调用，避免后续事件挂到已被移除的消息上 */
export function resetEventBridgeCursors() {
  const keepCheckpointBaseline = useUIStore.getState().isRestoringCheckpoint;
  _latestVerificationMsgId = undefined;
  _latestDoneToolCallId = undefined;
  _latestCheckpointId = keepCheckpointBaseline
    ? _latestCheckpointId
    : undefined;
  _firstTouchRollbackCheckpointByPath = new Map<string, string>();
  _currentRollbackRunId = undefined;
  _pendingRollbackAppliedRunId = keepCheckpointBaseline
    ? _pendingRollbackAppliedRunId
    : undefined;
  _pendingNextRunRollbackCheckpointId = keepCheckpointBaseline
    ? _pendingNextRunRollbackCheckpointId
    : undefined;
  _latestRunDiffRequestId += 1;
  if (!keepCheckpointBaseline) {
    _latestRestoreSyncRequestId += 1;
    useUIStore.getState().setRestoringCheckpoint(false);
  }
}

// trackRollbackRunBoundary 按 run_id 切分文件回退基线缓存，避免跨 run 复用旧 first-touch 映射。
function trackRollbackRunBoundary(runId: string) {
  const normalizedRunId = runId.trim();
  if (!normalizedRunId) return;
  if (_currentRollbackRunId === normalizedRunId) return;

  _currentRollbackRunId = normalizedRunId;
  _firstTouchRollbackCheckpointByPath = new Map<string, string>();

  // pending 基线只作用于“下一轮”；一旦已在某个 run 消费，切到后续 run 即失效。
  if (
    _pendingRollbackAppliedRunId &&
    _pendingRollbackAppliedRunId !== normalizedRunId
  ) {
    _pendingNextRunRollbackCheckpointId = undefined;
    _pendingRollbackAppliedRunId = undefined;
  }
}

/**
 * 把后端 toSlash 后的绝对路径与模型 raw 相对路径统一到工作区相对、正斜杠形式，
 * 让 ToolStart 占位条目与 ToolDiff 真实条目能命中同一个 dedup key。
 * Windows 下大小写不敏感比较；找不到工作区根时退化为只做斜杠/前导 ./ 归一化。
 */
function normalizeFilePath(input: string): string {
  if (!input) return input;
  let p = input.replace(/\\/g, "/").trim();
  const ws = useWorkspaceStore.getState();
  const root = ws.workspaces.find(
    (w) => w.hash === ws.currentWorkspaceHash,
  )?.path;
  if (root) {
    const r = root.replace(/\\/g, "/").replace(/\/+$/, "");
    if (r && p.toLowerCase().startsWith(r.toLowerCase() + "/")) {
      p = p.slice(r.length + 1);
    } else if (r && p.toLowerCase() === r.toLowerCase()) {
      p = "";
    }
  }
  while (p.startsWith("./")) p = p.slice(2);
  return p;
}

// resolveRollbackCheckpointID 计算文件项的回退 checkpoint，优先首次触碰基线，避免被后续 checkpoint 覆盖。
function resolveRollbackCheckpointID(
  path: string,
  fallback?: string,
  allowLatestFallback: boolean = true,
): string | undefined {
  const normalizedPath = normalizeFilePath(path);
  if (!normalizedPath) return fallback;

  const firstTouch = _firstTouchRollbackCheckpointByPath.get(normalizedPath);
  if (firstTouch) return firstTouch;

  const pending = _pendingNextRunRollbackCheckpointId;
  if (pending) {
    _firstTouchRollbackCheckpointByPath.set(normalizedPath, pending);
    if (_currentRollbackRunId && !_pendingRollbackAppliedRunId) {
      _pendingRollbackAppliedRunId = _currentRollbackRunId;
    }
    return pending;
  }

  const candidate =
    fallback || (allowLatestFallback ? _latestCheckpointId : undefined);
  if (candidate) {
    _firstTouchRollbackCheckpointByPath.set(normalizedPath, candidate);
  }
  return candidate;
}

function _upsertFileChange(
  rawPath: string,
  status: "pending" | "added" | "modified" | "deleted",
  parsed?: ParsedFileDiff,
) {
  const path = normalizeFilePath(rawPath);
  if (!path) return;
  useUIStore.getState().clearCheckpointRollbackUndo();
  const checkpointID = resolveRollbackCheckpointID(path);
  const existing = useUIStore
    .getState()
    .fileChanges.find((c) => c.path === path);
  if (existing) {
    useUIStore.setState((s) => ({
      fileChanges: s.fileChanges.map((c) =>
        c.path === path
          ? {
              ...c,
              status,
              additions: parsed?.additions ?? c.additions,
              deletions: parsed?.deletions ?? c.deletions,
              diff: parsed?.lines ?? c.diff,
              hunks: parsed?.hunks ?? c.hunks,
              checkpoint_id: c.checkpoint_id ?? checkpointID,
            }
          : c,
      ),
    }));
  } else {
    useUIStore.getState().addFileChange({
      id: `fc_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
      path,
      status,
      additions: parsed?.additions ?? 0,
      deletions: parsed?.deletions ?? 0,
      diff: parsed?.lines,
      hunks: parsed?.hunks,
      checkpoint_id: checkpointID,
    });
  }
}

function normalizeChangeStatus(
  kind: unknown,
): "added" | "modified" | "deleted" | undefined {
  if (kind === "added" || kind === "modified" || kind === "deleted")
    return kind;
  return undefined;
}

/** 写文件工具名集合 */
const FILE_WRITE_TOOLS = new Set([
  "filesystem_write_file",
  "filesystem_edit",
  "filesystem_delete_file",
  "filesystem_move_file",
  "filesystem_copy_file",
]);

/** 从 ToolStart 事件提取文件路径并立即填充面板（+0/-0 占位，等 tool_diff 覆盖真实数据） */
function _trackFileChangeFromTool(toolName: string, argsRaw: string) {
  if (!FILE_WRITE_TOOLS.has(toolName)) return;

  let args: Record<string, unknown>;
  try {
    args = JSON.parse(argsRaw);
  } catch {
    return;
  }

  // 统一用 pending 占位，真实状态由 tool_diff/run diff 事件覆盖
  if (
    toolName === "filesystem_move_file" ||
    toolName === "filesystem_copy_file"
  ) {
    const src = typeof args.source_path === "string" ? args.source_path : "";
    const dst =
      typeof args.destination_path === "string" ? args.destination_path : "";
    if (src) _upsertFileChange(src, "pending");
    if (dst) _upsertFileChange(dst, "pending");
  } else {
    const path = typeof args.path === "string" ? args.path : "";
    if (path) _upsertFileChange(path, "pending");
  }

  if (!useUIStore.getState().changesPanelOpen) {
    useUIStore.getState().toggleChangesPanel();
  }
}

/** 处理 tool_diff 事件：用后端提供的精确 diff 数据更新 FileChange 条目 */
function _applyToolDiff(payload: ToolDiffPayload) {
  // 多文件工具（move/copy）
  if (payload.diffs && payload.diffs.length > 0) {
    const kindByPath = new Map<string, "added" | "modified" | "deleted">();
    for (const file of payload.files ?? []) {
      const normalized = normalizeFilePath(file.path);
      const status = normalizeChangeStatus(file.kind);
      if (normalized && status) kindByPath.set(normalized, status);
    }
    for (const entry of payload.diffs) {
      const normalized = normalizeFilePath(entry.path);
      const status =
        normalizeChangeStatus(entry.kind) ??
        kindByPath.get(normalized) ??
        (entry.was_new ? "added" : "modified");
      const parsed = entry.diff ? parseSingleFileDiff(entry.diff) : undefined;
      _upsertFileChange(entry.path, status, parsed);
    }
  } else {
    // 单文件工具（write/edit/delete）
    const path = payload.file_path;
    if (!path) return;
    const status: "added" | "modified" | "deleted" = payload.was_new
      ? "added"
      : payload.tool_name === "filesystem_delete_file"
        ? "deleted"
        : "modified";
    const parsed = payload.diff ? parseSingleFileDiff(payload.diff) : undefined;
    _upsertFileChange(path, status, parsed);
  }

  if (!useUIStore.getState().changesPanelOpen) {
    useUIStore.getState().toggleChangesPanel();
  }
}

function _applyBashSideEffect(payload: BashSideEffectPayload) {
  let changed = false;
  for (const change of payload.changes ?? []) {
    const status = normalizeChangeStatus(change.kind);
    if (!status) continue;
    _upsertFileChange(change.path, status);
    changed = true;
  }
  if (changed && !useUIStore.getState().changesPanelOpen) {
    useUIStore.getState().toggleChangesPanel();
  }
}

function _fileChangesFromCheckpointDiff(
  diff: CheckpointDiffResultPayload,
  existingCheckpointByPath: Map<string, string | undefined>,
) {
  const parsed = diff.patch ? parseUnifiedPatch(diff.patch) : {};
  const parsedByPath = new Map<string, ParsedFileDiff>();
  for (const [path, parsedDiff] of Object.entries(parsed)) {
    const normalized = normalizeFilePath(path);
    if (normalized) parsedByPath.set(normalized, parsedDiff);
  }

  if (diff.file_entries && diff.file_entries.length > 0) {
    return diff.file_entries
      .map((entry) => {
        const path = normalizeFilePath(entry.path);
        const status = normalizeChangeStatus(entry.kind) ?? "modified";
        const parsedDiff = parsedByPath.get(path);
        const rollbackCheckpointID =
          entry.can_rollback === false
            ? undefined
            : entry.rollback_checkpoint_id;
        return {
          id: `fc_${path}`,
          path,
          status,
          additions: parsedDiff?.additions ?? 0,
          deletions: parsedDiff?.deletions ?? 0,
          diff: parsedDiff?.lines,
          hunks: parsedDiff?.hunks,
          checkpoint_id: rollbackCheckpointID,
          rollback_checkpoint_id: rollbackCheckpointID,
        };
      })
      .filter((change) => change.path)
      .sort((a, b) => a.path.localeCompare(b.path));
  }

  const byPath = new Map<string, "added" | "modified" | "deleted">();
  for (const path of diff.files?.added ?? [])
    byPath.set(normalizeFilePath(path), "added");
  for (const path of diff.files?.modified ?? [])
    byPath.set(normalizeFilePath(path), "modified");
  for (const path of diff.files?.deleted ?? [])
    byPath.set(normalizeFilePath(path), "deleted");

  for (const path of parsedByPath.keys()) {
    if (!byPath.has(path)) byPath.set(path, "modified");
  }

  return Array.from(byPath.entries())
    .filter(([path]) => path)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([path, status]) => {
      const parsedDiff = parsedByPath.get(path);
      const existingCheckpointID = existingCheckpointByPath.get(path);
      return {
        id: `fc_${path}`,
        path,
        status,
        additions: parsedDiff?.additions ?? 0,
        deletions: parsedDiff?.deletions ?? 0,
        diff: parsedDiff?.lines,
        hunks: parsedDiff?.hunks,
        checkpoint_id: existingCheckpointID,
        rollback_checkpoint_id: existingCheckpointID,
      };
    });
}

function _refreshRunFileChanges(
  gatewayAPI: GatewayAPI,
  sessionId: string,
  runId: string,
  checkpointId: string,
) {
  const requestId = ++_latestRunDiffRequestId;
  gatewayAPI
    .checkpointDiff({
      session_id: sessionId,
      run_id: runId,
      checkpoint_id: checkpointId,
      scope: "run",
    })
    .then((result) => {
      if (requestId !== _latestRunDiffRequestId) return;
      if (runId !== useGatewayStore.getState().currentRunId) return;
      if (sessionId !== useSessionStore.getState().currentSessionId) return;
      if (!result?.payload) return;
      if (result.payload.warning) {
        useUIStore
          .getState()
          .showToast(`Checkpoint warning: ${result.payload.warning}`, "info");
      }
      const existingCheckpointByPath = new Map<string, string | undefined>(
        useUIStore
          .getState()
          .fileChanges.map((change) => [
            change.path,
            change.rollback_checkpoint_id ?? change.checkpoint_id,
          ]),
      );
      const nextFileChanges = _fileChangesFromCheckpointDiff(
        result.payload,
        existingCheckpointByPath,
      );
      if (nextFileChanges.length > 0) {
        useUIStore.getState().clearCheckpointRollbackUndo();
      }
      useUIStore.getState().replaceFileChanges(nextFileChanges);
    })
    .catch((error) => {
      console.warn("[eventBridge] checkpoint.diff run scope failed:", error);
    });
}

// applyBaselineCheckpointRestoreEvent 只同步文件级 baseline 回退，不刷新会话消息或 insight。
function applyBaselineCheckpointRestoreEvent(
  payload: CheckpointRestoredPayload,
): boolean {
  const restoredPaths = new Set(
    (payload.paths ?? []).map(normalizeFilePath).filter(Boolean),
  );
  _latestRunDiffRequestId += 1;
  useUIStore.getState().setRestoringCheckpoint(false);
  useUIStore.getState().clearCheckpointRollbackUndo();
  if (restoredPaths.size === 0) {
    return false;
  }
  for (const path of restoredPaths) {
    _firstTouchRollbackCheckpointByPath.delete(path);
  }
  let removedAllChanges = false;
  useUIStore.setState((state) => {
    const remaining = state.fileChanges.filter(
      (change) => !restoredPaths.has(normalizeFilePath(change.path)),
    );
    removedAllChanges = state.fileChanges.length > 0 && remaining.length === 0;
    return { fileChanges: remaining };
  });
  return removedAllChanges;
}

// refreshSessionAfterCheckpointRestoreEvent 仅在当前会话收到 restore/undo 事件时刷新会话与文件变更视图。
function refreshSessionAfterCheckpointRestoreEvent(
  gatewayAPI: GatewayAPI,
  payloadSessionId: string,
  nextCheckpointId: string | undefined,
) {
  const sessionId = payloadSessionId.trim();
  const currentSessionId = useSessionStore.getState().currentSessionId.trim();
  if (!sessionId || !currentSessionId || sessionId !== currentSessionId) {
    return;
  }

  const normalizedNextCheckpointId = nextCheckpointId?.trim();
  if (normalizedNextCheckpointId) {
    _latestCheckpointId = normalizedNextCheckpointId;
    _pendingNextRunRollbackCheckpointId = normalizedNextCheckpointId;
    _pendingRollbackAppliedRunId = undefined;
  }

  const requestId = ++_latestRestoreSyncRequestId;
  _latestRunDiffRequestId += 1;
  const reloadSeq = beginCheckpointRestoreReloadSeq();
  _firstTouchRollbackCheckpointByPath = new Map<string, string>();
  useUIStore.getState().setRestoringCheckpoint(true);
  useUIStore.getState().clearFileChanges();
  void reloadSessionAfterCheckpointRestore(gatewayAPI, sessionId, reloadSeq)
    .then(() => {
      if (requestId !== _latestRestoreSyncRequestId) return;
      if (normalizedNextCheckpointId) {
        _latestCheckpointId = normalizedNextCheckpointId;
        _pendingNextRunRollbackCheckpointId = normalizedNextCheckpointId;
      }
      useUIStore.getState().setRestoringCheckpoint(false);
    })
    .catch((error) => {
      if (requestId !== _latestRestoreSyncRequestId) return;
      useUIStore.getState().setRestoringCheckpoint(false);
      console.warn(
        "[eventBridge] failed to reload session after checkpoint restore:",
        error,
      );
      useUIStore
        .getState()
        .showToast("Failed to refresh session after restore", "error");
    });
}

function normalizePermissionPayload(
  raw: unknown,
): PermissionRequestPayload | null {
  const r = raw as Record<string, unknown> | undefined;
  if (!r || typeof r !== "object") return null;
  const s = (k1: string, k2: string): string =>
    strField(r, k1) || strField(r, k2);
  return {
    request_id: s("request_id", "RequestID"),
    tool_call_id: s("tool_call_id", "ToolCallID"),
    tool_name: s("tool_name", "ToolName"),
    tool_category: s("tool_category", "ToolCategory"),
    action_type: s("action_type", "ActionType"),
    operation: s("operation", "Operation"),
    target_type: s("target_type", "TargetType"),
    target: s("target", "Target"),
    decision: s("decision", "Decision"),
    reason: s("reason", "Reason"),
  };
}

function normalizeUserQuestionRequestedPayload(
  raw: unknown,
): PendingUserQuestionSnapshot | null {
  const r = raw as Record<string, unknown> | undefined;
  if (!r || typeof r !== "object") return null;
  const requestId = strField(r, "request_id") || strField(r, "RequestID");
  if (!requestId) return null;
  const options = Array.isArray(r.options) ? [...r.options] : undefined;
  const parseNumberField = (
    camel: string,
    pascal: string,
  ): number | undefined => {
    const value = r[camel] ?? r[pascal];
    if (typeof value === "number" && Number.isFinite(value) && value > 0)
      return value;
    if (typeof value === "string") {
      const parsed = Number(value);
      if (Number.isFinite(parsed) && parsed > 0) return parsed;
    }
    return undefined;
  };
  return {
    request_id: requestId,
    question_id: strField(r, "question_id") || strField(r, "QuestionID"),
    title: strField(r, "title") || strField(r, "Title"),
    description: strField(r, "description") || strField(r, "Description"),
    kind: strField(r, "kind") || strField(r, "Kind"),
    options,
    required: !!(r.required ?? r.Required),
    allow_skip: !!(r.allow_skip ?? r.AllowSkip),
    max_choices: parseNumberField("max_choices", "MaxChoices"),
    timeout_sec: parseNumberField("timeout_sec", "TimeoutSec"),
  };
}

const CRITICAL_EVENTS = new Set<string>([EventType.Error]);

function strField(payload: unknown, key: string): string {
  return ((payload as PayloadRecord)?.[key] as string) ?? "";
}

function resolveCompactMode(payload: unknown): string {
  if (typeof payload === "string") return payload.trim() || "manual";
  return (
    strField(payload, "trigger_mode") ||
    strField(payload, "TriggerMode") ||
    "manual"
  );
}

function compactMessageForMode(mode: string): string {
  switch (mode) {
    case "proactive":
      return "Context is near the limit. Auto-compacting...";
    case "reactive":
      return "Model reported context too long. Compacting and retrying...";
    case "manual":
    default:
      return "Compacting context...";
  }
}

function compactErrorMessageForMode(mode: string, message: string): string {
  if (mode === "proactive" || mode === "reactive") {
    return message ? `Auto context compaction failed: ${message}` : "Auto context compaction failed";
  }
  return message || "Compaction failed";
}

/** 从 tool_result 事件中解析最终工具调用 ID，兼容字段名差异。 */
function resolveToolResultCallID(eventPayload: unknown): string {
  return (
    strField(eventPayload, "tool_call_id") ||
    strField(eventPayload, "toolCallId") ||
    strField(eventPayload, "id") ||
    strField(eventPayload, "call_id")
  );
}

/** 将 tool_result 结算到匹配条目；若 ID 缺失或乱序，回退结算最近一条 running 工具消息。 */
function settleToolResultMessage(eventPayload: unknown) {
  const resultText = strField(eventPayload, "content");
  const isError = !!(eventPayload as PayloadRecord)?.is_error;
  const status = isError ? ("error" as const) : ("done" as const);
  const callID = resolveToolResultCallID(eventPayload);

  if (callID) {
    const matched = useChatStore
      .getState()
      .updateToolCall(callID, resultText, status);
    if (matched) return callID;
  }

  let settledID = "";
  useChatStore.setState((state) => {
    let consumed = false;
    const next = [...state.messages];
    for (let i = next.length - 1; i >= 0; i -= 1) {
      const item = next[i];
      if (item.type !== "tool_call" || item.toolStatus !== "running") continue;
      if (consumed) break;
      settledID = item.toolCallId || "";
      next[i] = {
        ...item,
        toolStatus: status,
        toolResult: resultText || item.toolResult,
      };
      consumed = true;
    }
    return consumed ? { messages: next } : state;
  });
  return settledID;
}

/**
 * 将 Gateway 事件帧桥接到 Zustand store action。
 * 从 Go internal/tui/services/gateway_stream_client.go 的 decodeRuntimeEventFromGatewayNotification 对齐。
 */
export function handleGatewayEvent(
  frame: MessageFrame,
  gatewayAPI: GatewayAPI,
) {
  const payload = frame.payload as PayloadRecord;
  if (!payload) return;

  const innerEnvelope = payload.payload as PayloadRecord;
  const eventType =
    (innerEnvelope?.runtime_event_type as string | undefined) ??
    (payload.event_type as string | undefined);
  if (!eventType) return;

  // Discard non-critical events during workspace transition to avoid stale data
  // Only Error events are allowed through during transition
  if (
    useChatStore.getState().isTransitioning &&
    !CRITICAL_EVENTS.has(eventType)
  ) {
    return;
  }

  const eventPayload = innerEnvelope?.payload;

  const chatStore = useChatStore.getState();
  const uiStore = useUIStore.getState();
  const gwStore = useGatewayStore.getState();
  const insightStore = useRuntimeInsightStore.getState();

  const frameSessionId = frame.session_id;
  const frameRunId = frame.run_id;

  /** 更新最新 verification 消息的 data 为 insightStore 当前最后一条 record */
  function syncLatestVerificationToChat() {
    const history = useRuntimeInsightStore.getState().verificationHistory;
    if (_latestVerificationMsgId && history.length > 0) {
      chatStore.updateVerificationMessage(
        _latestVerificationMsgId,
        history[history.length - 1],
      );
    }
  }

  switch (eventType) {
    case EventType.ThinkingDelta: {
      const text = eventPayload as string | undefined;
      if (!text) break;
      if (!chatStore.streamingThinkingMessageId) {
        useChatStore.getState().startThinkingMessage();
      }
      useChatStore.getState().appendThinkingChunk(text);
      break;
    }

    case EventType.AgentChunk: {
      // 终结 thinking 消息
      if (chatStore.streamingThinkingMessageId) {
        chatStore.finalizeThinkingMessage();
      }
      const text = eventPayload as string | undefined;
      if (!text) break;
      if (!chatStore.streamingMessageId) {
        chatStore.startStreamingMessage();
      }
      useChatStore.getState().appendChunk(text);
      break;
    }

    case EventType.AgentDone: {
      if (chatStore.streamingThinkingMessageId) {
        chatStore.finalizeThinkingMessage();
      }
      if (chatStore.streamingMessageId) {
        const parts = (
          eventPayload as { parts?: { text?: string }[] } | undefined
        )?.parts;
        const content =
          parts && Array.isArray(parts)
            ? parts.map((p) => p?.text ?? "").join("")
            : strField(eventPayload, "content");
        chatStore.finalizeMessage(chatStore.streamingMessageId, content);
      }
      chatStore.setGenerating(false);
      chatStore.finalizeRunningToolCalls("done");
      if (frameSessionId) {
        useSessionStore
          .getState()
          .fetchSessions(gatewayAPI, true)
          .catch(() => {});
      }
      break;
    }

    case EventType.ToolStart: {
      trackRollbackRunBoundary(
        frameRunId || useGatewayStore.getState().currentRunId,
      );
      const toolName = strField(eventPayload, "name");
      const toolArgs = strField(eventPayload, "arguments");
      const msg = {
        id: `tool_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
        role: "tool" as const,
        type: "tool_call" as const,
        content: "",
        toolName,
        toolCallId: strField(eventPayload, "id"),
        toolArgs,
        toolStatus: "running" as const,
        timestamp: Date.now(),
      };
      useChatStore.getState().addMessage(msg);

      // 从写文件工具的参数中提取文件路径，立即填充 FileChangePanel
      _trackFileChangeFromTool(toolName, toolArgs);
      break;
    }

    case EventType.ToolResult: {
      const tcId = settleToolResultMessage(eventPayload);
      _latestDoneToolCallId = tcId;
      break;
    }

    case EventType.ToolDiff: {
      trackRollbackRunBoundary(
        frameRunId || useGatewayStore.getState().currentRunId,
      );
      const diffPayload = eventPayload as ToolDiffPayload | undefined;
      if (diffPayload) _applyToolDiff(diffPayload);
      break;
    }

    case EventType.BashSideEffect: {
      trackRollbackRunBoundary(
        frameRunId || useGatewayStore.getState().currentRunId,
      );
      const payload = eventPayload as BashSideEffectPayload | undefined;
      if (payload) _applyBashSideEffect(payload);
      break;
    }

    case EventType.ToolChunk: {
      const toolCallId = strField(eventPayload, "tool_call_id");
      if (toolCallId)
        useChatStore
          .getState()
          .appendToolOutput(toolCallId, strField(eventPayload, "content"));
      break;
    }

    case EventType.UserMessage:
      break;

    case EventType.InputNormalized: {
      const sessionId =
        strField(eventPayload, "session_id") || frameSessionId || "";
      const runId = strField(eventPayload, "run_id") || frameRunId || "";
      if (runId) gwStore.setCurrentRunId(runId);
      trackRollbackRunBoundary(runId);
      _firstTouchRollbackCheckpointByPath = new Map<string, string>();
      useUIStore.getState().clearFileChanges();
      if (
        sessionId &&
        sessionId !== useSessionStore.getState().currentSessionId
      ) {
        useSessionStore.getState().setCurrentSessionId(sessionId);
      }
      useSessionStore
        .getState()
        .fetchSessions(gatewayAPI, true)
        .catch(() => {});
      break;
    }

    case EventType.PermissionRequested: {
      const permPayload = normalizePermissionPayload(eventPayload);
      if (permPayload)
        useChatStore.getState().addPermissionRequest(permPayload);
      break;
    }

    case EventType.PermissionResolved: {
      const r = eventPayload as Record<string, unknown> | undefined;
      const requestId = strField(r, "request_id") || strField(r, "RequestID");
      if (requestId) useChatStore.getState().removePermissionRequest(requestId);
      break;
    }

    case EventType.UserQuestionRequested: {
      const payload = normalizeUserQuestionRequestedPayload(eventPayload);
      if (payload) useChatStore.getState().setPendingUserQuestion(payload);
      break;
    }

    case EventType.UserQuestionAnswered:
    case EventType.UserQuestionSkipped:
    case EventType.UserQuestionTimeout: {
      const p = eventPayload as Record<string, unknown> | undefined;
      const requestId = strField(p, "request_id") || strField(p, "RequestID");
      useChatStore.getState().clearPendingUserQuestion(requestId || undefined);
      break;
    }

    case EventType.TokenUsage: {
      const usage = eventPayload as TokenUsage | undefined;
      if (usage) useChatStore.getState().updateTokenUsage(usage);
      break;
    }

    case EventType.BudgetChecked: {
      const payload = eventPayload as BudgetCheckedPayload | undefined;
      if (payload) insightStore.setBudgetChecked(payload);
      break;
    }

    case EventType.BudgetEstimateFailed: {
      const payload = eventPayload as BudgetEstimateFailedPayload | undefined;
      if (payload) insightStore.setBudgetEstimateFailed(payload);
      break;
    }

    case EventType.LedgerReconciled: {
      const payload = eventPayload as LedgerReconciledPayload | undefined;
      if (payload) insightStore.setLedgerReconciled(payload);
      break;
    }

    case EventType.Error: {
      const errorMsg = (eventPayload as string) ?? "Unknown error";
      uiStore.showToast(errorMsg, "error");
      useChatStore.getState().resetGeneratingState();
      useChatStore.getState().finalizeRunningToolCalls("error");
      break;
    }

    case EventType.StopReasonDecided: {
      const reason = strField(eventPayload, "reason");
      const detail = strField(eventPayload, "detail");
      useChatStore.getState().setStopReason(reason);
      useChatStore.getState().setGenerating(false);
      if (reason === "fatal_error") {
        useChatStore.getState().finalizeRunningToolCalls("error");
      }
      if (reason === "fatal_error") {
        uiStore.showToast(detail || "模型调用失败，请检查配置", "error");
      }
      break;
    }

    case EventType.PhaseChanged: {
      useChatStore.getState().setPhase(strField(eventPayload, "to"));
      break;
    }

    case EventType.RunCanceled: {
      useChatStore.getState().resetGeneratingState();
      uiStore.showToast("Run cancelled", "info");
      break;
    }

    case EventType.ToolCallThinking:
      break;

    case EventType.CompactStart: {
      const mode = resolveCompactMode(eventPayload);
      useChatStore
        .getState()
        .startCompacting(mode, compactMessageForMode(mode));
      break;
    }

    case EventType.CompactApplied: {
      const mode = resolveCompactMode(eventPayload);
      useChatStore.getState().finishCompacting();
      if (mode === "manual") {
        uiStore.showToast("Context compacted", "success");
      }
      break;
    }

    case EventType.CompactError: {
      const mode = resolveCompactMode(eventPayload);
      const message =
        strField(eventPayload, "message") ||
        strField(eventPayload, "Message") ||
        (typeof eventPayload === "string" ? eventPayload : "");
      useChatStore.getState().finishCompacting();
      uiStore.showToast(compactErrorMessageForMode(mode, message), "error");
      break;
    }

    case EventType.SkillActivated: {
      uiStore.showToast(
        `Skill activated: ${strField(eventPayload, "skill_id")}`,
        "success",
      );
      break;
    }

    case EventType.SkillDeactivated: {
      uiStore.showToast(
        `Skill deactivated: ${strField(eventPayload, "skill_id")}`,
        "info",
      );
      break;
    }

    case EventType.SkillMissing: {
      uiStore.showToast(
        `Skill unavailable: ${strField(eventPayload, "skill_id")}`,
        "error",
      );
      break;
    }

    case EventType.AssetSaved: {
      const assetPath = strField(eventPayload, "path");
      if (assetPath) {
        uiStore.showToast(`File saved: ${assetPath}`, "success");
      }
      break;
    }

    case EventType.AssetSaveFailed: {
      uiStore.showToast(
        `Failed to save file: ${strField(eventPayload, "path")}`,
        "error",
      );
      break;
    }

    case EventType.TodoUpdated:
    case EventType.TodoSummaryInjected: {
      const payload = eventPayload as TodoEventPayload | undefined;
      if (payload) {
        insightStore.addTodoEvent(payload);
        if (payload.items) {
          insightStore.setTodoSnapshot({
            items: payload.items,
            summary: payload.summary,
          });
        }
      }
      break;
    }

    case EventType.TodoSnapshotUpdated: {
      const payload = eventPayload as TodoEventPayload | undefined;
      if (payload) {
        insightStore.addTodoEvent(payload);
        if (payload.items) {
          insightStore.applyTodoSnapshot({
            items: payload.items,
            summary: payload.summary,
          });
        }
      }
      break;
    }

    case EventType.ProgressEvaluated:
      break;

    case EventType.TodoConflict: {
      const payload = eventPayload as TodoEventPayload | undefined;
      if (payload) insightStore.setTodoConflict(payload);
      const reason = strField(eventPayload, "reason");
      // revision_conflict 与 invalid_transition 是可恢复冲突，仅在面板显示，不弹全局 toast;
      // 其余冲突降级为 info 避免打断聊天体验。
      if (
        reason &&
        reason !== "revision_conflict" &&
        reason !== "invalid_transition"
      ) {
        uiStore.showToast(`Todo conflict: ${reason}`, "info");
      }
      break;
    }

    case EventType.VerificationStarted: {
      const payload = eventPayload as VerificationStartedPayload | undefined;
      if (!payload) break;
      // completion gate 已拦截（典型场景: pending_todo），verifier 不会真正运行；
      // 跳过创建 verification chat message，避免出现 "0/1 passed" 误导。
      // 该状态由下游 AcceptanceDecided → AcceptanceMessage 完整呈现。
      if (payload.completion_passed === false) {
        break;
      }
      const recordId = insightStore.startVerification(payload);
      const history = useRuntimeInsightStore.getState().verificationHistory;
      const record =
        history.length > 0 ? history[history.length - 1] : undefined;
      if (record) {
        const msgId = `msg_${Date.now()}_verify_${recordId.slice(0, 8)}`;
        _latestVerificationMsgId = msgId;
        chatStore.addMessage({
          id: msgId,
          role: "assistant",
          type: "verification",
          content: "",
          verificationData: record,
          timestamp: Date.now(),
        });
      }
      chatStore.setPhase("verification");
      uiStore.showToast("Verification started", "info");
      break;
    }

    case EventType.VerificationStageFinished: {
      const payload = eventPayload as
        | VerificationStageFinishedPayload
        | undefined;
      if (payload) {
        insightStore.upsertVerificationStage(payload);
        syncLatestVerificationToChat();
      }
      break;
    }

    case EventType.VerificationFinished: {
      const payload = eventPayload as VerificationFinishedPayload | undefined;
      if (payload) {
        insightStore.finishVerification(payload);
        syncLatestVerificationToChat();
      }
      break;
    }

    case EventType.VerificationCompleted: {
      const payload = eventPayload as VerificationCompletedPayload | undefined;
      if (payload) {
        insightStore.completeVerification(payload);
        syncLatestVerificationToChat();
      }
      break;
    }

    case EventType.VerificationFailed: {
      const payload = eventPayload as VerificationFailedPayload | undefined;
      if (payload) {
        insightStore.failVerification(payload);
        syncLatestVerificationToChat();
      }
      uiStore.showToast(
        strField(eventPayload, "error_class") || "Verification failed",
        "error",
      );
      break;
    }

    case EventType.AcceptanceDecided: {
      const payload = eventPayload as AcceptanceDecidedPayload | undefined;
      if (payload) {
        insightStore.setAcceptanceDecision(payload);
        // 在聊天流中创建 acceptance 决策内联卡
        useChatStore.getState().addMessage({
          id: `msg_${Date.now()}_accept`,
          role: "assistant",
          type: "acceptance",
          content: payload.user_visible_summary || "",
          acceptanceData: payload,
          timestamp: Date.now(),
        });
      }
      break;
    }

    case EventType.CheckpointCreated: {
      const payload = eventPayload as CheckpointCreatedPayload | undefined;
      if (payload) {
        insightStore.addCheckpointEvent(payload);
        if (_latestDoneToolCallId) {
          chatStore.attachCheckpointToToolCall(
            _latestDoneToolCallId,
            payload.checkpoint_id,
          );
        }
        if (payload.reason !== CHECKPOINT_REASON_PRE_RESTORE_GUARD) {
          _latestCheckpointId = payload.checkpoint_id;
        }
        if (
          payload.reason === "end_of_turn" &&
          gatewayAPI &&
          frameSessionId &&
          frameRunId
        ) {
          _refreshRunFileChanges(
            gatewayAPI,
            frameSessionId,
            frameRunId,
            payload.checkpoint_id,
          );
        }
      }
      break;
    }

    case EventType.CheckpointWarning: {
      const payload = eventPayload as CheckpointWarningPayload | undefined;
      if (payload) insightStore.setCheckpointWarning(payload);
      uiStore.showToast(
        `Checkpoint warning: ${payload?.error ?? "unknown error"}`,
        "info",
      );
      break;
    }

    case EventType.CheckpointRestored: {
      const payload = eventPayload as CheckpointRestoredPayload | undefined;
      if (payload) {
        insightStore.addCheckpointEvent(payload);
        if (
          payload.session_id === useSessionStore.getState().currentSessionId
        ) {
          if (payload.mode === "baseline") {
            const removedAllChanges =
              applyBaselineCheckpointRestoreEvent(payload);
            if (removedAllChanges && payload.guard_checkpoint_id?.trim()) {
              useUIStore.getState().setCheckpointRollbackUndo({
                sessionId: payload.session_id,
                checkpointId: payload.checkpoint_id,
                guardCheckpointId: payload.guard_checkpoint_id,
                paths: payload.paths ?? [],
              });
            }
            uiStore.showToast("File rollback completed", "success");
            break;
          }
          useUIStore.getState().clearCheckpointRollbackUndo();
          chatStore.markAllCheckpointsRestored();
          refreshSessionAfterCheckpointRestoreEvent(
            gatewayAPI,
            payload.session_id,
            payload.checkpoint_id,
          );
          uiStore.showToast("Checkpoint restored", "success");
        }
      }
      break;
    }

    case EventType.CheckpointUndoRestore: {
      const payload = eventPayload as CheckpointUndoRestorePayload | undefined;
      if (payload) {
        insightStore.addCheckpointEvent(payload);
        if (
          payload.session_id === useSessionStore.getState().currentSessionId
        ) {
          const rollbackUndo = useUIStore.getState().checkpointRollbackUndo;
          useUIStore.getState().clearCheckpointRollbackUndo();
          useUIStore.getState().clearFileChanges();
          useUIStore.getState().setRestoringCheckpoint(false);
          if (
            rollbackUndo?.sessionId === payload.session_id &&
            rollbackUndo.guardCheckpointId === payload.guard_checkpoint_id
          ) {
            uiStore.showToast("Rollback undo completed", "success");
            break;
          }
          chatStore.markAllCheckpointsAvailable();
          refreshSessionAfterCheckpointRestoreEvent(
            gatewayAPI,
            payload.session_id,
            payload.guard_checkpoint_id,
          );
          uiStore.showToast("Checkpoint restore undone", "success");
        }
      }
      break;
    }

    default:
      break;
  }
}
