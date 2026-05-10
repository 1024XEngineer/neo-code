import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mapHistoryMessages, useSessionStore } from './useSessionStore'
import { useChatStore } from './useChatStore'
import { useGatewayStore } from './useGatewayStore'
import { useRuntimeInsightStore } from './useRuntimeInsightStore'

beforeEach(() => {
  useSessionStore.setState((useSessionStore.getInitialState?.() ?? { projects: [], currentSessionId: '', currentProjectId: '', loading: false }) as any)
  useChatStore.setState({ messages: [], isGenerating: false, streamingMessageId: '', permissionRequests: [], tokenUsage: null, phase: '', stopReason: '' } as any)
  useGatewayStore.setState({ connectionState: 'disconnected', currentRunId: '', token: '', authenticated: false } as any)
  useRuntimeInsightStore.getState().reset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useSessionStore', () => {
  it('mapHistoryMessages skips internal system acceptance reminders', () => {
    const mapped = mapHistoryMessages([
      { role: 'user', content: 'start' },
      {
        role: 'system',
        content: [
          '<acceptance_continue>',
          '<completion_blocked_reason>pending_todo</completion_blocked_reason>',
          '</acceptance_continue>',
        ].join(''),
      },
      { role: 'assistant', content: 'visible answer' },
    ])

    expect(mapped.map((m) => m.content)).toEqual(['start', 'visible answer'])
    expect(mapped.every((m) => m.content.includes('acceptance_continue') === false)).toBe(true)
  })

  it('mapHistoryMessages skips leaked assistant acceptance control text', () => {
    const mapped = mapHistoryMessages([
      {
        role: 'assistant',
        content: '<acceptance_continue><todo_convergence></todo_convergence></acceptance_continue>',
      },
      { role: 'assistant', content: 'normal assistant text' },
      {
        role: 'assistant',
        content: 'prefix <completion_blocked_reason>pending_todo</completion_blocked_reason>',
      },
    ])

    expect(mapped).toHaveLength(1)
    expect(mapped[0].content).toBe('normal assistant text')
  })

  it('mapHistoryMessages keeps normal messages and merges tool results', () => {
    const mapped = mapHistoryMessages([
      { role: 'user', content: 'please inspect' },
      {
        role: 'assistant',
        content: 'calling tool',
        tool_calls: [
          { id: 'call-1', name: 'filesystem_read_file', arguments: '{"path":"README.md"}' },
        ],
      },
      { role: 'tool', content: 'file content', tool_call_id: 'call-1' },
    ])

    expect(mapped).toHaveLength(3)
    expect(mapped[0]).toMatchObject({ role: 'user', type: 'text', content: 'please inspect' })
    expect(mapped[1]).toMatchObject({ role: 'assistant', type: 'text', content: 'calling tool' })
    expect(mapped[2]).toMatchObject({
      role: 'tool',
      type: 'tool_call',
      toolName: 'filesystem_read_file',
      toolCallId: 'call-1',
      toolResult: 'file content',
      toolStatus: 'done',
    })
  })

  it('createSession clears messages and resets session state', () => {
    useChatStore.getState().addMessage({ id: '1', role: 'user', content: 'hello', type: 'text', timestamp: 1 })
    useSessionStore.setState({ currentSessionId: 'sess-1' })

    useSessionStore.getState().createSession()

    expect(useChatStore.getState().messages).toHaveLength(0)
    expect(useSessionStore.getState().currentSessionId).toBe('')
  })

  it('prepareNewChat also clears state and does not set temp id', () => {
    useSessionStore.setState({ currentSessionId: 'sess-1' })
    useChatStore.getState().addMessage({ id: '1', role: 'user', content: 'hello', type: 'text', timestamp: 1 })

    useSessionStore.getState().prepareNewChat()

    expect(useChatStore.getState().messages).toHaveLength(0)
    expect(useSessionStore.getState().currentSessionId).toBe('')
    expect(useSessionStore.getState().currentProjectId).toBe('')
  })

  it('initializeActiveSession binds stream for valid session id', async () => {
    const mockBindStream = vi.fn().mockResolvedValue({})
    const mockAPI = { bindStream: mockBindStream } as any

    useSessionStore.setState({ currentSessionId: 'sess-1' })
    await useSessionStore.getState().initializeActiveSession(mockAPI)

    expect(mockBindStream).toHaveBeenCalledWith({ session_id: 'sess-1', channel: 'all' })
  })

  it('initializeActiveSession skips binding for empty session id', async () => {
    const mockBindStream = vi.fn().mockResolvedValue({})
    const mockAPI = { bindStream: mockBindStream } as any

    useSessionStore.setState({ currentSessionId: '' })
    await useSessionStore.getState().initializeActiveSession(mockAPI)

    expect(mockBindStream).not.toHaveBeenCalled()
  })

  it('switchSession binds stream and loads session data', async () => {
    const setMessagesSpy = vi.spyOn(useChatStore.getState(), 'setMessages')
    const addMessageSpy = vi.spyOn(useChatStore.getState(), 'addMessage')
    const mockBindStream = vi.fn().mockResolvedValue({})
    const mockLoadSession = vi.fn().mockResolvedValue({
      payload: {
        messages: [
          { role: 'user', content: 'hello', tool_calls: [] },
        ],
      },
    })
    const mockAPI = { bindStream: mockBindStream, loadSession: mockLoadSession } as any

    await useSessionStore.getState().switchSession('sess-2', mockAPI)

    expect(mockBindStream).toHaveBeenCalledWith({ session_id: 'sess-2', channel: 'all' })
    expect(setMessagesSpy).toHaveBeenCalledTimes(1)
    expect(addMessageSpy).not.toHaveBeenCalled()
    expect(useChatStore.getState().messages).toHaveLength(1)
    expect(useChatStore.getState().messages[0].role).toBe('user')
  })

  it('fetchSessions auto-selects first session and binds stream', async () => {
    const setMessagesSpy = vi.spyOn(useChatStore.getState(), 'setMessages')
    const addMessageSpy = vi.spyOn(useChatStore.getState(), 'addMessage')
    const mockListSessions = vi.fn().mockResolvedValue({
      payload: {
        sessions: [{
          id: 'sess-a',
          title: 'Alpha',
          created_at: '2026-05-09T01:00:00Z',
          updated_at: '2026-05-09T02:00:00Z',
        }],
      },
    })
    const mockBindStream = vi.fn().mockResolvedValue({})
    const mockLoadSession = vi.fn().mockResolvedValue({
      payload: { messages: [{ role: 'assistant', content: 'loaded history', tool_calls: [] }] },
    })
    const mockAPI = { listSessions: mockListSessions, bindStream: mockBindStream, loadSession: mockLoadSession } as any

    await useSessionStore.getState().fetchSessions(mockAPI)

    expect(useSessionStore.getState().currentSessionId).toBe('sess-a')
    expect(mockBindStream).toHaveBeenCalledWith({ session_id: 'sess-a', channel: 'all' })
    expect(setMessagesSpy).toHaveBeenCalled()
    expect(addMessageSpy).not.toHaveBeenCalled()
    expect(useChatStore.getState().messages[0]).toMatchObject({ role: 'assistant', content: 'loaded history' })
  })

  it('fetchSessions does not auto-select when current session is valid', async () => {
    const mockListSessions = vi.fn().mockResolvedValue({
      payload: {
        sessions: [{
          id: 'sess-a',
          title: 'Alpha',
          created_at: '2026-05-09T01:00:00Z',
          updated_at: '2026-05-09T02:00:00Z',
        }],
      },
    })
    const mockBindStream = vi.fn().mockResolvedValue({})
    const mockAPI = { listSessions: mockListSessions, bindStream: mockBindStream } as any

    useSessionStore.setState({ currentSessionId: 'sess-b' })
    await useSessionStore.getState().fetchSessions(mockAPI)

    expect(useSessionStore.getState().currentSessionId).toBe('sess-b')
    expect(mockBindStream).not.toHaveBeenCalled()
  })

  it('fetchSessions uses the newer of created_at/updated_at as display time', async () => {
    const mockListSessions = vi.fn().mockResolvedValue({
      payload: {
        sessions: [{
          id: 'sess-a',
          title: 'Alpha',
          created_at: '2026-05-09T09:30:00Z',
          updated_at: '2026-05-09T08:30:00Z',
        }],
      },
    })
    const mockBindStream = vi.fn().mockResolvedValue({})
    const mockLoadSession = vi.fn().mockResolvedValue({ payload: { messages: [] } })
    const mockAPI = { listSessions: mockListSessions, bindStream: mockBindStream, loadSession: mockLoadSession } as any

    await useSessionStore.getState().fetchSessions(mockAPI)

    const session = useSessionStore.getState().projects[0].sessions[0]
    expect(session.time).toBe('2026-05-09T09:30:00.000Z')
  })

  it('fetchSessions uses stable fallback time when created_at and updated_at are both invalid', async () => {
    const mockListSessions = vi.fn().mockResolvedValue({
      payload: {
        sessions: [{
          id: 'sess-invalid-time',
          title: 'InvalidTime',
          created_at: 'not-a-date',
          updated_at: '',
        }],
      },
    })
    const mockBindStream = vi.fn().mockResolvedValue({})
    const mockLoadSession = vi.fn().mockResolvedValue({ payload: { messages: [] } })
    const mockAPI = { listSessions: mockListSessions, bindStream: mockBindStream, loadSession: mockLoadSession } as any

    await useSessionStore.getState().fetchSessions(mockAPI)

    const session = useSessionStore.getState().projects[0].sessions[0]
    expect(session.time).toBe('1970-01-01T00:00:00.000Z')
  })

  it('switchSession concurrently fetches todos and checkpoints', async () => {
    const mockBindStream = vi.fn().mockResolvedValue({})
    const mockLoadSession = vi.fn().mockResolvedValue({
      payload: { messages: [{ role: 'user', content: 'hello', tool_calls: [] }] },
    })
    const mockListSessionTodos = vi.fn().mockResolvedValue({
      payload: {
        items: [{ id: 't1', content: 'x', status: 'open', required: true, revision: 1 }],
        summary: { total: 1, required_total: 1, required_completed: 0, required_failed: 0, required_open: 1 },
      },
    })
    const mockListCheckpoints = vi.fn().mockResolvedValue({
      payload: [{ checkpoint_id: 'cp1', session_id: 'sess-2', reason: 'test', status: 'active', restorable: true, created_at_ms: Date.now() }],
    })
    const mockAPI = {
      bindStream: mockBindStream,
      loadSession: mockLoadSession,
      listSessionTodos: mockListSessionTodos,
      listCheckpoints: mockListCheckpoints,
    } as any

    await useSessionStore.getState().switchSession('sess-2', mockAPI)

    expect(mockLoadSession).toHaveBeenCalledWith('sess-2')
    expect(mockListSessionTodos).toHaveBeenCalledWith('sess-2')
    expect(mockListCheckpoints).toHaveBeenCalledWith({ session_id: 'sess-2', limit: 50 })

    const insightStore = useRuntimeInsightStore.getState()
    expect(insightStore.todoSnapshot?.items?.[0].id).toBe('t1')
    expect(insightStore.checkpoints[0].checkpoint_id).toBe('cp1')
  })
})
