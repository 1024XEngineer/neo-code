import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useChatStore, createUserMessage } from './useChatStore'

beforeEach(() => {
  if (typeof URL.revokeObjectURL !== 'function') {
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
  }
  vi.restoreAllMocks()
  useChatStore.setState({
    messages: [],
    isGenerating: false,
    isCompacting: false,
    compactMode: '',
    compactMessage: '',
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

  it('setMessages replaces messages atomically', () => {
    const store = useChatStore.getState()
    store.addMessage({ id: 'old', role: 'user', content: 'old', type: 'text', timestamp: 1 })

    store.setMessages([
      { id: 'new-1', role: 'user', content: 'first', type: 'text', timestamp: 2 },
      { id: 'new-2', role: 'assistant', content: 'second', type: 'text', timestamp: 3 },
    ])

    expect(useChatStore.getState().messages.map((m) => m.id)).toEqual(['new-1', 'new-2'])
  })

  it('setMessages preserves unrelated chat state', () => {
    const store = useChatStore.getState()
    store.setGenerating(true)
    store.addPermissionRequest({
      request_id: 'r1',
      tool_name: 'filesystem_read_file',
      tool_category: 'filesystem',
      action_type: 'read',
      operation: 'read',
      target_type: 'file',
      target: 'README.md',
    } as any)
    store.updateTokenUsage({ input_tokens: 1, output_tokens: 2, total_tokens: 3 } as any)
    store.setPhase('running')
    store.setStopReason('manual')

    store.setMessages([{ id: 'hist', role: 'assistant', content: 'loaded', type: 'text', timestamp: 4 }])

    const state = useChatStore.getState()
    expect(state.messages.map((m) => m.id)).toEqual(['hist'])
    expect(state.isGenerating).toBe(true)
    expect(state.permissionRequests).toHaveLength(1)
    expect(state.tokenUsage).toEqual({ input_tokens: 1, output_tokens: 2, total_tokens: 3 })
    expect(state.phase).toBe('running')
    expect(state.stopReason).toBe('manual')
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
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    const store = useChatStore.getState()
    store.addMessage({
      id: 'msg-1',
      role: 'user',
      content: 'hi',
      type: 'text',
      timestamp: 1,
      attachments: [{ id: 'att-1', mimeType: 'image/png', previewUrl: 'blob:clear-preview' }],
    })
    store.clearMessages()
    expect(useChatStore.getState().messages).toHaveLength(0)
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:clear-preview')
  })

  it('removeMessage revokes sent attachment preview URLs', () => {
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    const store = useChatStore.getState()
    store.addMessage({
      id: 'msg-1',
      role: 'user',
      content: 'image',
      type: 'text',
      timestamp: 1,
      attachments: [{ id: 'att-1', mimeType: 'image/png', previewUrl: 'blob:preview-1' }],
    })

    store.removeMessage('msg-1')

    expect(useChatStore.getState().messages).toEqual([])
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:preview-1')
  })

  it('setMessages revokes preview URLs from replaced messages', () => {
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    const store = useChatStore.getState()
    store.addMessage({
      id: 'old',
      role: 'user',
      content: 'old image',
      type: 'text',
      timestamp: 1,
      attachments: [{ id: 'att-1', mimeType: 'image/png', previewUrl: 'blob:old-preview' }],
    })

    store.setMessages([{ id: 'new', role: 'assistant', content: 'new', type: 'text', timestamp: 2 }])

    expect(useChatStore.getState().messages.map((m) => m.id)).toEqual(['new'])
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:old-preview')
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

  it('truncateFromMessage revokes preview URLs from removed messages', () => {
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    const store = useChatStore.getState()
    store.addMessage({ id: 'u1', role: 'user', content: 'keep', type: 'text', timestamp: 1 })
    store.addMessage({
      id: 'u2',
      role: 'user',
      content: 'remove',
      type: 'text',
      timestamp: 2,
      attachments: [{ id: 'att-1', mimeType: 'image/png', previewUrl: 'blob:removed-preview' }],
    })

    store.truncateFromMessage('u2')

    expect(useChatStore.getState().messages.map((m) => m.id)).toEqual(['u1'])
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:removed-preview')
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

  it('tracks compact state independently from generation', () => {
    const store = useChatStore.getState()
    store.setGenerating(true)
    store.startCompacting('manual', 'Compacting context...')

    expect(useChatStore.getState().isGenerating).toBe(true)
    expect(useChatStore.getState().isCompacting).toBe(true)
    expect(useChatStore.getState().compactMode).toBe('manual')
    expect(useChatStore.getState().compactMessage).toBe('Compacting context...')

    store.finishCompacting()

    expect(useChatStore.getState().isGenerating).toBe(true)
    expect(useChatStore.getState().isCompacting).toBe(false)
    expect(useChatStore.getState().compactMode).toBe('')
  })

  it('resetGeneratingState clears stuck compact state', () => {
    const store = useChatStore.getState()
    store.startCompacting('manual', 'Compacting context...')
    store.resetGeneratingState()

    expect(useChatStore.getState().isCompacting).toBe(false)
    expect(useChatStore.getState().compactMessage).toBe('')
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
    store.startCompacting('manual', 'Compacting context...')
    store.clearMessages()
    expect(useChatStore.getState().permissionMode).toBe('default')
    expect(useChatStore.getState().isCompacting).toBe(false)
  })

  it('createUserMessage preserves attachment kind for text and image files', () => {
    const msg = createUserMessage('hi', [
      { id: 'a1', mimeType: 'image/png', kind: 'image', name: 'a.png' },
      { id: 'a2', mimeType: 'text/markdown', kind: 'text', name: 'b.md' },
    ])
    expect(msg.attachments).toEqual([
      { id: 'a1', mimeType: 'image/png', kind: 'image', name: 'a.png' },
      { id: 'a2', mimeType: 'text/markdown', kind: 'text', name: 'b.md' },
    ])
  })

  it('createUserMessage omits attachments when none provided', () => {
    const msg = createUserMessage('hi')
    expect(msg.attachments).toBeUndefined()
  })
})
