import { describe, it, expect, beforeEach } from 'vitest'
import { useChatStore } from './useChatStore'

beforeEach(() => {
  useChatStore.setState({
    messages: [],
    isGenerating: false,
    streamingMessageId: '',
    streamingThinkingMessageId: '',
    permissionRequests: [],
    tokenUsage: null,
    phase: '',
    stopReason: '',
    isTransitioning: false,
    agentMode: 'build',
    permissionMode: 'default',
  } as any)
})

describe('useChatStore', () => {
  it('addMessage appends a message', () => {
    useChatStore.getState().addMessage({
      id: 'msg-1',
      role: 'user',
      content: 'hello',
      type: 'text',
      timestamp: 1,
    })
    expect(useChatStore.getState().messages).toHaveLength(1)
    expect(useChatStore.getState().messages[0].content).toBe('hello')
  })

  it('appendChunk concatenates to streaming message', () => {
    const store = useChatStore.getState()
    store.addMessage({ id: 'stream-1', role: 'assistant', content: 'Hel', type: 'text', timestamp: 1 })
    store.setStreamingMessageId('stream-1')
    store.appendChunk('lo')
    expect(useChatStore.getState().messages[0].content).toBe('Hello')
  })

  it('finalizeMessage replaces content for streaming id', () => {
    const store = useChatStore.getState()
    store.addMessage({ id: 'stream-1', role: 'assistant', content: 'partial', type: 'text', timestamp: 1 })
    store.setStreamingMessageId('stream-1')
    store.finalizeMessage('stream-1', 'final text')
    expect(useChatStore.getState().messages[0].content).toBe('final text')
    expect(useChatStore.getState().streamingMessageId).toBe('')
  })

  it('clearMessages removes all messages', () => {
    const store = useChatStore.getState()
    store.addMessage({ id: 'msg-1', role: 'user', content: 'hi', type: 'text', timestamp: 1 })
    store.clearMessages()
    expect(useChatStore.getState().messages).toHaveLength(0)
  })

  it('truncateFromMessage removes the target and everything after it', () => {
    const store = useChatStore.getState()
    store.addMessage({ id: 'u1', role: 'user', content: 'hi', type: 'text', timestamp: 1 })
    store.addMessage({ id: 'a1', role: 'assistant', content: 'hello', type: 'text', timestamp: 2 })
    store.addMessage({ id: 'u2', role: 'user', content: 'follow', type: 'text', timestamp: 3 })
    store.addMessage({ id: 'a2', role: 'assistant', content: 'reply', type: 'text', timestamp: 4 })
    store.truncateFromMessage('u2')
    const remaining = useChatStore.getState().messages
    expect(remaining.map((m) => m.id)).toEqual(['u1', 'a1'])
  })

  it('truncateFromMessage clears generation-related state', () => {
    const store = useChatStore.getState()
    store.addMessage({ id: 'u1', role: 'user', content: 'hi', type: 'text', timestamp: 1 })
    store.addMessage({ id: 'a1', role: 'assistant', content: 'partial', type: 'text', timestamp: 2, streaming: true })
    store.setStreamingMessageId('a1')
    store.setGenerating(true)
    store.addPermissionRequest({
      request_id: 'r1',
      tool_name: 't',
      tool_category: '',
      action_type: '',
      operation: '',
      target_type: '',
      target: '',
    } as any)
    store.setPhase('running')
    store.setStopReason('something')

    store.truncateFromMessage('a1')
    const state = useChatStore.getState()
    expect(state.messages.map((m) => m.id)).toEqual(['u1'])
    expect(state.streamingMessageId).toBe('')
    expect(state.isGenerating).toBe(false)
    expect(state.permissionRequests).toEqual([])
    expect(state.phase).toBe('')
    expect(state.stopReason).toBe('')
  })

  it('truncateFromMessage is a no-op when the messageId is unknown', () => {
    const store = useChatStore.getState()
    store.addMessage({ id: 'u1', role: 'user', content: 'hi', type: 'text', timestamp: 1 })
    store.truncateFromMessage('not-found')
    expect(useChatStore.getState().messages.map((m) => m.id)).toEqual(['u1'])
  })

  it('truncateFromMessage handles the first message (clears all)', () => {
    const store = useChatStore.getState()
    store.addMessage({ id: 'u1', role: 'user', content: 'hi', type: 'text', timestamp: 1 })
    store.addMessage({ id: 'a1', role: 'assistant', content: 'hello', type: 'text', timestamp: 2 })
    store.truncateFromMessage('u1')
    expect(useChatStore.getState().messages).toHaveLength(0)
  })

  it('truncateFromMessage handles the last message (removes only that one)', () => {
    const store = useChatStore.getState()
    store.addMessage({ id: 'u1', role: 'user', content: 'hi', type: 'text', timestamp: 1 })
    store.addMessage({ id: 'a1', role: 'assistant', content: 'hello', type: 'text', timestamp: 2 })
    store.truncateFromMessage('a1')
    expect(useChatStore.getState().messages.map((m) => m.id)).toEqual(['u1'])
  })

  it('setGenerating toggles generation state', () => {
    useChatStore.getState().setGenerating(true)
    expect(useChatStore.getState().isGenerating).toBe(true)
    useChatStore.getState().setGenerating(false)
    expect(useChatStore.getState().isGenerating).toBe(false)
  })

  it('starts with default permission mode', () => {
    expect(useChatStore.getState().permissionMode).toBe('default')
  })

  it('setPermissionMode updates the permission mode', () => {
    useChatStore.getState().setPermissionMode('bypass')
    expect(useChatStore.getState().permissionMode).toBe('bypass')
  })

  it('clearMessages resets permission mode to default', () => {
    const store = useChatStore.getState()
    store.setPermissionMode('bypass')
    store.clearMessages()
    expect(useChatStore.getState().permissionMode).toBe('default')
  })
})
