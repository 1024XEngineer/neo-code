import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import ChatInput from './ChatInput'
import { useChatStore } from '@/stores/useChatStore'
import { useComposerStore } from '@/stores/useComposerStore'
import { useSessionStore } from '@/stores/useSessionStore'

vi.mock('@/context/RuntimeProvider', () => ({
  useGatewayAPI: () => null,
}))

describe('ChatInput', () => {
  beforeEach(() => {
    useComposerStore.setState({ composerText: '' })
    useSessionStore.setState({ currentSessionId: '' } as any)
    useChatStore.setState({
      isGenerating: false,
      messages: [],
      permissionRequests: [],
      agentMode: 'build',
      permissionMode: 'default',
    } as any)
  })

  it('shows the default/bypass selector in build mode', () => {
    render(<ChatInput />)

    expect(screen.getByRole('button', { name: 'Build' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'default' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'bypass' })).toBeInTheDocument()
  })

  it('hides the permission selector after switching to plan mode', () => {
    render(<ChatInput />)

    fireEvent.click(screen.getByRole('button', { name: 'Build' }))

    expect(screen.getByRole('button', { name: 'Plan' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'default' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'bypass' })).not.toBeInTheDocument()
  })

  it('does not render the unimplemented attachment and mention buttons', () => {
    render(<ChatInput />)

    expect(screen.queryByTitle('附加文件')).not.toBeInTheDocument()
    expect(screen.queryByTitle('引用上下文')).not.toBeInTheDocument()
  })
})
