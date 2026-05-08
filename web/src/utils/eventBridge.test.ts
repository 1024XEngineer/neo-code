import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useChatStore } from '@/stores/useChatStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { useUIStore } from '@/stores/useUIStore'
import { handleGatewayEvent, resetEventBridgeCursors } from './eventBridge'
import { EventType } from '@/api/protocol'

function createMockGatewayAPI(overrides: Record<string, unknown> = {}) {
  return {
    listSessions: async () => ({ payload: { sessions: [] } }),
    loadSession: async () => ({ payload: { messages: [] } }),
    bindStream: async () => ({}),
    checkpointDiff: async () => ({ payload: { checkpoint_id: 'cp', files: {}, patch: '' } }),
    ...overrides,
  } as any
}

beforeEach(() => {
  resetEventBridgeCursors()
  useChatStore.setState({
    messages: [],
    isGenerating: false,
    streamingMessageId: '',
    permissionRequests: [],
    tokenUsage: null,
    phase: '',
    stopReason: '',
  } as any)
  useGatewayStore.setState({
    connectionState: 'disconnected',
    currentRunId: '',
    token: '',
    authenticated: false,
  } as any)
  useRuntimeInsightStore.getState().reset()
  useUIStore.setState({ toasts: [], fileChanges: [] } as any)
})

