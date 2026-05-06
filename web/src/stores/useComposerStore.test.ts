import { describe, it, expect, beforeEach } from 'vitest'
import { useComposerStore } from './useComposerStore'

beforeEach(() => {
  useComposerStore.setState({ composerText: '' })
})

describe('useComposerStore', () => {
  it('starts with empty text', () => {
    expect(useComposerStore.getState().composerText).toBe('')
  })

  it('setComposerText updates the value', () => {
    useComposerStore.getState().setComposerText('hello')
    expect(useComposerStore.getState().composerText).toBe('hello')
  })

  it('overwrites existing text on subsequent setComposerText calls', () => {
    useComposerStore.getState().setComposerText('first')
    useComposerStore.getState().setComposerText('second')
    expect(useComposerStore.getState().composerText).toBe('second')
  })
})
