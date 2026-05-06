import { describe, it, expect } from 'vitest'
import { findCheckpointBeforeMessage } from './findCheckpointBeforeMessage'
import { type ChatMessage } from '@/stores/useChatStore'

function userMsg(id: string, content = ''): ChatMessage {
  return { id, role: 'user', type: 'text', content, timestamp: 0 }
}

function toolCallMsg(id: string, opts: { checkpointId?: string } = {}): ChatMessage {
  return {
    id,
    role: 'tool',
    type: 'tool_call',
    content: '',
    toolCallId: id,
    toolName: 'demo',
    toolStatus: 'done',
    checkpointId: opts.checkpointId,
    timestamp: 0,
  }
}

function assistantMsg(id: string, content = ''): ChatMessage {
  return { id, role: 'assistant', type: 'text', content, timestamp: 0 }
}

describe('findCheckpointBeforeMessage', () => {
  it('returns null for the very first user message', () => {
    const messages = [userMsg('u1')]
    expect(findCheckpointBeforeMessage(messages, 'u1')).toBeNull()
  })

  it('returns null when the message is not in the list', () => {
    const messages = [userMsg('u1'), toolCallMsg('t1', { checkpointId: 'cp_a' })]
    expect(findCheckpointBeforeMessage(messages, 'missing')).toBeNull()
  })

  it('returns the most recent tool_call checkpoint before the user message', () => {
    const messages = [
      userMsg('u1'),
      toolCallMsg('t1', { checkpointId: 'cp_a' }),
      assistantMsg('a1'),
      userMsg('u2'),
    ]
    expect(findCheckpointBeforeMessage(messages, 'u2')).toEqual({ checkpointId: 'cp_a' })
  })

  it('skips tool_call messages without checkpointId', () => {
    const messages = [
      userMsg('u1'),
      toolCallMsg('t1', { checkpointId: 'cp_a' }),
      toolCallMsg('t2'), // no checkpoint
      userMsg('u2'),
    ]
    expect(findCheckpointBeforeMessage(messages, 'u2')).toEqual({ checkpointId: 'cp_a' })
  })

  it('returns null when only non-tool_call messages precede the user message', () => {
    const messages = [
      assistantMsg('a1', 'welcome'),
      userMsg('u1'),
    ]
    expect(findCheckpointBeforeMessage(messages, 'u1')).toBeNull()
  })

  it('uses the latest preceding checkpoint when multiple exist', () => {
    const messages = [
      userMsg('u1'),
      toolCallMsg('t1', { checkpointId: 'cp_a' }),
      assistantMsg('a1'),
      userMsg('u2'),
      toolCallMsg('t2', { checkpointId: 'cp_b' }),
      assistantMsg('a2'),
      userMsg('u3'),
    ]
    expect(findCheckpointBeforeMessage(messages, 'u3')).toEqual({ checkpointId: 'cp_b' })
  })
})
