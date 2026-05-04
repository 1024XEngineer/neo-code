import { describe, it, expect, beforeEach } from 'vitest'
import { useChatStore } from '@/stores/useChatStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { handleGatewayEvent } from './eventBridge'
import { EventType } from '@/api/protocol'

function createMockGatewayAPI() {
  return {
    listSessions: async () => ({ payload: { sessions: [] } }),
    loadSession: async () => ({ payload: { messages: [] } }),
    bindStream: async () => ({}),
  } as any
}

beforeEach(() => {
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
})