describe('eventBridge', () => {
  it('AgentChunk adds assistant message and appends text', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.AgentChunk,
      payload: { payload: { runtime_event_type: EventType.AgentChunk, payload: 'hello' } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    const msgs = useChatStore.getState().messages
    expect(msgs).toHaveLength(1)
    expect(msgs[0].role).toBe('assistant')
    expect(msgs[0].content).toBe('hello')
  })

  it('AgentChunk appends to existing streaming message', () => {
    const api = createMockGatewayAPI()
    const store = useChatStore.getState()
    store.addMessage({ id: 's1', role: 'assistant', content: 'He', type: 'text', timestamp: 1 })
    store.setStreamingMessageId('s1')

    handleGatewayEvent({
      type: EventType.AgentChunk,
      payload: { payload: { runtime_event_type: EventType.AgentChunk, payload: 'llo' } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useChatStore.getState().messages[0].content).toBe('Hello')
  })

  it('AgentDone finalizes message from parts array', () => {
    const api = createMockGatewayAPI()
    const store = useChatStore.getState()
    store.addMessage({ id: 's1', role: 'assistant', content: 'He', type: 'text', timestamp: 1 })
    store.setStreamingMessageId('s1')
    store.setGenerating(true)

    handleGatewayEvent({
      type: EventType.AgentDone,
      payload: { payload: { runtime_event_type: EventType.AgentDone, payload: { parts: [{ text: 'Hello world' }] } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useChatStore.getState().messages[0].content).toBe('Hello world')
    expect(useChatStore.getState().isGenerating).toBe(false)
    expect(useChatStore.getState().streamingMessageId).toBe('')
  })

  it('AgentDone falls back to content field when parts missing', () => {
    const api = createMockGatewayAPI()
    const store = useChatStore.getState()
    store.addMessage({ id: 's1', role: 'assistant', content: '', type: 'text', timestamp: 1 })
    store.setStreamingMessageId('s1')
    store.setGenerating(true)

    handleGatewayEvent({
      type: EventType.AgentDone,
      payload: { payload: { runtime_event_type: EventType.AgentDone, payload: { content: 'fallback' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useChatStore.getState().messages[0].content).toBe('fallback')
  })

  it('InputNormalizes sets currentSessionId and currentRunId', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.InputNormalized,
      payload: { payload: { runtime_event_type: EventType.InputNormalized, payload: { session_id: 'sess-1', run_id: 'run-1' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useSessionStore.getState().currentSessionId).toBe('sess-1')
    expect(useGatewayStore.getState().currentRunId).toBe('run-1')
  })

  it('ToolStart adds a tool call message', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.ToolStart,
      payload: { payload: { runtime_event_type: EventType.ToolStart, payload: { name: 'read_file', id: 'tc1', arguments: '{"path":"/a"}' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    const msgs = useChatStore.getState().messages
    expect(msgs).toHaveLength(1)
    expect(msgs[0].type).toBe('tool_call')
    expect(msgs[0].toolName).toBe('read_file')
  })

  it('ToolResult updates an existing tool call message', () => {
    const api = createMockGatewayAPI()
    // 先触发 ToolStart 创建工具消息
    handleGatewayEvent({
      type: EventType.ToolStart,
      payload: { payload: { runtime_event_type: EventType.ToolStart, payload: { name: 'read_file', id: 'tc1', arguments: '{"path":"/a"}' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    // 再触发 ToolResult 更新结果
    handleGatewayEvent({
      type: EventType.ToolResult,
      payload: { payload: { runtime_event_type: EventType.ToolResult, payload: { tool_call_id: 'tc1', content: 'file contents', is_error: false } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    const msgs = useChatStore.getState().messages
    expect(msgs).toHaveLength(1)
    expect(msgs[0].role).toBe('tool')
    expect(msgs[0].toolResult).toBe('file contents')
    expect(msgs[0].toolStatus).toBe('done')
  })

  it('BudgetChecked updates runtime insight budget state', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.BudgetChecked,
      payload: { payload: { runtime_event_type: EventType.BudgetChecked, payload: { attempt_seq: 1, request_hash: 'h1', action: 'allow', estimated_input_tokens: 80, prompt_budget: 100 } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useRuntimeInsightStore.getState().budgetChecked?.action).toBe('allow')
    expect(useRuntimeInsightStore.getState().budgetUsageRatio).toBe(0.8)
  })

  it('VerificationStageFinished upserts verifier status', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.VerificationStageFinished,
      payload: { payload: { runtime_event_type: EventType.VerificationStageFinished, payload: { name: 'test', status: 'passed', summary: 'ok' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useRuntimeInsightStore.getState().verificationStages.test.status).toBe('passed')
  })

  it('AcceptanceDecided stores acceptance decision', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.AcceptanceDecided,
      payload: { payload: { runtime_event_type: EventType.AcceptanceDecided, payload: { status: 'accepted', user_visible_summary: 'done' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useRuntimeInsightStore.getState().acceptanceDecision?.status).toBe('accepted')
  })

  it('TodoSnapshotUpdated stores todo snapshot', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.TodoSnapshotUpdated,
      payload: { payload: { runtime_event_type: EventType.TodoSnapshotUpdated, payload: { action: 'snapshot', items: [{ id: 't1', content: 'x', status: 'blocked', required: true, blocked_reason: 'wait', revision: 1 }], summary: { total: 1, required_total: 1, required_completed: 0, required_failed: 0, required_open: 1 } } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useRuntimeInsightStore.getState().todoSnapshot?.items?.[0].blocked_reason).toBe('wait')
  })

  it('TodoSnapshotUpdated does NOT clear TodoConflict', () => {
    const api = createMockGatewayAPI()
    // First, trigger a conflict
    handleGatewayEvent({
      type: EventType.TodoConflict,
      payload: { payload: { runtime_event_type: EventType.TodoConflict, payload: { action: 'update', reason: 'revision_conflict', items: [{ id: 't1', content: 'x', status: 'in_progress', required: true, revision: 1 }] } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe('revision_conflict')

    // Then, snapshot update should preserve conflict
    handleGatewayEvent({
      type: EventType.TodoSnapshotUpdated,
      payload: { payload: { runtime_event_type: EventType.TodoSnapshotUpdated, payload: { action: 'snapshot', items: [{ id: 't1', content: 'x', status: 'in_progress', required: true, revision: 1 }] } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe('revision_conflict')
    expect(useRuntimeInsightStore.getState().todoSnapshot?.items?.[0].id).toBe('t1')
  })

  it('TodoUpdated clears TodoConflict', () => {
    const api = createMockGatewayAPI()
    // Set conflict first
    handleGatewayEvent({
      type: EventType.TodoConflict,
      payload: { payload: { runtime_event_type: EventType.TodoConflict, payload: { action: 'update', reason: 'revision_conflict' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    expect(useRuntimeInsightStore.getState().todoConflict).not.toBeNull()

    // Successful update should clear conflict
    handleGatewayEvent({
      type: EventType.TodoUpdated,
      payload: { payload: { runtime_event_type: EventType.TodoUpdated, payload: { action: 'update', items: [{ id: 't1', content: 'x', status: 'completed', required: true, revision: 2 }] } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useRuntimeInsightStore.getState().todoConflict).toBeNull()
    expect(useRuntimeInsightStore.getState().todoSnapshot?.items?.[0].id).toBe('t1')
  })

  it('TodoConflict revision_conflict does NOT show toast', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.TodoConflict,
      payload: { payload: { runtime_event_type: EventType.TodoConflict, payload: { action: 'update', reason: 'revision_conflict' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe('revision_conflict')
    expect(useUIStore.getState().toasts).toHaveLength(0)
  })

  it('TodoConflict invalid_arguments shows info toast', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.TodoConflict,
      payload: { payload: { runtime_event_type: EventType.TodoConflict, payload: { action: 'update', reason: 'invalid_arguments' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useRuntimeInsightStore.getState().todoConflict?.reason).toBe('invalid_arguments')
    expect(useUIStore.getState().toasts).toHaveLength(1)
    expect(useUIStore.getState().toasts[0].type).toBe('info')
    expect(useUIStore.getState().toasts[0].message).toContain('invalid_arguments')
  })

  it('Checkpoint events are stored in runtime insight state', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.CheckpointCreated,
      payload: { payload: { runtime_event_type: EventType.CheckpointCreated, payload: { checkpoint_id: 'cp1', code_checkpoint_ref: 'c', session_checkpoint_ref: 's', commit_hash: 'abc', reason: 'pre_write' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useRuntimeInsightStore.getState().checkpointEvents[0]).toMatchObject({ checkpoint_id: 'cp1' })
  })

  it('VerificationStarted creates a verification ChatMessage', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.VerificationStarted,
      payload: { payload: { runtime_event_type: EventType.VerificationStarted, payload: { completion_passed: true } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    const verifyMsg = useChatStore.getState().messages.find((m) => m.type === 'verification')
    expect(verifyMsg).toBeDefined()
    expect(verifyMsg?.verificationData?.status).toBe('running')
    expect(useRuntimeInsightStore.getState().verificationHistory).toHaveLength(1)
  })

  it('VerificationStageFinished updates the verification message', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.VerificationStarted,
      payload: { payload: { runtime_event_type: EventType.VerificationStarted, payload: { completion_passed: true } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    handleGatewayEvent({
      type: EventType.VerificationStageFinished,
      payload: { payload: { runtime_event_type: EventType.VerificationStageFinished, payload: { name: 'lint', status: 'passed', summary: 'all good' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    const verifyMsg = useChatStore.getState().messages.find((m) => m.type === 'verification')
    expect(verifyMsg?.verificationData?.stages.lint.status).toBe('passed')
    expect(verifyMsg?.verificationData?.stages.lint.summary).toBe('all good')
  })

  it('VerificationFinished updates history and chat message', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.VerificationStarted,
      payload: { payload: { runtime_event_type: EventType.VerificationStarted, payload: { completion_passed: true } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    handleGatewayEvent({
      type: EventType.VerificationFinished,
      payload: { payload: { runtime_event_type: EventType.VerificationFinished, payload: { acceptance_status: 'accepted' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    const verifyMsg = useChatStore.getState().messages.find((m) => m.type === 'verification')
    expect(verifyMsg?.verificationData?.status).toBe('finished')
    expect(useRuntimeInsightStore.getState().verificationHistory[0].status).toBe('finished')
  })

  it('AcceptanceDecided creates an acceptance ChatMessage', () => {
    const api = createMockGatewayAPI()
    handleGatewayEvent({
      type: EventType.AcceptanceDecided,
      payload: { payload: { runtime_event_type: EventType.AcceptanceDecided, payload: { status: 'accepted', user_visible_summary: 'looks good' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    const msg = useChatStore.getState().messages.find((m) => m.type === 'acceptance')
    expect(msg).toBeDefined()
    expect(msg?.acceptanceData?.status).toBe('accepted')
    expect(msg?.acceptanceData?.user_visible_summary).toBe('looks good')
    expect(useRuntimeInsightStore.getState().acceptanceDecision?.status).toBe('accepted')
  })

  it('CheckpointCreated attaches checkpointId to the latest done tool_call', () => {
    const api = createMockGatewayAPI()
    // 先创建并完成一个 tool call
    handleGatewayEvent({
      type: EventType.ToolStart,
      payload: { payload: { runtime_event_type: EventType.ToolStart, payload: { name: 'write_file', id: 'tc1', arguments: '{}' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    handleGatewayEvent({
      type: EventType.ToolResult,
      payload: { payload: { runtime_event_type: EventType.ToolResult, payload: { tool_call_id: 'tc1', content: 'ok' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    // 然后创建 checkpoint
    handleGatewayEvent({
      type: EventType.CheckpointCreated,
      payload: { payload: { runtime_event_type: EventType.CheckpointCreated, payload: { checkpoint_id: 'cp1', code_checkpoint_ref: 'c', session_checkpoint_ref: 's', commit_hash: 'abc', reason: 'pre_write' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    const toolMsg = useChatStore.getState().messages.find((m) => m.type === 'tool_call')
    expect(toolMsg?.checkpointId).toBe('cp1')
    expect(toolMsg?.checkpointStatus).toBe('available')
  })

  it('clearMessages resets eventBridge cursors so new session does not inherit prior checkpoint', () => {
    const api = createMockGatewayAPI()

    // 会话 A:tool_call + checkpoint,使 _latestCheckpointId=cp_old
    handleGatewayEvent({
      type: EventType.ToolStart,
      payload: { payload: { runtime_event_type: EventType.ToolStart, payload: { name: 'filesystem_write_file', id: 'tcA1', arguments: '{"path":"a.txt"}' } } },
      session_id: 'sess-A',
      run_id: 'run-A',
    }, api)
    handleGatewayEvent({
      type: EventType.ToolResult,
      payload: { payload: { runtime_event_type: EventType.ToolResult, payload: { tool_call_id: 'tcA1', content: 'ok' } } },
      session_id: 'sess-A',
      run_id: 'run-A',
    }, api)
    handleGatewayEvent({
      type: EventType.CheckpointCreated,
      payload: { payload: { runtime_event_type: EventType.CheckpointCreated, payload: { checkpoint_id: 'cp_old', code_checkpoint_ref: 'c', session_checkpoint_ref: 's', commit_hash: 'abc', reason: 'pre_write' } } },
      session_id: 'sess-A',
      run_id: 'run-A',
    }, api)

    // 同一会话内的下一次写文件应继承 cp_old(确认游标确实已被设置)
    handleGatewayEvent({
      type: EventType.ToolStart,
      payload: { payload: { runtime_event_type: EventType.ToolStart, payload: { name: 'filesystem_write_file', id: 'tcA2', arguments: '{"path":"a2.txt"}' } } },
      session_id: 'sess-A',
      run_id: 'run-A',
    }, api)
    const inheritedChange = useUIStore.getState().fileChanges.find((c) => c.path === 'a2.txt')
    expect(inheritedChange?.checkpoint_id).toBe('cp_old')

    // 模拟切换会话:clearMessages 是 switchSession/createSession/prepareNewChat 的统一入口
    useUIStore.getState().clearFileChanges()
    useChatStore.getState().clearMessages()

    // 会话 B:再触发一次写文件,但不再有 CheckpointCreated 事件
    handleGatewayEvent({
      type: EventType.ToolStart,
      payload: { payload: { runtime_event_type: EventType.ToolStart, payload: { name: 'filesystem_write_file', id: 'tcB', arguments: '{"path":"b.txt"}' } } },
      session_id: 'sess-B',
      run_id: 'run-B',
    }, api)

    const newChange = useUIStore.getState().fileChanges.find((c) => c.path === 'b.txt')
    expect(newChange).toBeDefined()
    expect(newChange?.checkpoint_id).toBeUndefined()
  })

  it('replaces transient tool diffs with run-scoped checkpoint diff on end-of-turn checkpoint', async () => {
    const checkpointDiff = vi.fn(async () => ({
      payload: {
        checkpoint_id: 'cp2',
        files: { modified: ['a.txt'] },
        patch: '--- a/a.txt\n+++ b/a.txt\n@@ -1,3 +1,3 @@\n line 1\n-A\n+C\n line 3\n@@ -10,3 +10,3 @@\n line 10\n-B\n+D\n line 12\n',
      },
    }))
    const api = createMockGatewayAPI({ checkpointDiff })

    handleGatewayEvent({
      type: EventType.InputNormalized,
      payload: { payload: { runtime_event_type: EventType.InputNormalized, payload: { session_id: 'sess-1', run_id: 'run-1' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    handleGatewayEvent({
      type: EventType.ToolStart,
      payload: { payload: { runtime_event_type: EventType.ToolStart, payload: { name: 'filesystem_write_file', id: 'tc1', arguments: '{"path":"a.txt"}' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    handleGatewayEvent({
      type: EventType.ToolDiff,
      payload: { payload: { runtime_event_type: EventType.ToolDiff, payload: { tool_call_id: 'tc1', tool_name: 'filesystem_write_file', file_path: 'a.txt', diff: '--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-A\n+B\n' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    handleGatewayEvent({
      type: EventType.ToolDiff,
      payload: { payload: { runtime_event_type: EventType.ToolDiff, payload: { tool_call_id: 'tc2', tool_name: 'filesystem_write_file', file_path: 'a.txt', diff: '--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-B\n+C\n' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useUIStore.getState().fileChanges[0]?.hunks?.[0]?.lines.map((line) => line.content)).toEqual([
      '@@ -1 +1 @@',
      'B',
      'C',
    ])

    handleGatewayEvent({
      type: EventType.CheckpointCreated,
      payload: { payload: { runtime_event_type: EventType.CheckpointCreated, payload: { checkpoint_id: 'cp2', code_checkpoint_ref: 'c', session_checkpoint_ref: 's', commit_hash: '', reason: 'end_of_turn' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    await Promise.resolve()
    await Promise.resolve()

    expect(checkpointDiff).toHaveBeenCalledWith({
      session_id: 'sess-1',
      run_id: 'run-1',
      checkpoint_id: 'cp2',
      scope: 'run',
    })
    const changes = useUIStore.getState().fileChanges
    expect(changes).toHaveLength(1)
    expect(changes[0]).toMatchObject({ path: 'a.txt', status: 'modified', additions: 2, deletions: 2 })
    expect(changes[0].hunks).toHaveLength(2)
    expect(changes[0].hunks?.[0]?.lines.map((line) => line.content)).toEqual([
      '@@ -1,3 +1,3 @@',
      'line 1',
      'A',
      'C',
      'line 3',
    ])
    expect(changes[0].hunks?.[1]?.lines.map((line) => line.content)).toEqual([
      '@@ -10,3 +10,3 @@',
      'line 10',
      'B',
      'D',
      'line 12',
    ])
  })

  it('stores hunk structure for transient tool diffs before aggregate checkpoint diff arrives', () => {
    const api = createMockGatewayAPI()

    handleGatewayEvent({
      type: EventType.ToolStart,
      payload: { payload: { runtime_event_type: EventType.ToolStart, payload: { name: 'filesystem_write_file', id: 'tc1', arguments: '{"path":"a.txt"}' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    handleGatewayEvent({
      type: EventType.ToolDiff,
      payload: {
        payload: {
          runtime_event_type: EventType.ToolDiff,
          payload: {
            tool_call_id: 'tc1',
            tool_name: 'filesystem_write_file',
            file_path: 'a.txt',
            diff: '--- a/a.txt\n+++ b/a.txt\n@@ -1,3 +1,3 @@\n line 1\n-old\n+new\n line 3\n@@ -10,2 +10,3 @@\n line 10\n+line 11\n line 12\n',
          },
        },
      },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    const change = useUIStore.getState().fileChanges.find((entry) => entry.path === 'a.txt')
    expect(change?.hunks).toHaveLength(2)
    expect(change?.hunks?.[0]?.lines.map((line) => line.type)).toEqual(['header', 'context', 'del', 'add', 'context'])
    expect(change?.hunks?.[1]?.lines.map((line) => line.content)).toEqual([
      '@@ -10,2 +10,3 @@',
      'line 10',
      'line 11',
      'line 12',
    ])
  })

  it('keeps transient tool diffs visible when backend sends simplified diff without @@ header', () => {
    const api = createMockGatewayAPI()

    handleGatewayEvent({
      type: EventType.ToolStart,
      payload: { payload: { runtime_event_type: EventType.ToolStart, payload: { name: 'filesystem_write_file', id: 'tc1', arguments: '{"path":"a.txt"}' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    handleGatewayEvent({
      type: EventType.ToolDiff,
      payload: {
        payload: {
          runtime_event_type: EventType.ToolDiff,
          payload: {
            tool_call_id: 'tc1',
            tool_name: 'filesystem_write_file',
            file_path: 'a.txt',
            diff: '--- a/a.txt\n+++ b/a.txt\n-old\n+new\n',
          },
        },
      },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    const change = useUIStore.getState().fileChanges.find((entry) => entry.path === 'a.txt')
    expect(change).toMatchObject({ additions: 1, deletions: 1 })
    expect(change?.hunks).toHaveLength(1)
    expect(change?.hunks?.[0]?.lines.map((line) => line.content)).toEqual(['old', 'new'])
  })

  it('filters final run-scoped modified entries that have no renderable patch', async () => {
    const checkpointDiff = vi.fn(async () => ({
      payload: {
        checkpoint_id: 'cp2',
        files: { modified: ['a.txt', 'b.txt'] },
        patch: '--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n',
      },
    }))
    const api = createMockGatewayAPI({ checkpointDiff })

    handleGatewayEvent({
      type: EventType.InputNormalized,
      payload: { payload: { runtime_event_type: EventType.InputNormalized, payload: { session_id: 'sess-1', run_id: 'run-1' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    handleGatewayEvent({
      type: EventType.ToolStart,
      payload: { payload: { runtime_event_type: EventType.ToolStart, payload: { name: 'filesystem_write_file', id: 'tc1', arguments: '{"path":"b.txt"}' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)

    expect(useUIStore.getState().fileChanges.find((entry) => entry.path === 'b.txt')).toBeDefined()

    handleGatewayEvent({
      type: EventType.CheckpointCreated,
      payload: { payload: { runtime_event_type: EventType.CheckpointCreated, payload: { checkpoint_id: 'cp2', code_checkpoint_ref: 'c', session_checkpoint_ref: 's', commit_hash: '', reason: 'end_of_turn' } } },
      session_id: 'sess-1',
      run_id: 'run-1',
    }, api)
    await Promise.resolve()
    await Promise.resolve()

    const changes = useUIStore.getState().fileChanges
    expect(changes).toHaveLength(1)
    expect(changes[0]).toMatchObject({ path: 'a.txt', additions: 1, deletions: 1 })
    expect(changes.find((entry) => entry.path === 'b.txt')).toBeUndefined()
  })
})
