import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import ChatInput from './ChatInput'
import { useChatStore } from '@/stores/useChatStore'
import { useComposerStore } from '@/stores/useComposerStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useWorkspaceStore } from '@/stores/useWorkspaceStore'

const mockGatewayAPI = {
  listAvailableSkills: vi.fn(),
  listModels: vi.fn(),
  createSession: vi.fn(),
  uploadSessionAsset: vi.fn(),
  deleteSessionAsset: vi.fn(),
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
    mockGatewayAPI.createSession.mockResolvedValue({ payload: { session_id: 'session-created' } })
    mockGatewayAPI.uploadSessionAsset.mockResolvedValue({
      session_id: 'session-created',
      asset_id: 'asset-1',
      mime_type: 'image/png',
      size: 3,
    })
    mockGatewayAPI.deleteSessionAsset.mockResolvedValue({})
    mockGatewayAPI.run.mockResolvedValue({ session_id: 'session-created', run_id: 'run-1' })
    mockGatewayAPI.bindStream.mockResolvedValue({})
    if (typeof URL.createObjectURL !== 'function') {
      Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn() })
    }
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:preview-1')

    useComposerStore.setState({ composerText: '', attachments: [] })
    useSessionStore.setState({ currentSessionId: '' } as never)
    useWorkspaceStore.setState({ currentWorkspaceHash: 'workspace-b' } as never)
    useGatewayStore.setState({ currentRunId: '' } as never)
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

  it('renders the image attachment picker but keeps mention button absent', () => {
    render(<ChatInput />)

    expect(screen.getByRole('button', { name: /添加附件/ })).toBeInTheDocument()
    expect(screen.queryByTitle('引用上下文')).not.toBeInTheDocument()
  })

  it('uploads selected image and sends image-only input parts after creating a session', async () => {
    render(<ChatInput />)

    const file = new File(['img'], 'a.png', { type: 'image/png' })
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [file] } })

    await waitFor(() => {
      expect(screen.getByAltText('a.png')).toBeInTheDocument()
    })

    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    await waitFor(() => {
      expect(mockGatewayAPI.createSession).toHaveBeenCalled()
      expect(mockGatewayAPI.uploadSessionAsset).toHaveBeenCalledWith('session-created', file, 'workspace-b')
      expect(mockGatewayAPI.run).toHaveBeenCalledWith({
        session_id: 'session-created',
        input_parts: [
          { type: 'image', media: { asset_id: 'asset-1', mime_type: 'image/png', file_name: 'a.png' } },
        ],
        mode: 'build',
      })
    })
    expect(mockGatewayAPI.createSession.mock.invocationCallOrder[0]).toBeLessThan(
      mockGatewayAPI.bindStream.mock.invocationCallOrder[0],
    )
    expect(mockGatewayAPI.bindStream.mock.invocationCallOrder[0]).toBeLessThan(
      mockGatewayAPI.uploadSessionAsset.mock.invocationCallOrder[0],
    )
    expect(mockGatewayAPI.uploadSessionAsset.mock.invocationCallOrder[0]).toBeLessThan(
      mockGatewayAPI.run.mock.invocationCallOrder[0],
    )

    expect(useChatStore.getState().messages[0]).toMatchObject({
      role: 'user',
      attachments: [{ assetId: 'asset-1', previewUrl: 'blob:preview-1', workspaceHash: 'workspace-b' }],
    })
  })

  it('blocks image selection when the selected model rejects images but still allows text', async () => {
    mockGatewayAPI.listModels.mockResolvedValueOnce({
      payload: {
        models: [{
          id: 'text-model',
          name: 'Text Model',
          provider: 'openai',
          capability_hints: { image_input: 'unsupported' },
        }],
        selected_provider_id: 'openai',
        selected_model_id: 'text-model',
      },
    })
    render(<ChatInput />)

    // 等待模型能力加载完成：按钮 title 切换为不支持图片的提示，作为同步信号。
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /添加附件/ })).toHaveAttribute('title', '当前模型不支持图片，可添加文本文件')
    })

    // 图片附件仍受模型视觉能力限制，被拒绝。
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [new File(['img'], 'a.png', { type: 'image/png' })] } })
    await waitFor(() => {
      expect(useComposerStore.getState().attachments).toHaveLength(0)
    })

    // 文本附件不依赖模型视觉能力，仍可添加。
    fireEvent.change(input, { target: { files: [new File(['# hi'], 'note.md', { type: 'text/markdown' })] } })
    await waitFor(() => {
      expect(useComposerStore.getState().attachments).toHaveLength(1)
      expect(useComposerStore.getState().attachments[0].kind).toBe('text')
    })
  })

  it('blocks sending existing image attachments when the selected model rejects images', async () => {
    mockGatewayAPI.listModels.mockResolvedValueOnce({
      payload: {
        models: [{
          id: 'text-model',
          name: 'Text Model',
          provider: 'openai',
          capability_hints: { image_input: 'unsupported' },
        }],
        selected_provider_id: 'openai',
        selected_model_id: 'text-model',
      },
    })
    render(<ChatInput />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /添加附件/ })).toHaveAttribute('title', '当前模型不支持图片，可添加文本文件')
    })
    // 直接塞入图片附件（绕过选择校验），验证发送阶段对图片附件的阻断。
    useComposerStore.getState().addAttachmentFiles([new File(['img'], 'a.png', { type: 'image/png' })])
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    await waitFor(() => {
      expect(mockGatewayAPI.createSession).not.toHaveBeenCalled()
      expect(mockGatewayAPI.uploadSessionAsset).not.toHaveBeenCalled()
      expect(mockGatewayAPI.run).not.toHaveBeenCalled()
    })
  })

  it('uploads selected text file and sends it as an image part with the real text mime', async () => {
    useSessionStore.setState({ currentSessionId: 'session-1' } as never)
    mockGatewayAPI.uploadSessionAsset.mockResolvedValueOnce({
      session_id: 'session-1',
      asset_id: 'asset-text-1',
      mime_type: 'text/markdown',
      size: 5,
    })
    render(<ChatInput />)

    const file = new File(['# hi'], 'note.md', { type: 'text/markdown' })
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [file] } })

    // 文本附件以 chip 形式预览，展示文件名。
    await waitFor(() => {
      expect(screen.getByText('note.md')).toBeInTheDocument()
    })

    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    await waitFor(() => {
      expect(mockGatewayAPI.uploadSessionAsset).toHaveBeenCalledWith('session-1', file, 'workspace-b')
      // 文本附件复用 type:'image' + 真实文本 mime，由 runtime 按白名单内联为 text part。
      expect(mockGatewayAPI.run).toHaveBeenCalledWith({
        session_id: 'session-1',
        input_parts: [
          { type: 'image', media: { asset_id: 'asset-text-1', mime_type: 'text/markdown', file_name: 'note.md' } },
        ],
        mode: 'build',
      })
    })

    expect(useChatStore.getState().messages[0]).toMatchObject({
      role: 'user',
      attachments: [{ assetId: 'asset-text-1', kind: 'text', name: 'note.md' }],
    })
  })

  it('rejects unsupported file types with a clear error', async () => {
    render(<ChatInput />)

    const file = new File(['%PDF-1.4'], 'doc.pdf', { type: 'application/pdf' })
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [file] } })

    await waitFor(() => {
      // 未知类型应被拒绝，不进入附件列表。
      expect(useComposerStore.getState().attachments).toHaveLength(0)
    })
  })

  it('deletes uploaded session assets when run fails', async () => {
    useSessionStore.setState({ currentSessionId: 'session-1' } as never)
    mockGatewayAPI.uploadSessionAsset.mockResolvedValueOnce({
      session_id: 'session-1',
      asset_id: 'asset-failed',
      mime_type: 'image/png',
      size: 3,
    })
    mockGatewayAPI.run.mockRejectedValueOnce(new Error('run failed'))
    render(<ChatInput />)

    const file = new File(['img'], 'failed.png', { type: 'image/png' })
    const input = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [file] } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    await waitFor(() => {
      expect(mockGatewayAPI.deleteSessionAsset).toHaveBeenCalledWith('session-1', 'asset-failed', 'workspace-b')
    })
    expect(useChatStore.getState().messages).toHaveLength(0)
  })

  it('treats slash text as a normal message when an image is attached', async () => {
    useSessionStore.setState({ currentSessionId: 'session-1' } as never)
    mockGatewayAPI.uploadSessionAsset.mockResolvedValueOnce({
      session_id: 'session-1',
      asset_id: 'asset-2',
      mime_type: 'image/png',
      size: 3,
    })
    render(<ChatInput />)

    const file = new File(['img'], 'slash.png', { type: 'image/png' })
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(fileInput, { target: { files: [file] } })
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '/memo' } })
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })

    await waitFor(() => {
      expect(mockGatewayAPI.executeSystemTool).not.toHaveBeenCalled()
      expect(mockGatewayAPI.uploadSessionAsset).toHaveBeenCalledWith('session-1', file, 'workspace-b')
      expect(mockGatewayAPI.run).toHaveBeenCalledWith(expect.objectContaining({
        session_id: 'session-1',
        input_parts: [
          { type: 'text', text: '/memo' },
          { type: 'image', media: { asset_id: 'asset-2', mime_type: 'image/png', file_name: 'slash.png' } },
        ],
      }))
    })
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

  it('sends session id when cancelling an active run', async () => {
    useSessionStore.setState({ currentSessionId: 'session-1' } as never)
    useGatewayStore.setState({ currentRunId: 'run-1' } as never)
    useChatStore.setState({ isGenerating: true } as never)
    mockGatewayAPI.cancel.mockResolvedValueOnce({ payload: { canceled: true, run_id: 'run-1' } })
    render(<ChatInput />)

    fireEvent.click(screen.getByTitle('停止生成'))

    await waitFor(() => {
      expect(mockGatewayAPI.cancel).toHaveBeenCalledWith({ session_id: 'session-1', run_id: 'run-1' })
    })
  })

  it('falls back to run id only when cancelling without an active session', async () => {
    useSessionStore.setState({ currentSessionId: '' } as never)
    useGatewayStore.setState({ currentRunId: 'run-1' } as never)
    useChatStore.setState({ isGenerating: true } as never)
    mockGatewayAPI.cancel.mockResolvedValueOnce({ payload: { canceled: true, run_id: 'run-1' } })
    render(<ChatInput />)

    fireEvent.click(screen.getByTitle(/停止生成/))

    await waitFor(() => {
      expect(mockGatewayAPI.cancel).toHaveBeenCalledWith({ run_id: 'run-1' })
    })
  })

  it('does not reset generating state when no cancel request is sent', async () => {
    const resetGeneratingState = vi.spyOn(useChatStore.getState(), 'resetGeneratingState')
    useSessionStore.setState({ currentSessionId: '' } as never)
    useGatewayStore.setState({ currentRunId: '' } as never)
    useChatStore.setState({ isGenerating: true } as never)
    render(<ChatInput />)

    fireEvent.click(screen.getByTitle(/停止生成/))

    await waitFor(() => {
      expect(mockGatewayAPI.cancel).not.toHaveBeenCalled()
    })
    expect(resetGeneratingState).not.toHaveBeenCalled()
    resetGeneratingState.mockRestore()
  })
})
