import { describe, it, expect, beforeEach } from 'vitest'
import { useRuntimeInsightStore } from './useRuntimeInsightStore'

beforeEach(() => {
  useRuntimeInsightStore.getState().reset()
})

describe('useRuntimeInsightStore', () => {
  it('upserts verification stages by name', () => {
    const store = useRuntimeInsightStore.getState()

    store.upsertVerificationStage({ name: 'test', status: 'failed', reason: 'first' })
    store.upsertVerificationStage({ name: 'test', status: 'passed', summary: 'ok' })

    expect(useRuntimeInsightStore.getState().verificationStages.test.status).toBe('passed')
    expect(useRuntimeInsightStore.getState().verificationStages.test.summary).toBe('ok')
  })

  it('calculates budget usage ratio when prompt budget is available', () => {
    useRuntimeInsightStore.getState().setBudgetChecked({
      attempt_seq: 1,
      request_hash: 'hash-1',
      action: 'allow',
      estimated_input_tokens: 50,
      prompt_budget: 100,
    })

    expect(useRuntimeInsightStore.getState().budgetUsageRatio).toBe(0.5)
  })

  it('uses null budget usage ratio when prompt budget is zero', () => {
    useRuntimeInsightStore.getState().setBudgetChecked({
      attempt_seq: 1,
      request_hash: 'hash-1',
      action: 'allow',
      estimated_input_tokens: 50,
      prompt_budget: 0,
    })

    expect(useRuntimeInsightStore.getState().budgetUsageRatio).toBeNull()
  })

  it('resets all insight state', () => {
    const store = useRuntimeInsightStore.getState()
    store.setAcceptanceDecision({ status: 'accepted', user_visible_summary: 'done' })
    store.setTodoSnapshot({ summary: { total: 1, required_total: 1, required_completed: 1, required_failed: 0, required_open: 0 } })

    store.reset()

    expect(useRuntimeInsightStore.getState().acceptanceDecision).toBeNull()
    expect(useRuntimeInsightStore.getState().todoSnapshot).toBeNull()
  })
})
