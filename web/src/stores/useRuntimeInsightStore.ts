import { create } from 'zustand'
import {
  type AcceptanceDecidedPayload,
  type BudgetCheckedPayload,
  type BudgetEstimateFailedPayload,
  type CheckpointCreatedPayload,
  type CheckpointDiffResultPayload,
  type CheckpointEntry,
  type CheckpointRestoredPayload,
  type CheckpointUndoRestorePayload,
  type CheckpointWarningPayload,
  type LedgerReconciledPayload,
  type TodoEventPayload,
  type TodoSnapshot,
  type TodoViewItem,
  type VerificationCompletedPayload,
  type VerificationFailedPayload,
  type VerificationFinishedPayload,
  type VerificationStageFinishedPayload,
  type VerificationStartedPayload,
} from '@/api/protocol'

/** 单次 verification 跑批的归档记录,用于 InsightPanel 的历史 tab 与聊天流内联折叠卡共享 */
export interface VerificationRunRecord {
  id: string
  startedAt: number
  finishedAt?: number
  started: VerificationStartedPayload
  stages: Record<string, VerificationStageFinishedPayload>
  finished?: VerificationFinishedPayload
  completed?: VerificationCompletedPayload
  failed?: VerificationFailedPayload
  status: 'running' | 'finished' | 'completed' | 'failed'
}

/** 会话内 todo 累积历史:每次 snapshot 合并写入,旧条目即使被新 snapshot 移除也保留 */
export interface TodoHistoryEntry extends TodoViewItem {
  lastSeenAt: number
  firstSeenAt: number
}

interface RuntimeInsightState {
  checkpoints: CheckpointEntry[]
  checkpointDiff: CheckpointDiffResultPayload | null
  checkpointEvents: Array<CheckpointCreatedPayload | CheckpointRestoredPayload | CheckpointUndoRestorePayload>
  checkpointWarning: CheckpointWarningPayload | null
  verificationRunning: boolean
  verificationStarted: VerificationStartedPayload | null
  verificationStages: Record<string, VerificationStageFinishedPayload>
  verificationFinished: VerificationFinishedPayload | null
  verificationCompleted: VerificationCompletedPayload | null
  verificationFailed: VerificationFailedPayload | null
  /** 历史归档:每次 VerificationStarted 追加一条 record,后续 stage/finished/completed/failed 写入末尾 */
  verificationHistory: VerificationRunRecord[]
  acceptanceDecision: AcceptanceDecidedPayload | null
  todoSnapshot: TodoSnapshot | null
  todoEvents: TodoEventPayload[]
  todoConflict: TodoEventPayload | null
  todoHistory: Record<string, TodoHistoryEntry>
  budgetChecked: BudgetCheckedPayload | null
  budgetEstimateFailed: BudgetEstimateFailedPayload | null
  ledgerReconciled: LedgerReconciledPayload | null
  budgetUsageRatio: number | null

  setCheckpoints: (checkpoints: CheckpointEntry[]) => void
  setCheckpointDiff: (diff: CheckpointDiffResultPayload | null) => void
  addCheckpointEvent: (event: CheckpointCreatedPayload | CheckpointRestoredPayload | CheckpointUndoRestorePayload) => void
  setCheckpointWarning: (warning: CheckpointWarningPayload | null) => void
  startVerification: (payload: VerificationStartedPayload) => string
  upsertVerificationStage: (payload: VerificationStageFinishedPayload) => void
  finishVerification: (payload: VerificationFinishedPayload) => void
  completeVerification: (payload: VerificationCompletedPayload) => void
  failVerification: (payload: VerificationFailedPayload) => void
  setAcceptanceDecision: (payload: AcceptanceDecidedPayload | null) => void
  /** 成功事件更新快照并清除冲突（用于 todo_updated / todo_summary_injected） */
  setTodoSnapshot: (snapshot: TodoSnapshot | null) => void
  /** 仅更新快照，保留冲突状态（用于 todo_snapshot_updated） */
  applyTodoSnapshot: (snapshot: TodoSnapshot | null) => void
  addTodoEvent: (event: TodoEventPayload) => void
  setTodoConflict: (event: TodoEventPayload | null) => void
  setBudgetChecked: (payload: BudgetCheckedPayload) => void
  setBudgetEstimateFailed: (payload: BudgetEstimateFailedPayload | null) => void
  setLedgerReconciled: (payload: LedgerReconciledPayload | null) => void
  reset: () => void
}

const initialState = {
  checkpoints: [] as CheckpointEntry[],
  checkpointDiff: null as CheckpointDiffResultPayload | null,
  checkpointEvents: [] as Array<CheckpointCreatedPayload | CheckpointRestoredPayload | CheckpointUndoRestorePayload>,
  checkpointWarning: null as CheckpointWarningPayload | null,
  verificationRunning: false,
  verificationStarted: null as VerificationStartedPayload | null,
  verificationStages: {} as Record<string, VerificationStageFinishedPayload>,
  verificationFinished: null as VerificationFinishedPayload | null,
  verificationCompleted: null as VerificationCompletedPayload | null,
  verificationFailed: null as VerificationFailedPayload | null,
  verificationHistory: [] as VerificationRunRecord[],
  acceptanceDecision: null as AcceptanceDecidedPayload | null,
  todoSnapshot: null as TodoSnapshot | null,
  todoEvents: [] as TodoEventPayload[],
  todoConflict: null as TodoEventPayload | null,
  todoHistory: {} as Record<string, TodoHistoryEntry>,
  budgetChecked: null as BudgetCheckedPayload | null,
  budgetEstimateFailed: null as BudgetEstimateFailedPayload | null,
  ledgerReconciled: null as LedgerReconciledPayload | null,
  budgetUsageRatio: null as number | null,
}

