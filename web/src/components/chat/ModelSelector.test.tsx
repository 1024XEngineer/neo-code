import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import ModelSelector from './ModelSelector'
import { useSessionStore } from '@/stores/useSessionStore'
import { useChatStore } from '@/stores/useChatStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useUIStore } from '@/stores/useUIStore'

let mockGatewayAPI: any = null

vi.mock('@/context/RuntimeProvider', () => ({
  useGatewayAPI: () => mockGatewayAPI,
}))

describe('ModelSelector', () => {
  beforeEach(() => {
    cleanup()
    mockGatewayAPI = null
    useSessionStore.setState({
      currentSessionId: '',
      currentProjectId: '',
      projects: [],
      loading: false,
      _switchAbort: null,
      _initialBindDone: false,
    } as any)
    useChatStore.setState({ isGenerating: false } as any)
    useGatewayStore.getState().reset()
    useUIStore.setState({
      showToast: vi.fn(),
    } as any)
  })

  it('does not auto-write the session model after loading a session-scoped model list', async () => {
    mockGatewayAPI = {
      listModels: vi.fn().mockResolvedValue({
        payload: {
          models: [{ id: 'gpt-4.1', name: 'GPT-4.1', provider: 'openai' }],
          selected_provider_id: 'openai',
          selected_model_id: 'gpt-4.1',
        },
      }),
      setSessionModel: vi.fn(),
      selectProviderModel: vi.fn(),
    }
    useSessionStore.setState({ currentSessionId: 'session-1' } as any)

    render(<ModelSelector />)

    await screen.findByText('openai / GPT-4.1')
    expect(mockGatewayAPI.setSessionModel).not.toHaveBeenCalled()
    expect(mockGatewayAPI.selectProviderModel).not.toHaveBeenCalled()
  })

  it('defers a session model change until generation completes and applies it once through the global switch path', async () => {
    mockGatewayAPI = {
      listModels: vi.fn().mockResolvedValue({
        payload: {
          models: [
            { id: 'gpt-4.1', name: 'GPT-4.1', provider: 'openai' },
            { id: 'gpt-4o', name: 'GPT-4o', provider: 'openai' },
          ],
          selected_provider_id: 'openai',
          selected_model_id: 'gpt-4.1',
        },
      }),
      setSessionModel: vi.fn(),
      selectProviderModel: vi.fn().mockResolvedValue(undefined),
    }
    useSessionStore.setState({ currentSessionId: 'session-1' } as any)
    useChatStore.setState({ isGenerating: true } as any)

    const view = render(<ModelSelector />)

    await screen.findByText('openai / GPT-4.1')
    fireEvent.click(screen.getByRole('button', { name: /openai \/ GPT-4\.1/i }))
    fireEvent.click(screen.getByText('GPT-4o'))

    expect(mockGatewayAPI.setSessionModel).not.toHaveBeenCalled()
    expect(mockGatewayAPI.selectProviderModel).not.toHaveBeenCalled()

    useChatStore.setState({ isGenerating: false } as any)
    view.rerender(<ModelSelector />)

    await waitFor(() => {
      expect(mockGatewayAPI.selectProviderModel).toHaveBeenCalledTimes(1)
    })
    expect(mockGatewayAPI.selectProviderModel).toHaveBeenCalledWith({ provider_id: 'openai', model_id: 'gpt-4o' })
    expect(useGatewayStore.getState().providerChangeTick).toBe(1)
  })

  it('updates the global default selection when there is no current session', async () => {
    mockGatewayAPI = {
      listModels: vi.fn().mockResolvedValue({
        payload: {
          models: [{ id: 'gemini-2.5-pro', name: 'Gemini 2.5 Pro', provider: 'gemini' }],
          selected_provider_id: 'gemini',
          selected_model_id: 'gemini-2.5-pro',
        },
      }),
      setSessionModel: vi.fn(),
      selectProviderModel: vi.fn().mockResolvedValue(undefined),
    }

    render(<ModelSelector />)

    await screen.findByText('gemini / Gemini 2.5 Pro')
    fireEvent.click(screen.getByRole('button', { name: /gemini \/ Gemini 2\.5 Pro/i }))
    fireEvent.click(screen.getByText('Gemini 2.5 Pro'))

    await waitFor(() => {
      expect(mockGatewayAPI.selectProviderModel).toHaveBeenCalledTimes(1)
    })
    expect(mockGatewayAPI.selectProviderModel).toHaveBeenCalledWith({
      provider_id: 'gemini',
      model_id: 'gemini-2.5-pro',
    })
    expect(mockGatewayAPI.setSessionModel).not.toHaveBeenCalled()
    expect(useGatewayStore.getState().providerChangeTick).toBe(1)
  })

  it('rolls back the optimistic selection when applying a session model change fails', async () => {
    const showToast = vi.fn()
    mockGatewayAPI = {
      listModels: vi.fn().mockResolvedValue({
        payload: {
          models: [
            { id: 'gpt-4.1', name: 'GPT-4.1', provider: 'openai' },
            { id: 'gpt-4o', name: 'GPT-4o', provider: 'openai' },
          ],
          selected_provider_id: 'openai',
          selected_model_id: 'gpt-4.1',
        },
      }),
      setSessionModel: vi.fn(),
      selectProviderModel: vi.fn().mockRejectedValue(new Error('boom')),
    }
    useSessionStore.setState({ currentSessionId: 'session-1' } as any)
    useUIStore.setState({ showToast } as any)

    render(<ModelSelector />)

    await screen.findByText('openai / GPT-4.1')
    fireEvent.click(screen.getByRole('button', { name: /openai \/ GPT-4\.1/i }))
    fireEvent.click(screen.getByText('GPT-4o'))

    await waitFor(() => {
      expect(mockGatewayAPI.selectProviderModel).toHaveBeenCalledWith({ provider_id: 'openai', model_id: 'gpt-4o' })
    })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /openai \/ GPT-4\.1/i })).toBeTruthy()
    })
    expect(mockGatewayAPI.setSessionModel).not.toHaveBeenCalled()
    expect(showToast).toHaveBeenCalledWith('Failed to apply model change', 'error')
  })

  it('re-syncs the confirmed selection when a deferred model change fails', async () => {
    const showToast = vi.fn()
    mockGatewayAPI = {
      listModels: vi.fn()
        .mockResolvedValueOnce({
          payload: {
            models: [
              { id: 'gpt-4.1', name: 'GPT-4.1', provider: 'openai' },
              { id: 'gpt-4o', name: 'GPT-4o', provider: 'openai' },
            ],
            selected_provider_id: 'openai',
            selected_model_id: 'gpt-4.1',
          },
        })
        .mockResolvedValueOnce({
          payload: {
            models: [
              { id: 'gpt-4.1', name: 'GPT-4.1', provider: 'openai' },
              { id: 'gpt-4o', name: 'GPT-4o', provider: 'openai' },
            ],
            selected_provider_id: 'openai',
            selected_model_id: 'gpt-4.1',
          },
        })
        .mockResolvedValue({
          payload: {
            models: [
              { id: 'gpt-4.1', name: 'GPT-4.1', provider: 'openai' },
              { id: 'gpt-4o', name: 'GPT-4o', provider: 'openai' },
            ],
            selected_provider_id: 'openai',
            selected_model_id: 'gpt-4.1',
          },
        }),
      setSessionModel: vi.fn(),
      selectProviderModel: vi.fn().mockRejectedValue(new Error('boom')),
    }
    useSessionStore.setState({ currentSessionId: 'session-1' } as any)
    useChatStore.setState({ isGenerating: true } as any)
    useUIStore.setState({ showToast } as any)

    const view = render(<ModelSelector />)

    await screen.findByText('openai / GPT-4.1')
    fireEvent.click(screen.getByRole('button', { name: /openai \/ GPT-4\.1/i }))
    fireEvent.click(screen.getByText('GPT-4o'))

    expect(screen.getByRole('button', { name: /openai \/ GPT-4o/i })).toBeTruthy()

    await act(async () => {
      useChatStore.setState({ isGenerating: false } as any)
      view.rerender(<ModelSelector />)
    })

    await waitFor(() => {
      expect(mockGatewayAPI.selectProviderModel).toHaveBeenCalledWith({ provider_id: 'openai', model_id: 'gpt-4o' })
    })
    await waitFor(() => {
      expect(mockGatewayAPI.listModels.mock.calls.length).toBeGreaterThanOrEqual(2)
    })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /openai \/ GPT-4\.1/i })).toBeTruthy()
    })
    expect(showToast).toHaveBeenCalledWith('Model change will apply on the next turn', 'info')
    expect(showToast).toHaveBeenCalledWith('Failed to apply model change', 'error')
  })

  it('refreshes the displayed selection when the active session changes, such as after switching workspace', async () => {
    mockGatewayAPI = {
      listModels: vi.fn()
        .mockResolvedValueOnce({
          payload: {
            models: [{ id: 'gpt-4.1', name: 'GPT-4.1', provider: 'openai' }],
            selected_provider_id: 'openai',
            selected_model_id: 'gpt-4.1',
          },
        })
        .mockResolvedValueOnce({
          payload: {
            models: [{ id: 'gemini-2.5-pro', name: 'Gemini 2.5 Pro', provider: 'gemini' }],
            selected_provider_id: 'gemini',
            selected_model_id: 'gemini-2.5-pro',
          },
        }),
      setSessionModel: vi.fn(),
      selectProviderModel: vi.fn(),
    }
    useSessionStore.setState({ currentSessionId: 'session-a' } as any)

    const view = render(<ModelSelector />)

    await screen.findByText('openai / GPT-4.1')

    useSessionStore.setState({ currentSessionId: 'session-b' } as any)
    view.rerender(<ModelSelector />)

    await screen.findByText('gemini / Gemini 2.5 Pro')
    expect(mockGatewayAPI.listModels).toHaveBeenNthCalledWith(1, 'session-a')
    expect(mockGatewayAPI.listModels).toHaveBeenNthCalledWith(2, 'session-b')
  })
})
