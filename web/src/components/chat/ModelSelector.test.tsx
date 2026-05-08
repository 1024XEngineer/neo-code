import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
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

  it('defers a session model change until generation completes and applies it once', async () => {
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
      setSessionModel: vi.fn().mockResolvedValue(undefined),
      selectProviderModel: vi.fn(),
    }
    useSessionStore.setState({ currentSessionId: 'session-1' } as any)
    useChatStore.setState({ isGenerating: true } as any)

    const view = render(<ModelSelector />)

    await screen.findByText('openai / GPT-4.1')
    fireEvent.click(screen.getByRole('button', { name: /openai \/ GPT-4\.1/i }))
    fireEvent.click(screen.getByText('GPT-4o'))

    expect(mockGatewayAPI.setSessionModel).not.toHaveBeenCalled()

    useChatStore.setState({ isGenerating: false } as any)
    view.rerender(<ModelSelector />)

    await waitFor(() => {
      expect(mockGatewayAPI.setSessionModel).toHaveBeenCalledTimes(1)
    })
    expect(mockGatewayAPI.setSessionModel).toHaveBeenCalledWith('session-1', 'gpt-4o', 'openai')
    expect(mockGatewayAPI.selectProviderModel).not.toHaveBeenCalled()
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
})
