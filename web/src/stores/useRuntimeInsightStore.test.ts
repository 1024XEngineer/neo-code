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

  it('startVerification appends a history record', () => {
    const store = useRuntimeInsightStore.getState()
    store.startVerification({ completion_passed: true })
    expect(useRuntimeInsightStore.getState().verificationHistory).toHaveLength(1)
    expect(useRuntimeInsightStore.getState().verificationHistory[0].status).toBe('running')
  })

  it('upsertVerificationStage updates the latest history record', () => {
    const store = useRuntimeInsightStore.getState()
    store.startVerification({ completion_passed: true })
    store.upsertVerificationStage({ name: 'lint', status: 'passed', summary: 'all good' })
    const latest = useRuntimeInsightStore.getState().verificationHistory[0]
    expect(latest.stages.lint.status).toBe('passed')
    expect(latest.stages.lint.summary).toBe('all good')
  })

  it('finishVerification updates history status', () => {
    const store = useRuntimeInsightStore.getState()
    store.startVerification({ completion_passed: true })
    store.finishVerification({ acceptance_status: 'accepted' })
    expect(useRuntimeInsightStore.getState().verificationHistory[0].status).toBe('finished')
  })

  it('failVerification updates history status', () => {
    const store = useRuntimeInsightStore.getState()
    store.startVerification({ completion_passed: true })
    store.failVerification({ stop_reason: 'error', error_class: 'TestError' })
    expect(useRuntimeInsightStore.getState().verificationHistory[0].status).toBe('failed')
  })

  it('reset clears verificationHistory', () => {
    const store = useRuntimeInsightStore.getState()
    store.startVerification({ completion_passed: true })
    store.reset()
    expect(useRuntimeInsightStore.getState().verificationHistory).toHaveLength(0)
  })

  it('setTodoSnapshot clears any stale todoConflict on a valid update', () => {
    const store = useRuntimeInsightStore.getState()
    store.setTodoConflict({ action: 'todo_conflict', reason: 'todo_not_found' })
    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe('todo_not_found')

    store.setTodoSnapshot({
      items: [{ id: 'a', content: 'task', status: 'pending', required: true, revision: 1 }],
      summary: { total: 1, required_total: 1, required_completed: 0, required_failed: 0, required_open: 1 },
    })

    expect(useRuntimeInsightStore.getState().todoConflict).toBeNull()
    expect(useRuntimeInsightStore.getState().todoSnapshot?.items?.[0].id).toBe('a')
  })

  it('setTodoSnapshot clears conflict on valid update', () => {
    const store = useRuntimeInsightStore.getState()
    store.setTodoConflict({ action: 'todo_conflict', reason: 'todo_not_found' })
    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe('todo_not_found')

    store.setTodoSnapshot({
      items: [{ id: 'a', content: 'task', status: 'pending', required: true, revision: 1 }],
      summary: { total: 1, required_total: 1, required_completed: 0, required_failed: 0, required_open: 1 },
    })

    expect(useRuntimeInsightStore.getState().todoConflict).toBeNull()
    expect(useRuntimeInsightStore.getState().todoSnapshot?.items?.[0].id).toBe('a')
  })

  it('setTodoSnapshot with empty items clears current snapshot items and conflict while preserving history', () => {
    const store = useRuntimeInsightStore.getState()
    store.setTodoSnapshot({
      items: [{ id: 'a', content: 'task a', status: 'in_progress', required: true, revision: 1 }],
      summary: { total: 1, required_total: 1, required_completed: 0, required_failed: 0, required_open: 1 },
    })
    store.setTodoConflict({ action: 'todo_conflict', reason: 'todo_not_found' })

    store.setTodoSnapshot({
      items: [],
      summary: { total: 0, required_total: 0, required_completed: 0, required_failed: 0, required_open: 0 },
    })

    const state = useRuntimeInsightStore.getState()
    expect(state.todoSnapshot?.items).toEqual([])
    expect(state.todoHistory.a).toBeDefined()
    expect(state.todoConflict).toBeNull()
  })

  it('applyTodoSnapshot updates snapshot but does NOT clear conflict', () => {
    const store = useRuntimeInsightStore.getState()
    store.setTodoConflict({ action: 'todo_conflict', reason: 'revision_conflict' })
    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe('revision_conflict')

    store.applyTodoSnapshot({
      items: [{ id: 'b', content: 'task b', status: 'pending', required: true, revision: 2 }],
      summary: { total: 1, required_total: 1, required_completed: 0, required_failed: 0, required_open: 1 },
    })

    const state = useRuntimeInsightStore.getState()
    expect(state.todoSnapshot?.items?.[0].id).toBe('b')
    // conflict must be preserved
    expect(state.todoConflict?.reason).toBe('revision_conflict')
  })

  it('applyTodoSnapshot with empty items clears current snapshot items but preserves conflict and history', () => {
    const store = useRuntimeInsightStore.getState()
    store.setTodoSnapshot({
      items: [{ id: 'a', content: 'task a', status: 'in_progress', required: true, revision: 1 }],
    })
    store.setTodoConflict({ action: 'todo_conflict', reason: 'todo_not_found' })

    store.applyTodoSnapshot({ items: [] })

    const state = useRuntimeInsightStore.getState()
    expect(state.todoSnapshot?.items).toEqual([])
    expect(state.todoConflict?.reason).toBe('todo_not_found')
    expect(state.todoHistory.a).toBeDefined()
  })

  it('setTodoSnapshot accumulates todoHistory across replacements', () => {
    const store = useRuntimeInsightStore.getState()
    store.setTodoSnapshot({
      items: [{ id: 'a', content: 'task a', status: 'pending', required: true, revision: 1 }],
    })
    store.setTodoSnapshot({
      items: [{ id: 'b', content: 'task b', status: 'in_progress', required: true, revision: 1 }],
    })

    const state = useRuntimeInsightStore.getState()
    expect(Object.keys(state.todoHistory).sort()).toEqual(['a', 'b'])
    expect(state.todoSnapshot?.items?.map((i) => i.id)).toEqual(['b'])
    expect(state.todoHistory.a.firstSeenAt).toBeLessThanOrEqual(state.todoHistory.b.firstSeenAt)
  })
})
