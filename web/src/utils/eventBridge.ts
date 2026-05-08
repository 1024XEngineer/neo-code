import {
  EventType,
  type AcceptanceDecidedPayload,
  type BudgetCheckedPayload,
  type BudgetEstimateFailedPayload,
  type CheckpointCreatedPayload,
  type CheckpointRestoredPayload,
  type CheckpointUndoRestorePayload,
  type CheckpointWarningPayload,
  type LedgerReconciledPayload,
  type MessageFrame,
  type PermissionRequestPayload,
  type TodoEventPayload,
  type TokenUsage,
  type VerificationCompletedPayload,
  type VerificationFailedPayload,
  type VerificationFinishedPayload,
  type VerificationStageFinishedPayload,
  type VerificationStartedPayload,
  type ToolDiffPayload,
} from '@/api/protocol'
import { type GatewayAPI } from '@/api/gateway'
import { useChatStore } from '@/stores/useChatStore'
import { useUIStore } from '@/stores/useUIStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { useWorkspaceStore } from '@/stores/useWorkspaceStore'
import { parseSingleFileDiff, type ParsedFileDiff } from '@/utils/patchParser'

type PayloadRecord = Record<string, unknown> | undefined

// 模块级缓存:最新 verification 消息 ID 与最近完成的 tool_call ID
// 用于避免每次 verification stage / checkpoint 事件都全量扫描 messages 数组
let _latestVerificationMsgId: string | undefined
let _latestDoneToolCallId: string | undefined

// 模块级缓存最新的 checkpoint_id，供 reject 回退使用
let _latestCheckpointId: string | undefined

/** 重置模块级游标 —— 在截断聊天历史 / 切换会话等场景调用，避免后续事件挂到已被移除的消息上 */
export function resetEventBridgeCursors() {
  _latestVerificationMsgId = undefined
  _latestDoneToolCallId = undefined
  _latestCheckpointId = undefined
}

/**
 * 把后端 toSlash 后的绝对路径与模型 raw 相对路径统一到工作区相对、正斜杠形式，
 * 让 ToolStart 占位条目与 ToolDiff 真实条目能命中同一个 dedup key。
 * Windows 下大小写不敏感比较；找不到工作区根时退化为只做斜杠/前导 ./ 归一化。
 */
function normalizeFilePath(input: string): string {
  if (!input) return input
  let p = input.replace(/\\/g, '/').trim()
  const ws = useWorkspaceStore.getState()
  const root = ws.workspaces.find((w) => w.hash === ws.currentWorkspaceHash)?.path
  if (root) {
    const r = root.replace(/\\/g, '/').replace(/\/+$/, '')
    if (r && p.toLowerCase().startsWith(r.toLowerCase() + '/')) {
      p = p.slice(r.length + 1)
    } else if (r && p.toLowerCase() === r.toLowerCase()) {
      p = ''
    }
  }
  while (p.startsWith('./')) p = p.slice(2)
  return p
}

