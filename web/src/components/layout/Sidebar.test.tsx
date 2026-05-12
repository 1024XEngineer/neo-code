import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import Sidebar from './Sidebar'
import { useChatStore } from '@/stores/useChatStore'
import { useGatewayStore } from '@/stores/useGatewayStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useUIStore } from '@/stores/useUIStore'
import { useWorkspaceStore } from '@/stores/useWorkspaceStore'

let mockGatewayAPI: any = null
const appCss = readFileSync('src/index.css', 'utf-8')

vi.mock('@/context/RuntimeProvider', () => ({
  useGatewayAPI: () => mockGatewayAPI,
  useRuntime: () => ({
    mode: 'browser',
    selectWorkdir: vi.fn(),
  }),
}))

describe('Sidebar ProviderModal', () => {
  beforeEach(() => {
    cleanup()
    mockGatewayAPI = {
      listProviders: vi.fn().mockResolvedValue({
        payload: {
          providers: [
            {
              id: 'gemini',
              name: 'Gemini',
              source: 'builtin',
              selected: false,
              models: [{ id: 'gemini-2.5-pro', name: 'Gemini 2.5 Pro' }],
            },
            {
              id: 'openai',
              name: 'OpenAI',
              source: 'builtin',
              selected: true,
              models: [{ id: 'gpt-5.4', name: 'GPT-5.4' }],
            },
          ],
        },
      }),
      selectProviderModel: vi.fn().mockResolvedValue({
        payload: {
          provider_id: 'gemini',
          model_id: 'gemini-2.5-pro',
        },
      }),
      getSessionModel: vi.fn().mockResolvedValue({
        payload: {
          provider_id: 'openai',
          model_id: 'gpt-5.4',
          model_name: 'GPT-5.4',
          provider: 'openai',
        },
      }),
      setSessionModel: vi.fn().mockResolvedValue(undefined),
    }

    useGatewayStore.getState().reset()
    useUIStore.setState({
      sidebarOpen: true,
      searchQuery: '',
      theme: 'dark',
      toggleSidebar: vi.fn(),
      setSearchQuery: vi.fn(),
      setTheme: vi.fn(),
      showToast: vi.fn(),
    } as any)
    useChatStore.setState({
      isGenerating: false,
    } as any)
    useSessionStore.setState({
      projects: [{
        id: 'group_today',
        name: 'Today',
        sessions: [
          { id: 'session-1', title: 'Session 1', time: '2026-05-08T12:00:00Z' },
          { id: 'session-2', title: 'Session 2', time: '2026-05-08T12:01:00Z' },
        ],
      }],
      currentSessionId: 'session-1',
      currentProjectId: '',
      loading: false,
      _switchAbort: null,
      _initialBindDone: false,
      switchSession: vi.fn(),
      setCurrentProjectId: vi.fn(),
    } as any)
    useWorkspaceStore.setState({
      workspaces: [],
      currentWorkspaceHash: '',
      switchWorkspace: vi.fn(),
      renameWorkspace: vi.fn(),
      deleteWorkspace: vi.fn(),
      createWorkspace: vi.fn(),
    } as any)
  })

  async function openProviderModal() {
    render(<Sidebar />)
    fireEvent.click(screen.getByRole('button', { name: /供应商/i }))
    await screen.findByText('Gemini')
  }

  function providerCard(name: string): HTMLElement {
    const card = screen.getByText(name).closest('.config-card')
    if (!(card instanceof HTMLElement)) {
      throw new Error(`${name} card not found`)
    }
    return card
  }

  it('switches the global provider through a single backend call', async () => {
    await openProviderModal()

    const geminiCard = providerCard('Gemini')
    fireEvent.click(within(geminiCard).getByRole('button', { name: /选择/i }))

    await waitFor(() => {
      expect(mockGatewayAPI.selectProviderModel).toHaveBeenCalledWith({ provider_id: 'gemini' })
    })
    expect(mockGatewayAPI.setSessionModel).not.toHaveBeenCalled()
    expect(useGatewayStore.getState().providerChangeTick).toBe(1)
  })

  it('marks the provider selected according to the active session model, not the global snapshot alone', async () => {
    mockGatewayAPI.listProviders = vi.fn().mockResolvedValue({
      payload: {
        providers: [
          {
            id: 'deepseek',
            name: 'deepseek',
            source: 'builtin',
            selected: true,
            models: [{ id: 'deepseek-v4-pro', name: 'DeepSeek V4 Pro' }],
          },
          {
            id: 'mimo',
            name: 'mimo',
            source: 'builtin',
            selected: false,
            models: [{ id: 'mimo-v2.5-pro', name: 'MiMo V2.5 Pro' }],
          },
        ],
      },
    })
    mockGatewayAPI.getSessionModel = vi.fn().mockResolvedValue({
      payload: {
        provider_id: 'mimo',
        model_id: 'mimo-v2.5-pro',
        model_name: 'MiMo V2.5 Pro',
        provider: 'mimo',
      },
    })

    render(<Sidebar />)
    fireEvent.click(screen.getByRole('button', { name: /供应商/i }))
    await screen.findByText('deepseek')

    expect(within(providerCard('mimo')).getByRole('button', { name: /当前使用/i })).toBeTruthy()
    expect(within(providerCard('deepseek')).getByRole('button', { name: /选择/i })).toBeTruthy()
  })

  it('still works when there are no loaded sessions', async () => {
    useSessionStore.setState({ currentSessionId: '', projects: [] } as any)

    await openProviderModal()

    const geminiCard = providerCard('Gemini')
    fireEvent.click(within(geminiCard).getByRole('button', { name: /选择/i }))

    await waitFor(() => {
      expect(mockGatewayAPI.selectProviderModel).toHaveBeenCalledWith({ provider_id: 'gemini' })
    })
    expect(mockGatewayAPI.getSessionModel).not.toHaveBeenCalled()
    expect(mockGatewayAPI.setSessionModel).not.toHaveBeenCalled()
    expect(useGatewayStore.getState().providerChangeTick).toBe(1)
  })

  it('shows the backend error without synthesizing partial sync messages', async () => {
    const showToast = vi.fn()
    mockGatewayAPI.selectProviderModel = vi.fn().mockRejectedValue(new Error('switch failed'))
    useUIStore.setState({ showToast } as any)

    await openProviderModal()

    const geminiCard = providerCard('Gemini')
    fireEvent.click(within(geminiCard).getByRole('button', { name: /选择/i }))

    await waitFor(() => {
      expect(mockGatewayAPI.selectProviderModel).toHaveBeenCalledWith({ provider_id: 'gemini' })
    })
    expect(mockGatewayAPI.setSessionModel).not.toHaveBeenCalled()
    expect(mockGatewayAPI.listProviders).toHaveBeenCalledTimes(1)
    expect(useGatewayStore.getState().providerChangeTick).toBe(0)
    expect(showToast).not.toHaveBeenCalled()
    expect(screen.getByText('switch failed')).toBeInTheDocument()
  })

  it('keeps provider models in a single scrollable row when there are many models', async () => {
    mockGatewayAPI.listProviders = vi.fn().mockResolvedValue({
      payload: {
        providers: [
          {
            id: 'ark',
            name: 'Ark',
            source: 'custom',
            selected: false,
            models: Array.from({ length: 16 }, (_, index) => ({
              id: `ark-code-${index + 1}`,
              name: `ark-code-${index + 1}`,
            })),
          },
        ],
      },
    })
    mockGatewayAPI.getSessionModel = vi.fn().mockResolvedValue({
      payload: {
        provider_id: 'openai',
        model_id: 'gpt-5.4',
        model_name: 'GPT-5.4',
        provider: 'openai',
      },
    })

    render(<Sidebar />)
    fireEvent.click(screen.getByRole('button', { name: /供应商/i }))
    await screen.findByText('Ark')

    const arkCard = providerCard('Ark')
    const models = arkCard.querySelector('.config-card-models')
    expect(models).toBeInstanceOf(HTMLElement)
    const modelRule = appCss.match(/\.config-card-models\s*{(?<body>[^}]*)}/)?.groups?.body ?? ''
    expect(modelRule).toContain('flex-wrap: nowrap')
    expect(modelRule).toContain('overflow-x: auto')
    expect(modelRule).toContain('overflow-y: hidden')
    expect(models?.querySelectorAll('.config-card-model-tag')).toHaveLength(16)
    expect(within(arkCard).getByRole('button', { name: /选择/i })).toBeTruthy()
    expect(within(arkCard).getByRole('button', { name: /删除/i })).toBeTruthy()
  })

  it('only shows the expanded workspace style on the current workspace', async () => {
    const switchWorkspace = vi.fn().mockResolvedValue(undefined)
    useWorkspaceStore.setState({
      workspaces: [
        { hash: 'w1', path: '/workspace-one', name: 'Workspace One', createdAt: '1', updatedAt: '1' },
        { hash: 'w2', path: '/workspace-two', name: 'Workspace Two', createdAt: '1', updatedAt: '1' },
      ],
      currentWorkspaceHash: 'w1',
      switchWorkspace,
    } as any)

    render(<Sidebar />)

    const workspaceOne = screen.getByRole('button', { name: /Workspace One/i })
    const workspaceTwo = screen.getByRole('button', { name: /Workspace Two/i })
    const chevronFor = (button: HTMLElement) => {
      const chevron = button.querySelector('.chevron')
      if (!(chevron instanceof HTMLElement)) {
        throw new Error('workspace chevron not found')
      }
      return chevron
    }

    await waitFor(() => {
      expect(chevronFor(workspaceOne)).toHaveClass('expanded')
    })

    fireEvent.click(workspaceOne)
    await waitFor(() => {
      expect(chevronFor(workspaceOne)).not.toHaveClass('expanded')
    })
    fireEvent.click(workspaceOne)
    await waitFor(() => {
      expect(chevronFor(workspaceOne)).toHaveClass('expanded')
    })

    fireEvent.click(workspaceTwo)
    await waitFor(() => {
      expect(switchWorkspace).toHaveBeenCalledWith('w2', mockGatewayAPI)
    })
    useWorkspaceStore.setState({ currentWorkspaceHash: 'w2' } as any)

    await waitFor(() => {
      expect(chevronFor(workspaceOne)).not.toHaveClass('expanded')
      expect(chevronFor(workspaceTwo)).toHaveClass('expanded')
    })
  })
})
