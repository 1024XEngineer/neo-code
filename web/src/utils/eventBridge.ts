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
} from '@/api/protocol'
import { type GatewayAPI } from '@/api/gateway'
import { useChatStore } from '@/stores/useChatStore'
import { useUIStore } from '@/stores/useUIStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'

type PayloadRecord = Record<string, unknown> | undefined

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

  switch (eventType) {
    case EventType.AgentChunk: {
      const text = eventPayload as string | undefined
      if (!text) break
      if (!chatStore.streamingMessageId) {
        chatStore.startStreamingMessage()
      }
      useChatStore.getState().appendChunk(text)
      break
    }

    case EventType.AgentDone: {
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
      const msg = {
        id: `tool_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`,
        role: 'tool' as const,
        type: 'tool_call' as const,
        content: '',
        toolName: strField(eventPayload, 'name'),
        toolCallId: strField(eventPayload, 'id'),
        toolArgs: strField(eventPayload, 'arguments'),
        toolStatus: 'running' as const,
        timestamp: Date.now(),
      }
      useChatStore.getState().addMessage(msg)
      break
    }

    case EventType.ToolResult: {
      useChatStore.getState().updateToolCall(strField(eventPayload, 'tool_call_id'), strField(eventPayload, 'content'), 'done')
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
      const errorMsg = (eventPayload as string) ?? '未知错误'
      uiStore.showToast(errorMsg, 'error')
      useChatStore.getState().resetGeneratingState()
      break
    }

    case EventType.StopReasonDecided: {
      useChatStore.getState().setStopReason(strField(eventPayload, 'reason'))
      useChatStore.getState().setGenerating(false)
      break
    }

    case EventType.PhaseChanged: {
      useChatStore.getState().setPhase(strField(eventPayload, 'to'))
      break
    }

    case EventType.RunCanceled: {
      useChatStore.getState().resetGeneratingState()
      uiStore.showToast('运行已取消', 'info')
      break
    }

    case EventType.ToolCallThinking:
      break

    case EventType.CompactStart: {
      uiStore.showToast('正在压缩上下文...', 'info')
      break
    }

    case EventType.CompactApplied: {
      uiStore.showToast('上下文压缩完成', 'success')
      break
    }

    case EventType.CompactError: {
      uiStore.showToast((eventPayload as string) ?? '压缩失败', 'error')
      break
    }

    case EventType.SkillActivated: {
      uiStore.showToast(`技能已激活: ${strField(eventPayload, 'skill_id')}`, 'success')
      break
    }

    case EventType.SkillDeactivated: {
      uiStore.showToast(`技能已停用: ${strField(eventPayload, 'skill_id')}`, 'info')
      break
    }

    case EventType.SkillMissing: {
      uiStore.showToast(`技能不可用: ${strField(eventPayload, 'skill_id')}`, 'error')
      break
    }

    case EventType.AssetSaved: {
      const assetPath = strField(eventPayload, 'path')
      if (assetPath) {
        uiStore.showToast(`文件已保存: ${assetPath}`, 'success')
        const currentChanges = useUIStore.getState().fileChanges
        if (!currentChanges.find((c) => c.path === assetPath)) {
          uiStore.addFileChange({
            id: `fc_${Date.now()}`,
            path: assetPath,
            status: 'modified' as const,
            additions: 0,
            deletions: 0,
          })
        }
      }
      break
    }

    case EventType.AssetSaveFailed: {
      uiStore.showToast(`文件保存失败: ${strField(eventPayload, 'path')}`, 'error')
      break
    }

    case EventType.TodoUpdated:
    case EventType.TodoSummaryInjected: {
      const payload = eventPayload as TodoEventPayload | undefined
      if (payload) {
        insightStore.addTodoEvent(payload)
        if (payload.items || payload.summary) {
          insightStore.setTodoSnapshot({ items: payload.items, summary: payload.summary })
        }
      }
      break
    }

    case EventType.TodoSnapshotUpdated: {
      const payload = eventPayload as TodoEventPayload | undefined
      if (payload) {
        insightStore.addTodoEvent(payload)
        insightStore.setTodoSnapshot({ items: payload.items, summary: payload.summary })
      }
      break
    }

    case EventType.ProgressEvaluated:
      break

    case EventType.TodoConflict: {
      const payload = eventPayload as TodoEventPayload | undefined
      if (payload) insightStore.setTodoConflict(payload)
      uiStore.showToast(`Todo 冲突: ${strField(eventPayload, 'reason')}`, 'error')
      break
    }

    case EventType.VerificationStarted: {
      const payload = eventPayload as VerificationStartedPayload | undefined
      if (payload) insightStore.startVerification(payload)
      useChatStore.getState().setPhase('verification')
      uiStore.showToast('验证开始', 'info')
      break
    }

    case EventType.VerificationStageFinished: {
      const payload = eventPayload as VerificationStageFinishedPayload | undefined
      if (payload) insightStore.upsertVerificationStage(payload)
      break
    }

    case EventType.VerificationFinished: {
      const payload = eventPayload as VerificationFinishedPayload | undefined
      if (payload) insightStore.finishVerification(payload)
      break
    }

    case EventType.VerificationCompleted: {
      const payload = eventPayload as VerificationCompletedPayload | undefined
      if (payload) insightStore.completeVerification(payload)
      break
    }

    case EventType.VerificationFailed: {
      const payload = eventPayload as VerificationFailedPayload | undefined
      if (payload) insightStore.failVerification(payload)
      uiStore.showToast(strField(eventPayload, 'error_class') || '验证失败', 'error')
      break
    }

    case EventType.AcceptanceDecided: {
      const payload = eventPayload as AcceptanceDecidedPayload | undefined
      if (payload) insightStore.setAcceptanceDecision(payload)
      break
    }

    case EventType.CheckpointCreated: {
      const payload = eventPayload as CheckpointCreatedPayload | undefined
      if (payload) insightStore.addCheckpointEvent(payload)
      break
    }

    case EventType.CheckpointWarning: {
      const payload = eventPayload as CheckpointWarningPayload | undefined
      if (payload) insightStore.setCheckpointWarning(payload)
      uiStore.showToast(`Checkpoint 告警: ${strField(eventPayload, 'phase')}`, 'info')
      break
    }

    case EventType.CheckpointRestored: {
      const payload = eventPayload as CheckpointRestoredPayload | undefined
      if (payload) insightStore.addCheckpointEvent(payload)
      uiStore.showToast('Checkpoint 已恢复', 'success')
      break
    }

    case EventType.CheckpointUndoRestore: {
      const payload = eventPayload as CheckpointUndoRestorePayload | undefined
      if (payload) insightStore.addCheckpointEvent(payload)
      uiStore.showToast('Checkpoint 恢复已撤销', 'success')
      break
    }

    default:
      break
  }
}
