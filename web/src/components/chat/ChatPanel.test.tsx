import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
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
      resolveUserQuestion: vi.fn().mockResolvedValue(undefined),
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
      permissionRequests: [],
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

  it('submits single choice ask_user with additional free text only', async () => {
    useChatStore.setState({
      pendingUserQuestion: {
        request_id: 'uq-single',
        title: 'Choose an option',
        description: 'Pick one',
        kind: 'single_choice',
        options: ['A', 'B', 'C'],
        allow_skip: false,
      },
    } as any)

    render(<ChatPanel />)

    fireEvent.change(screen.getByPlaceholderText('否，我有其他想法要告诉Neo-Code'), {
      target: { value: '我有其他方案：先做灰度' },
    })
    fireEvent.click(screen.getByRole('button', { name: /提交/ }))

    await waitFor(() => {
      expect(mockGatewayAPI.resolveUserQuestion).toHaveBeenCalledWith({
        request_id: 'uq-single',
        status: 'answered',
        values: undefined,
        message: '我有其他方案：先做灰度',
      })
    })
  })

  it('submits multi choice ask_user with selected values and additional free text', async () => {
    useChatStore.setState({
      pendingUserQuestion: {
        request_id: 'uq-multi',
        title: 'Choose options',
        description: 'Pick one or more',
        kind: 'multi_choice',
        options: ['A', 'B', 'C'],
        allow_skip: false,
      },
    } as any)

    render(<ChatPanel />)

    fireEvent.click(screen.getByRole('checkbox', { name: 'A' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'C' }))
    fireEvent.change(screen.getByPlaceholderText('否，我有其他想法要告诉Neo-Code'), {
      target: { value: 'C 放到后面，我建议先做 A' },
    })
    fireEvent.click(screen.getByRole('button', { name: /提交/ }))

    await waitFor(() => {
      expect(mockGatewayAPI.resolveUserQuestion).toHaveBeenCalledWith({
        request_id: 'uq-multi',
        status: 'answered',
        values: ['A', 'C'],
        message: 'C 放到后面，我建议先做 A',
      })
    })
  })

  it('renders option description tooltip icon for ask_user options', () => {
    useChatStore.setState({
      pendingUserQuestion: {
        request_id: 'uq-desc',
        title: 'Choose one',
        description: 'Pick one option',
        kind: 'single_choice',
        options: [
          { label: '选项 A', description: '先执行方案 A' },
          { label: '选项 B', description: '再执行方案 B' },
        ],
        allow_skip: false,
      },
    } as any)

    render(<ChatPanel />)

    expect(screen.getByLabelText('选项说明：先执行方案 A')).toBeInTheDocument()
    expect(screen.getByLabelText('选项说明：再执行方案 B')).toBeInTheDocument()
  })
})
