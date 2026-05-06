import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useSessionStore } from './useSessionStore'
import { useChatStore } from './useChatStore'
import { useGatewayStore } from './useGatewayStore'
import { useRuntimeInsightStore } from './useRuntimeInsightStore'

beforeEach(() => {
  useSessionStore.setState((useSessionStore.getInitialState?.() ?? { projects: [], currentSessionId: '', currentProjectId: '', loading: false }) as any)
  useChatStore.setState({ messages: [], isGenerating: false, streamingMessageId: '', permissionRequests: [], tokenUsage: null, phase: '', stopReason: '' } as any)
  useGatewayStore.setState({ connectionState: 'disconnected', currentRunId: '', token: '', authenticated: false } as any)
  useRuntimeInsightStore.getState().reset()
})

describe('useSessionStore', () => {
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
    expect(useChatStore.getState().messages).toHaveLength(1)
    expect(useChatStore.getState().messages[0].role).toBe('user')
  })

  it('fetchSessions auto-selects first session and binds stream', async () => {
    const mockListSessions = vi.fn().mockResolvedValue({
      payload: { sessions: [{ id: 'sess-a', title: 'Alpha' }] },
    })
    const mockBindStream = vi.fn().mockResolvedValue({})
    const mockLoadSession = vi.fn().mockResolvedValue({ payload: { messages: [] } })
    const mockAPI = { listSessions: mockListSessions, bindStream: mockBindStream, loadSession: mockLoadSession } as any

    await useSessionStore.getState().fetchSessions(mockAPI)

    expect(useSessionStore.getState().currentSessionId).toBe('sess-a')
    expect(mockBindStream).toHaveBeenCalledWith({ session_id: 'sess-a', channel: 'all' })
  })

  it('fetchSessions does not auto-select when current session is valid', async () => {
    const mockListSessions = vi.fn().mockResolvedValue({
      payload: { sessions: [{ id: 'sess-a', title: 'Alpha' }] },
    })
    const mockBindStream = vi.fn().mockResolvedValue({})
    const mockAPI = { listSessions: mockListSessions, bindStream: mockBindStream } as any

    useSessionStore.setState({ currentSessionId: 'sess-b' })
    await useSessionStore.getState().fetchSessions(mockAPI)

    expect(useSessionStore.getState().currentSessionId).toBe('sess-b')
    expect(mockBindStream).not.toHaveBeenCalled()
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
