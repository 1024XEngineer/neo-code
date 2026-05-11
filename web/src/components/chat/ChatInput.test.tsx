import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import ChatInput from './ChatInput'
import { useChatStore } from '@/stores/useChatStore'
import { useComposerStore } from '@/stores/useComposerStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'

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

async function submitSlashCommand(command: string) {
  const textarea = screen.getByRole('textbox') as HTMLTextAreaElement
  fireEvent.change(textarea, { target: { value: command } })
  fireEvent.keyDown(textarea, { key: 'Enter' })
}

function renderWithBudget(input: {
  action: string
  estimated_input_tokens: number
  prompt_budget: number
  context_window?: number
}) {
  useRuntimeInsightStore.getState().setBudgetChecked({
    attempt_seq: 1,
    request_hash: 'budget-test',
    ...input,
  })
  render(<ChatInput />)
  return screen.getByTestId('budget-token-ring')
}

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
    useRuntimeInsightStore.getState().reset()
    useChatStore.setState({
      isGenerating: false,
      isCompacting: false,
      compactMode: '',
      compactMessage: '',
      messages: [],
      permissionRequests: [],
      agentMode: 'build',
      permissionMode: 'default',
      tokenUsage: null,
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
  it('blocks normal sends while compaction is running', async () => {
    useChatStore.getState().startCompacting('manual', 'Compacting context...')
    render(<ChatInput />)

    const textarea = screen.getByRole('textbox')
    fireEvent.change(textarea, { target: { value: 'hello' } })
    fireEvent.keyDown(textarea, { key: 'Enter' })

    await waitFor(() => {
      expect(mockGatewayAPI.run).not.toHaveBeenCalled()
    })
    expect(useChatStore.getState().messages).toHaveLength(0)
  })

  it('blocks duplicate compact commands while compaction is running', async () => {
    useSessionStore.setState({ currentSessionId: 'session-1' } as never)
    useChatStore.getState().startCompacting('manual', 'Compacting context...')
    render(<ChatInput />)

    await submitSlashCommand('/compact')

    await waitFor(() => {
      expect(mockGatewayAPI.compact).not.toHaveBeenCalled()
    })
  })

  it('sets compact state immediately when running /compact', async () => {
    useSessionStore.setState({ currentSessionId: 'session-1' } as never)
    let resolveCompact: (value: unknown) => void = () => {}
    mockGatewayAPI.compact.mockReturnValueOnce(new Promise((resolve) => {
      resolveCompact = resolve
    }))
    render(<ChatInput />)

    await submitSlashCommand('/compact')

    await waitFor(() => {
      expect(useChatStore.getState().isCompacting).toBe(true)
    })

    resolveCompact({})
    await waitFor(() => {
      expect(useChatStore.getState().isCompacting).toBe(false)
    })
  })

  it('executes /memo without session id and shows payload.Content', async () => {
    mockGatewayAPI.executeSystemTool.mockResolvedValueOnce({
      payload: {
        Content: 'User Memo:\n- [user] coding preference',
      },
    })
    render(<ChatInput />)

    await submitSlashCommand('/memo')

    await waitFor(() => {
      expect(mockGatewayAPI.executeSystemTool).toHaveBeenCalledWith('', '', 'memo_list', {})
    })
    await waitFor(() => {
      expect(useChatStore.getState().messages.some((msg) => msg.type === 'system' && msg.content.includes('coding preference'))).toBe(true)
    })
  })

  it('uses fallback text when memo payload has no content field', async () => {
    mockGatewayAPI.executeSystemTool.mockResolvedValueOnce({ payload: {} })
    render(<ChatInput />)

    await submitSlashCommand('/memo')

    await waitFor(() => {
      expect(useChatStore.getState().messages.some((msg) => msg.type === 'system' && msg.content === 'Memo query complete')).toBe(true)
    })
  })

  it('executes /remember and /forget without session id', async () => {
    mockGatewayAPI.executeSystemTool
      .mockResolvedValueOnce({ payload: { Content: 'Memory saved: [user] keep tests strict' } })
      .mockResolvedValueOnce({ payload: { Content: 'Removed 1 memo(s) matching \"strict\".' } })
    render(<ChatInput />)

    await submitSlashCommand('/remember keep tests strict')
    await waitFor(() => {
      expect(mockGatewayAPI.executeSystemTool).toHaveBeenNthCalledWith(1, '', '', 'memo_remember', {
        type: 'user',
        title: 'keep tests strict',
        content: 'keep tests strict',
      })
    })

    await submitSlashCommand('/forget strict')
    await waitFor(() => {
      expect(mockGatewayAPI.executeSystemTool).toHaveBeenNthCalledWith(2, '', '', 'memo_remove', {
        keyword: 'strict',
        scope: 'all',
      })
    })
  })

  it('keeps argument validation for /remember and /forget', async () => {
    render(<ChatInput />)

    await submitSlashCommand('/remember')
    await submitSlashCommand('/forget')

    await waitFor(() => {
      expect(mockGatewayAPI.executeSystemTool).not.toHaveBeenCalled()
    })
  })

  it('shows a green budget ring below the warning threshold', () => {
    const ring = renderWithBudget({
      action: 'allow',
      estimated_input_tokens: 80,
      prompt_budget: 100,
      context_window: 200,
    })

    expect(ring).toHaveAttribute('stroke', 'var(--success)')
  })

  it('shows a yellow budget ring near the automatic compact threshold', () => {
    const ring = renderWithBudget({
      action: 'allow',
      estimated_input_tokens: 90,
      prompt_budget: 100,
      context_window: 200,
    })

    expect(ring).toHaveAttribute('stroke', 'var(--warning)')
  })

  it('shows a red budget ring near the context window limit', () => {
    const ring = renderWithBudget({
      action: 'allow',
      estimated_input_tokens: 190,
      prompt_budget: 100,
      context_window: 200,
    })

    expect(ring).toHaveAttribute('stroke', 'var(--error)')
  })

  it('falls back to prompt budget as the limit when context window is missing', () => {
    const ring = renderWithBudget({
      action: 'allow',
      estimated_input_tokens: 100,
      prompt_budget: 100,
    })

    expect(ring).toHaveAttribute('stroke', 'var(--error)')
  })

  it('honors compact budget action as a yellow color override', () => {
    const ring = renderWithBudget({
      action: 'compact',
      estimated_input_tokens: 20,
      prompt_budget: 100,
      context_window: 200,
    })

    expect(ring).toHaveAttribute('stroke', 'var(--warning)')
  })

  it('honors stop budget action as a red color override', () => {
    const ring = renderWithBudget({
      action: 'stop',
      estimated_input_tokens: 20,
      prompt_budget: 100,
      context_window: 200,
    })

    expect(ring).toHaveAttribute('stroke', 'var(--error)')
  })
})