function _upsertFileChange(
  rawPath: string,
  status: 'added' | 'modified' | 'deleted',
  parsed?: ParsedFileDiff,
) {
  const path = normalizeFilePath(rawPath)
  if (!path) return
  const existing = useUIStore.getState().fileChanges.find((c) => c.path === path)
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
              checkpoint_id: _latestCheckpointId ?? c.checkpoint_id,
            }
          : c,
      ),
    }))
  } else {
    useUIStore.getState().addFileChange({
      id: `fc_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
      path,
      status,
      additions: parsed?.additions ?? 0,
      deletions: parsed?.deletions ?? 0,
      diff: parsed?.lines,
      checkpoint_id: _latestCheckpointId,
    })
  }
}

/** 写文件工具名集合 */
const FILE_WRITE_TOOLS = new Set([
  'filesystem_write_file',
  'filesystem_edit',
  'filesystem_delete_file',
  'filesystem_move_file',
  'filesystem_copy_file',
])

/** 从 ToolStart 事件提取文件路径并立即填充面板（+0/-0 占位，等 tool_diff 覆盖真实数据） */
function _trackFileChangeFromTool(toolName: string, argsRaw: string) {
  if (!FILE_WRITE_TOOLS.has(toolName)) return

  let args: Record<string, unknown>
  try {
    args = JSON.parse(argsRaw)
  } catch {
    return
  }

  // 统一用 modified 占位，真实状态由 tool_diff 事件覆盖
  if (toolName === 'filesystem_move_file' || toolName === 'filesystem_copy_file') {
    const src = typeof args.source_path === 'string' ? args.source_path : ''
    const dst = typeof args.destination_path === 'string' ? args.destination_path : ''
    if (src) _upsertFileChange(src, 'modified')
    if (dst) _upsertFileChange(dst, 'modified')
  } else {
    const path = typeof args.path === 'string' ? args.path : ''
    if (path) _upsertFileChange(path, 'modified')
  }

  if (!useUIStore.getState().changesPanelOpen) {
    useUIStore.getState().toggleChangesPanel()
  }
}

/** 处理 tool_diff 事件：用后端提供的精确 diff 数据更新 FileChange 条目 */
function _applyToolDiff(payload: ToolDiffPayload) {
  // 多文件工具（move/copy）
  if (payload.diffs && payload.diffs.length > 0) {
    for (const entry of payload.diffs) {
      const status: 'added' | 'modified' | 'deleted' = entry.was_new ? 'added' : 'modified'
      const parsed = entry.diff ? parseSingleFileDiff(entry.diff) : undefined
      _upsertFileChange(entry.path, status, parsed)
    }
  } else {
    // 单文件工具（write/edit/delete）
    const path = payload.file_path
    if (!path) return
    const status: 'added' | 'modified' | 'deleted' =
      payload.was_new ? 'added' :
      payload.tool_name === 'filesystem_delete_file' ? 'deleted' : 'modified'
    const parsed = payload.diff ? parseSingleFileDiff(payload.diff) : undefined
    _upsertFileChange(path, status, parsed)
  }

  if (!useUIStore.getState().changesPanelOpen) {
    useUIStore.getState().toggleChangesPanel()
  }
}

function normalizePermissionPayload(raw: unknown): PermissionRequestPayload | null {
  const r = raw as Record<string, unknown> | undefined
  if (!r || typeof r !== 'object') return null
  const s = (k1: string, k2: string): string => strField(r, k1) || strField(r, k2)
  return {
    request_id: s('request_id', 'RequestID'),
    tool_call_id: s('tool_call_id', 'ToolCallID'),
    tool_name: s('tool_name', 'ToolName'),
    tool_category: s('tool_category', 'ToolCategory'),
    action_type: s('action_type', 'ActionType'),
    operation: s('operation', 'Operation'),
    target_type: s('target_type', 'TargetType'),
    target: s('target', 'Target'),
    decision: s('decision', 'Decision'),
    reason: s('reason', 'Reason'),
  }
}

const CRITICAL_EVENTS = new Set<string>([
  EventType.Error,
])

function strField(payload: unknown, key: string): string {
  return ((payload as PayloadRecord)?.[key] as string) ?? ''
}

/**
 * 将 Gateway 事件帧桥接到 Zustand store action。
 * 从 Go internal/tui/services/gateway_stream_client.go 的 decodeRuntimeEventFromGatewayNotification 对齐。
 */
export function handleGatewayEvent(frame: MessageFrame, gatewayAPI: GatewayAPI) {
  const payload = frame.payload as PayloadRecord
  if (!payload) return

  const innerEnvelope = payload.payload as PayloadRecord
  const eventType = (innerEnvelope?.runtime_event_type as string | undefined)
    ?? (payload.event_type as string | undefined)
  if (!eventType) return

  // Discard non-critical events during workspace transition to avoid stale data
  // Only Error events are allowed through during transition
  if (useChatStore.getState().isTransitioning && !CRITICAL_EVENTS.has(eventType)) {
    return
  }

  const eventPayload = innerEnvelope?.payload

  const chatStore = useChatStore.getState()
  const uiStore = useUIStore.getState()
  const gwStore = useGatewayStore.getState()
  const insightStore = useRuntimeInsightStore.getState()

  const frameSessionId = frame.session_id
  const frameRunId = frame.run_id

  /** 更新最新 verification 消息的 data 为 insightStore 当前最后一条 record */
  function syncLatestVerificationToChat() {
    const history = useRuntimeInsightStore.getState().verificationHistory
    if (_latestVerificationMsgId && history.length > 0) {
      chatStore.updateVerificationMessage(_latestVerificationMsgId, history[history.length - 1])
    }
  }

  switch (eventType) {
    case EventType.ThinkingDelta: {
      const text = eventPayload as string | undefined
      if (!text) break
      if (!chatStore.streamingThinkingMessageId) {
        useChatStore.getState().startThinkingMessage()
      }
      useChatStore.getState().appendThinkingChunk(text)
      break
    }

    case EventType.AgentChunk: {
      // 终结 thinking 消息
      if (chatStore.streamingThinkingMessageId) {
        chatStore.finalizeThinkingMessage()
      }
      const text = eventPayload as string | undefined
      if (!text) break
      if (!chatStore.streamingMessageId) {
        chatStore.startStreamingMessage()
      }
      useChatStore.getState().appendChunk(text)
      break
    }

    case EventType.AgentDone: {
      if (chatStore.streamingThinkingMessageId) {
        chatStore.finalizeThinkingMessage()
      }
      if (chatStore.streamingMessageId) {
        const parts = (eventPayload as { parts?: { text?: string }[] } | undefined)?.parts
        const content = parts && Array.isArray(parts)
          ? parts.map((p) => p?.text ?? '').join('')
          : strField(eventPayload, 'content')
        chatStore.finalizeMessage(chatStore.streamingMessageId, content)
      }
      chatStore.setGenerating(false)
      if (frameSessionId) {
        useSessionStore.getState().fetchSessions(gatewayAPI, true).catch(() => {})
      }
      break
    }

    case EventType.ToolStart: {
      const toolName = strField(eventPayload, 'name')
      const toolArgs = strField(eventPayload, 'arguments')
      const msg = {
        id: `tool_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
        role: 'tool' as const,
        type: 'tool_call' as const,
        content: '',
        toolName,
        toolCallId: strField(eventPayload, 'id'),
        toolArgs,
        toolStatus: 'running' as const,
        timestamp: Date.now(),
      }
      useChatStore.getState().addMessage(msg)

      // 从写文件工具的参数中提取文件路径，立即填充 FileChangePanel
      _trackFileChangeFromTool(toolName, toolArgs)
      break
    }

    case EventType.ToolResult: {
      const tcId = strField(eventPayload, 'tool_call_id')
      useChatStore.getState().updateToolCall(tcId, strField(eventPayload, 'content'), 'done')
      _latestDoneToolCallId = tcId
      break
    }

    case EventType.ToolDiff: {
      const diffPayload = eventPayload as ToolDiffPayload | undefined
      if (diffPayload) _applyToolDiff(diffPayload)
      break
    }

    case EventType.ToolChunk: {
      const toolCallId = strField(eventPayload, 'tool_call_id')
      if (toolCallId) useChatStore.getState().appendToolOutput(toolCallId, strField(eventPayload, 'content'))
      break
    }

    case EventType.UserMessage:
      break

    case EventType.InputNormalized: {
      const sessionId = strField(eventPayload, 'session_id') || frameSessionId || ''
      const runId = strField(eventPayload, 'run_id') || frameRunId || ''
      if (runId) gwStore.setCurrentRunId(runId)
      if (sessionId && sessionId !== useSessionStore.getState().currentSessionId) {
        useSessionStore.getState().setCurrentSessionId(sessionId)
      }
      useSessionStore.getState().fetchSessions(gatewayAPI, true).catch(() => {})
      break
    }

    case EventType.PermissionRequested: {
      const permPayload = normalizePermissionPayload(eventPayload)
      if (permPayload) useChatStore.getState().addPermissionRequest(permPayload)
      break
    }

    case EventType.PermissionResolved: {
      const r = eventPayload as Record<string, unknown> | undefined
      const requestId = strField(r, 'request_id') || strField(r, 'RequestID')
      if (requestId) useChatStore.getState().removePermissionRequest(requestId)
      break
    }

    case EventType.TokenUsage: {
      const usage = eventPayload as TokenUsage | undefined
      if (usage) useChatStore.getState().updateTokenUsage(usage)
      break
    }

    case EventType.BudgetChecked: {
      const payload = eventPayload as BudgetCheckedPayload | undefined
      if (payload) insightStore.setBudgetChecked(payload)
      break
    }

    case EventType.BudgetEstimateFailed: {
      const payload = eventPayload as BudgetEstimateFailedPayload | undefined
      if (payload) insightStore.setBudgetEstimateFailed(payload)
      break
    }

    case EventType.LedgerReconciled: {
      const payload = eventPayload as LedgerReconciledPayload | undefined
      if (payload) insightStore.setLedgerReconciled(payload)
      break
    }

    case EventType.Error: {
      const errorMsg = (eventPayload as string) ?? 'Unknown error'
      uiStore.showToast(errorMsg, 'error')
      useChatStore.getState().resetGeneratingState()
      break
    }

    case EventType.StopReasonDecided: {
      const reason = strField(eventPayload, 'reason')
      const detail = strField(eventPayload, 'detail')
      useChatStore.getState().setStopReason(reason)
      useChatStore.getState().setGenerating(false)
      if (reason === 'fatal_error') {
        uiStore.showToast(detail || '模型调用失败，请检查配置', 'error')
      }
      break
    }

    case EventType.PhaseChanged: {
      useChatStore.getState().setPhase(strField(eventPayload, 'to'))
      break
    }

    case EventType.RunCanceled: {
      useChatStore.getState().resetGeneratingState()
      uiStore.showToast('Run cancelled', 'info')
      break
    }

    case EventType.ToolCallThinking:
      break

    case EventType.CompactStart: {
      uiStore.showToast('Compacting context...', 'info')
      break
    }

    case EventType.CompactApplied: {
      uiStore.showToast('Context compacted', 'success')
      break
    }

    case EventType.CompactError: {
      uiStore.showToast((eventPayload as string) ?? 'Compaction failed', 'error')
      break
    }

    case EventType.SkillActivated: {
      uiStore.showToast(`Skill activated: ${strField(eventPayload, 'skill_id')}`, 'success')
      break
    }

    case EventType.SkillDeactivated: {
      uiStore.showToast(`Skill deactivated: ${strField(eventPayload, 'skill_id')}`, 'info')
      break
    }

    case EventType.SkillMissing: {
      uiStore.showToast(`Skill unavailable: ${strField(eventPayload, 'skill_id')}`, 'error')
      break
    }

    case EventType.AssetSaved: {
      const assetPath = strField(eventPayload, 'path')
      if (assetPath) {
        uiStore.showToast(`File saved: ${assetPath}`, 'success')
      }
      break
    }

    case EventType.AssetSaveFailed: {
      uiStore.showToast(`Failed to save file: ${strField(eventPayload, 'path')}`, 'error')
      break
    }

    case EventType.TodoUpdated:
    case EventType.TodoSummaryInjected: {
      const payload = eventPayload as TodoEventPayload | undefined
      if (payload) {
        insightStore.addTodoEvent(payload)
        if (payload.items) {
          insightStore.setTodoSnapshot({ items: payload.items, summary: payload.summary })
        }
      }
      break
    }

    case EventType.TodoSnapshotUpdated: {
      const payload = eventPayload as TodoEventPayload | undefined
      if (payload) {
        insightStore.addTodoEvent(payload)
        if (payload.items) {
          insightStore.applyTodoSnapshot({ items: payload.items, summary: payload.summary })
        }
      }
      break
    }

    case EventType.ProgressEvaluated:
      break

    case EventType.TodoConflict: {
      const payload = eventPayload as TodoEventPayload | undefined
      if (payload) insightStore.setTodoConflict(payload)
      const reason = strField(eventPayload, 'reason')
      // revision_conflict 是可恢复冲突，仅在面板显示，不弹全局 toast;
      // 其余冲突降级为 info 避免打断聊天体验。
      if (reason && reason !== 'revision_conflict') {
        uiStore.showToast(`Todo conflict: ${reason}`, 'info')
      }
      break
    }

    case EventType.VerificationStarted: {
      const payload = eventPayload as VerificationStartedPayload | undefined
      if (!payload) break
      // completion gate 已拦截（典型场景: pending_todo），verifier 不会真正运行；
      // 跳过创建 verification chat message，避免出现 "0/1 passed" 误导。
      // 该状态由下游 AcceptanceDecided → AcceptanceMessage 完整呈现。
      if (payload.completion_passed === false) {
        break
      }
      const recordId = insightStore.startVerification(payload)
      const history = useRuntimeInsightStore.getState().verificationHistory
      const record = history.length > 0 ? history[history.length - 1] : undefined
      if (record) {
        const msgId = `msg_${Date.now()}_verify_${recordId.slice(0, 8)}`
        _latestVerificationMsgId = msgId
        chatStore.addMessage({
          id: msgId,
          role: 'assistant',
          type: 'verification',
          content: '',
          verificationData: record,
          timestamp: Date.now(),
        })
      }
      chatStore.setPhase('verification')
      uiStore.showToast('Verification started', 'info')
      break
    }

    case EventType.VerificationStageFinished: {
      const payload = eventPayload as VerificationStageFinishedPayload | undefined
      if (payload) {
        insightStore.upsertVerificationStage(payload)
        syncLatestVerificationToChat()
      }
      break
    }

    case EventType.VerificationFinished: {
      const payload = eventPayload as VerificationFinishedPayload | undefined
      if (payload) {
        insightStore.finishVerification(payload)
        syncLatestVerificationToChat()
      }
      break
    }

    case EventType.VerificationCompleted: {
      const payload = eventPayload as VerificationCompletedPayload | undefined
      if (payload) {
        insightStore.completeVerification(payload)
        syncLatestVerificationToChat()
      }
      break
    }

    case EventType.VerificationFailed: {
      const payload = eventPayload as VerificationFailedPayload | undefined
      if (payload) {
        insightStore.failVerification(payload)
        syncLatestVerificationToChat()
      }
      uiStore.showToast(strField(eventPayload, 'error_class') || 'Verification failed', 'error')
      break
    }

    case EventType.AcceptanceDecided: {
      const payload = eventPayload as AcceptanceDecidedPayload | undefined
      if (payload) {
        insightStore.setAcceptanceDecision(payload)
        // 在聊天流中创建 acceptance 决策内联卡
        useChatStore.getState().addMessage({
          id: `msg_${Date.now()}_accept`,
          role: 'assistant',
          type: 'acceptance',
          content: payload.user_visible_summary || '',
          acceptanceData: payload,
          timestamp: Date.now(),
        })
      }
      break
    }

    case EventType.CheckpointCreated: {
      const payload = eventPayload as CheckpointCreatedPayload | undefined
      if (payload) {
        insightStore.addCheckpointEvent(payload)
        if (_latestDoneToolCallId) {
          chatStore.attachCheckpointToToolCall(_latestDoneToolCallId, payload.checkpoint_id)
        }
        _latestCheckpointId = payload.checkpoint_id
      }
      break
    }

    case EventType.CheckpointWarning: {
      const payload = eventPayload as CheckpointWarningPayload | undefined
      if (payload) insightStore.setCheckpointWarning(payload)
      uiStore.showToast(`Checkpoint warning: ${payload?.error ?? 'unknown error'}`, 'info')
      break
    }

    case EventType.CheckpointRestored: {
      const payload = eventPayload as CheckpointRestoredPayload | undefined
      if (payload) insightStore.addCheckpointEvent(payload)
      chatStore.markAllCheckpointsRestored()
      uiStore.showToast('Checkpoint restored', 'success')
      break
    }

    case EventType.CheckpointUndoRestore: {
      const payload = eventPayload as CheckpointUndoRestorePayload | undefined
      if (payload) insightStore.addCheckpointEvent(payload)
      chatStore.markAllCheckpointsAvailable()
      uiStore.showToast('Checkpoint restore undone', 'success')
      break
    }

    default:
      break
  }
}
