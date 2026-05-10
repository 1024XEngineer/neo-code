import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import ChatInput from './ChatInput'
import { useChatStore } from '@/stores/useChatStore'
import { useComposerStore } from '@/stores/useComposerStore'
import { useSessionStore } from '@/stores/useSessionStore'

const mockGatewayAPI = {
  listAvailableSkills: vi.fn(),
  listModels: vi.fn(),
  run: vi.fn(),
  bindStream: vi.fn(),
  cancel: vi.fn(),
  compact: vi.fn(),
  executeSystemTool: vi.fn(),
  activateSessionSkill: vi.fn(),
  deactivateSessionSkill: vi.fn(),
}

vi.mock('@/context/RuntimeProvider', () => ({
  useGatewayAPI: () => mockGatewayAPI,
}))

vi.mock('./ModelSelector', () => ({
  default: () => <div data-testid="model-selector" />,
}))

describe('ChatInput', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGatewayAPI.listAvailableSkills.mockResolvedValue({
      payload: {
        skills: [
          {
            descriptor: { id: 'skill.demo', description: 'demo skill' },
            active: true,
          },
        ],
      },
    })
    mockGatewayAPI.listModels.mockResolvedValue({
      payload: {
        models: [],
        selected_provider_id: '',
        selected_model_id: '',
      },
    })

    useComposerStore.setState({ composerText: '' })
    useSessionStore.setState({ currentSessionId: '' } as never)
    useChatStore.setState({
      isGenerating: false,
      messages: [],
      permissionRequests: [],
      agentMode: 'build',
      permissionMode: 'default',
    } as never)
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

  it('opens slash suggestions for bare slash and loads skills immediately', async () => {
    render(<ChatInput />)

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: '/' } })

    await waitFor(() => {
      expect(screen.getByTestId('slash-command-menu')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(mockGatewayAPI.listAvailableSkills).toHaveBeenCalledWith(undefined)
    })
    await waitFor(() => {
      expect(screen.getByText('/skill.demo')).toBeInTheDocument()
    })
  })

  it('keeps slash menu visible for fuzzy inputs like /w', async () => {
    render(<ChatInput />)

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: '/w' } })

    await waitFor(() => {
      expect(screen.getByTestId('slash-command-menu')).toBeInTheDocument()
    })
    expect(screen.getAllByText((_, el) => Boolean(el?.textContent?.includes('/help'))).length).toBeGreaterThan(0)
  })

  it('supports keyboard navigation, tab completion, and escape for slash menu', async () => {
    render(<ChatInput />)

    const textarea = screen.getByRole('textbox') as HTMLTextAreaElement
    fireEvent.change(textarea, { target: { value: '/' } })

    await waitFor(() => {
      expect(screen.getByTestId('slash-command-menu')).toBeInTheDocument()
    })

    fireEvent.keyDown(textarea, { key: 'ArrowDown' })
    fireEvent.keyDown(textarea, { key: 'Tab' })
    expect(textarea.value).toBe('/compact ')

    fireEvent.change(textarea, { target: { value: '/' } })
    await waitFor(() => {
      expect(screen.getByTestId('slash-command-menu')).toBeInTheDocument()
    })
    fireEvent.keyDown(textarea, { key: 'Escape' })
    await waitFor(() => {
      expect(screen.queryByTestId('slash-command-menu')).not.toBeInTheDocument()
    })
  })

  it('does not render the unimplemented attachment and mention buttons', () => {
    render(<ChatInput />)

    expect(screen.queryByTitle('附件文件')).not.toBeInTheDocument()
    expect(screen.queryByTitle('引用上下文')).not.toBeInTheDocument()
  })
})