function calculateBudgetUsageRatio(payload: BudgetCheckedPayload): number | null {
  if (!payload.prompt_budget || payload.prompt_budget <= 0) return null
  return payload.estimated_input_tokens / payload.prompt_budget
}

let _verificationCounter = 0
function nextVerificationId(): string {
  return `vrun_${Date.now()}_${++_verificationCounter}`
}

/** 把 updater 应用到 history 的最后一条 record(若存在) */
function patchLatestVerification(
  history: VerificationRunRecord[],
  updater: (record: VerificationRunRecord) => VerificationRunRecord,
): VerificationRunRecord[] {
  if (history.length === 0) return history
  const next = history.slice()
  next[next.length - 1] = updater(next[next.length - 1])
  return next
}

export const useRuntimeInsightStore = create<RuntimeInsightState>((set) => ({
  ...initialState,

  setCheckpoints: (checkpoints) => set({ checkpoints }),
  setCheckpointDiff: (checkpointDiff) => set({ checkpointDiff }),
  addCheckpointEvent: (event) => set((s) => ({ checkpointEvents: [...s.checkpointEvents, event] })),
  setCheckpointWarning: (checkpointWarning) => set({ checkpointWarning }),
  startVerification: (verificationStarted) => {
    const record: VerificationRunRecord = {
      id: nextVerificationId(),
      startedAt: Date.now(),
      started: verificationStarted,
      stages: {},
      status: 'running',
    }
    set((s) => ({
      verificationRunning: true,
      verificationStarted,
      verificationStages: {},
      verificationFinished: null,
      verificationCompleted: null,
      verificationFailed: null,
      verificationHistory: [...s.verificationHistory, record].slice(-50),
    }))
    return record.id
  },
  upsertVerificationStage: (stage) => set((s) => ({
    verificationStages: { ...s.verificationStages, [stage.name]: stage },
    verificationHistory: patchLatestVerification(s.verificationHistory, (record) => ({
      ...record,
      stages: { ...record.stages, [stage.name]: stage },
    })),
  })),
  finishVerification: (verificationFinished) => set((s) => ({
    verificationRunning: false,
    verificationFinished,
    verificationHistory: patchLatestVerification(s.verificationHistory, (record) => ({
      ...record,
      finished: verificationFinished,
      finishedAt: Date.now(),
      status: 'finished',
    })),
  })),
  completeVerification: (verificationCompleted) => set((s) => ({
    verificationRunning: false,
    verificationCompleted,
    verificationHistory: patchLatestVerification(s.verificationHistory, (record) => ({
      ...record,
      completed: verificationCompleted,
      finishedAt: record.finishedAt ?? Date.now(),
      status: 'completed',
    })),
  })),
  failVerification: (verificationFailed) => set((s) => ({
    verificationRunning: false,
    verificationFailed,
    verificationHistory: patchLatestVerification(s.verificationHistory, (record) => ({
      ...record,
      failed: verificationFailed,
      finishedAt: record.finishedAt ?? Date.now(),
      status: 'failed',
    })),
  })),
  setAcceptanceDecision: (acceptanceDecision) => set({ acceptanceDecision }),
  setTodoSnapshot: (todoSnapshot) => set((s) => {
    const items = todoSnapshot?.items ?? []
    if (!todoSnapshot) {
      return { todoSnapshot: null, todoConflict: null }
    }
    if (items.length === 0) {
      return { todoSnapshot, todoConflict: null }
    }
    const now = Date.now()
    const todoHistory = { ...s.todoHistory }
    for (const item of items) {
      const prev = todoHistory[item.id]
      todoHistory[item.id] = {
        ...item,
        lastSeenAt: now,
        firstSeenAt: prev?.firstSeenAt ?? now,
      }
    }
    return { todoSnapshot, todoConflict: null, todoHistory }
  }),
  applyTodoSnapshot: (todoSnapshot) => set((s) => {
    const items = todoSnapshot?.items ?? []
    if (!todoSnapshot) {
      return { todoSnapshot: null }
    }
    if (items.length === 0) {
      return { todoSnapshot }
    }
    const now = Date.now()
    const todoHistory = { ...s.todoHistory }
    for (const item of items) {
      const prev = todoHistory[item.id]
      todoHistory[item.id] = {
        ...item,
        lastSeenAt: now,
        firstSeenAt: prev?.firstSeenAt ?? now,
      }
    }
    return { todoSnapshot, todoHistory }
  }),
  addTodoEvent: (event) => set((s) => ({ todoEvents: [...s.todoEvents, event] })),
  setTodoConflict: (todoConflict) => set({ todoConflict }),
  setBudgetChecked: (budgetChecked) => set({
    budgetChecked,
    budgetUsageRatio: calculateBudgetUsageRatio(budgetChecked),
  }),
  setBudgetEstimateFailed: (budgetEstimateFailed) => set({ budgetEstimateFailed }),
  setLedgerReconciled: (ledgerReconciled) => set({ ledgerReconciled }),
  reset: () => set(initialState),
}))
