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
  type VerificationCompletedPayload,
  type VerificationFailedPayload,
  type VerificationFinishedPayload,
  type VerificationStageFinishedPayload,
  type VerificationStartedPayload,
} from '@/api/protocol'

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
  acceptanceDecision: AcceptanceDecidedPayload | null
  todoSnapshot: TodoSnapshot | null
  todoEvents: TodoEventPayload[]
  todoConflict: TodoEventPayload | null
  budgetChecked: BudgetCheckedPayload | null
  budgetEstimateFailed: BudgetEstimateFailedPayload | null
  ledgerReconciled: LedgerReconciledPayload | null
  budgetUsageRatio: number | null

  setCheckpoints: (checkpoints: CheckpointEntry[]) => void
  setCheckpointDiff: (diff: CheckpointDiffResultPayload | null) => void
  addCheckpointEvent: (event: CheckpointCreatedPayload | CheckpointRestoredPayload | CheckpointUndoRestorePayload) => void
  setCheckpointWarning: (warning: CheckpointWarningPayload | null) => void
  startVerification: (payload: VerificationStartedPayload) => void
  upsertVerificationStage: (payload: VerificationStageFinishedPayload) => void
  finishVerification: (payload: VerificationFinishedPayload) => void
  completeVerification: (payload: VerificationCompletedPayload) => void
  failVerification: (payload: VerificationFailedPayload) => void
  setAcceptanceDecision: (payload: AcceptanceDecidedPayload | null) => void
  setTodoSnapshot: (snapshot: TodoSnapshot | null) => void
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
  acceptanceDecision: null as AcceptanceDecidedPayload | null,
  todoSnapshot: null as TodoSnapshot | null,
  todoEvents: [] as TodoEventPayload[],
  todoConflict: null as TodoEventPayload | null,
  budgetChecked: null as BudgetCheckedPayload | null,
  budgetEstimateFailed: null as BudgetEstimateFailedPayload | null,
  ledgerReconciled: null as LedgerReconciledPayload | null,
  budgetUsageRatio: null as number | null,
}

function calculateBudgetUsageRatio(payload: BudgetCheckedPayload): number | null {
  if (!payload.prompt_budget || payload.prompt_budget <= 0) return null
  return payload.estimated_input_tokens / payload.prompt_budget
}

export const useRuntimeInsightStore = create<RuntimeInsightState>((set) => ({
  ...initialState,

  setCheckpoints: (checkpoints) => set({ checkpoints }),
  setCheckpointDiff: (checkpointDiff) => set({ checkpointDiff }),
  addCheckpointEvent: (event) => set((s) => ({ checkpointEvents: [...s.checkpointEvents, event] })),
  setCheckpointWarning: (checkpointWarning) => set({ checkpointWarning }),
  startVerification: (verificationStarted) => set({
    verificationRunning: true,
    verificationStarted,
    verificationStages: {},
    verificationFinished: null,
    verificationCompleted: null,
    verificationFailed: null,
  }),
  upsertVerificationStage: (stage) => set((s) => ({
    verificationStages: { ...s.verificationStages, [stage.name]: stage },
  })),
  finishVerification: (verificationFinished) => set({ verificationRunning: false, verificationFinished }),
  completeVerification: (verificationCompleted) => set({ verificationRunning: false, verificationCompleted }),
  failVerification: (verificationFailed) => set({ verificationRunning: false, verificationFailed }),
  setAcceptanceDecision: (acceptanceDecision) => set({ acceptanceDecision }),
  setTodoSnapshot: (todoSnapshot) => set({ todoSnapshot }),
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
