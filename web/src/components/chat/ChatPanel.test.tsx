import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen, waitFor } from '@testing-library/react'
import ChatPanel from './ChatPanel'
import { useChatStore } from '@/stores/useChatStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useUIStore } from '@/stores/useUIStore'

let mockGatewayAPI: any = null

vi.mock('@/context/RuntimeProvider', () => ({
  useGatewayAPI: () => mockGatewayAPI,
}))

vi.mock('./MessageList', () => ({
  default: () => <div data-testid="message-list" />,
}))

vi.mock('./ChatInput', () => ({
  default: () => <div data-testid="chat-input" />,
}))

vi.mock('./ModelSelector', () => ({
  default: () => <div data-testid="model-selector" />,
}))

vi.mock('./TodoStrip', () => ({
  default: () => <div data-testid="todo-strip" />,
}))

describe('ChatPanel', () => {
  beforeEach(() => {
    mockGatewayAPI = {
      resolvePermission: vi.fn().mockResolvedValue(undefined),
    }

    useUIStore.setState({
      sidebarOpen: true,
      changesPanelOpen: false,
      fileTreePanelOpen: false,
      toggleSidebar: vi.fn(),
      toggleChangesPanel: vi.fn(),
      toggleFileTreePanel: vi.fn(),
      showToast: vi.fn(),
    } as any)

    useSessionStore.setState({
      currentSessionId: 'session-1',
      currentProjectId: '',
      projects: [],
      loading: false,
      _switchAbort: null,
      _initialBindDone: false,
    } as any)

    useChatStore.setState({
      messages: [],
      isGenerating: false,
      isCompacting: false,
      compactMode: '',
      compactMessage: '',
      permissionRequests: [],
      pendingUserQuestion: null,
      agentMode: 'build',
      permissionMode: 'default',
    } as any)
  })

  it('does not auto-resolve permission requests in default mode', async () => {
    useChatStore.setState({
      permissionRequests: [{
        request_id: 'req-default',
        tool_call_id: 'tool-1',
        tool_name: 'filesystem_edit',
        tool_category: 'filesystem',
        action_type: 'write',
        operation: 'edit',
        target_type: 'file',
        target: 'foo.txt',
        decision: '',
        reason: 'needs approval',
      }],
    } as any)

    render(<ChatPanel />)

    expect(screen.getByText('权限请求')).toBeInTheDocument()
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(mockGatewayAPI.resolvePermission).not.toHaveBeenCalled()
  })

  it('auto-resolves permission requests once in build bypass mode', async () => {
    useChatStore.setState({
      permissionMode: 'bypass',
      permissionRequests: [{
        request_id: 'req-bypass',
        tool_call_id: 'tool-2',
        tool_name: 'filesystem_edit',
        tool_category: 'filesystem',
        action_type: 'write',
        operation: 'edit',
        target_type: 'file',
        target: 'bar.txt',
        decision: '',
        reason: 'needs approval',
      }],
    } as any)

    render(<ChatPanel />)

    await waitFor(() => {
      expect(mockGatewayAPI.resolvePermission).toHaveBeenCalledTimes(1)
    })
    expect(mockGatewayAPI.resolvePermission).toHaveBeenCalledWith({
      request_id: 'req-bypass',
      decision: 'allow_once',
    })
  })

  it('does not auto-resolve the same request more than once before it is removed', async () => {
    useChatStore.setState({
      permissionMode: 'bypass',
      permissionRequests: [{
        request_id: 'req-once',
        tool_call_id: 'tool-3',
        tool_name: 'filesystem_edit',
        tool_category: 'filesystem',
        action_type: 'write',
        operation: 'edit',
        target_type: 'file',
        target: 'baz.txt',
        decision: '',
        reason: 'needs approval',
      }],
    } as any)

    render(<ChatPanel />)

    await waitFor(() => {
      expect(mockGatewayAPI.resolvePermission).toHaveBeenCalledTimes(1)
    })

    await act(async () => {
      useChatStore.setState({
        permissionRequests: [{
          request_id: 'req-once',
          tool_call_id: 'tool-3',
          tool_name: 'filesystem_edit',
          tool_category: 'filesystem',
          action_type: 'write',
          operation: 'edit',
          target_type: 'file',
          target: 'baz.txt',
          decision: '',
          reason: 'needs approval',
        }],
      } as any)
    })

    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(mockGatewayAPI.resolvePermission).toHaveBeenCalledTimes(1)
  })

  it('shows compact status panel above the normal chat input', () => {
    useChatStore.setState({
      isCompacting: true,
      compactMode: 'proactive',
      compactMessage: 'Context is near the limit. Auto-compacting...',
    } as any)

    render(<ChatPanel />)

    expect(screen.getByRole('status')).toHaveTextContent('Context is near the limit. Auto-compacting...')
    expect(screen.getByTestId('chat-input')).toBeInTheDocument()
  })

  it('keeps compact status visible while a permission request is shown', () => {
    useChatStore.setState({
      isCompacting: true,
      compactMode: 'reactive',
      compactMessage: 'Model reported context too long. Compacting and retrying...',
      permissionRequests: [{
        request_id: 'req-compact-permission',
        tool_call_id: 'tool-compact',
        tool_name: 'filesystem_edit',
        tool_category: 'filesystem',
        action_type: 'write',
        operation: 'edit',
        target_type: 'file',
        target: 'foo.txt',
        decision: '',
        reason: 'needs approval',
      }],
    } as any)

    render(<ChatPanel />)

    expect(screen.getByRole('status')).toHaveTextContent('Model reported context too long. Compacting and retrying...')
    expect(screen.getByText('权限请求')).toBeInTheDocument()
    expect(screen.queryByTestId('chat-input')).not.toBeInTheDocument()
  })

  it('keeps compact status visible while a user question is shown', () => {
    useChatStore.setState({
      isCompacting: true,
      compactMode: 'manual',
      compactMessage: 'Compacting context...',
      pendingUserQuestion: {
        request_id: 'ask-compact',
        question_id: 'question-compact',
        title: 'Need input',
        description: '',
        kind: 'text',
        required: true,
        allow_skip: false,
      },
    } as any)

    render(<ChatPanel />)

    expect(screen.getByRole('status')).toHaveTextContent('Compacting context...')
    expect(screen.getByText('Need input')).toBeInTheDocument()
    expect(screen.queryByTestId('chat-input')).not.toBeInTheDocument()
  })
})
