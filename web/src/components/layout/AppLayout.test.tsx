import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import AppLayout from './AppLayout'
import { useUIStore } from '@/stores/useUIStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useWorkspaceStore } from '@/stores/useWorkspaceStore'

vi.mock('./Sidebar', () => ({ default: ({ collapsed }: { collapsed?: boolean }) => <div>{collapsed ? 'sidebar-collapsed' : 'sidebar-open'}</div> }))
vi.mock('@/components/chat/ChatPanel', () => ({ default: () => <div>chat-panel</div> }))
vi.mock('@/components/panels/FileChangePanel', () => ({ default: () => <div>changes-panel</div> }))
vi.mock('@/components/panels/FileTreePanel', () => ({ default: () => <div>tree-panel</div> }))
vi.mock('@/components/status/StatusBar', () => ({ default: () => <div>status-bar</div> }))
vi.mock('@/components/ui/ToastContainer', () => ({ default: () => <div>toast-container</div> }))

describe('AppLayout', () => {
	beforeEach(() => {
		useUIStore.setState({
			sidebarOpen: true,
			sidebarWidth: 280,
			setSidebarWidth: vi.fn(),
			changesPanelOpen: false,
			changesPanelWidth: 360,
			setChangesPanelWidth: vi.fn(),
			fileTreePanelOpen: false,
			fileTreePanelWidth: 320,
			setFileTreePanelWidth: vi.fn(),
		} as any)
		useSessionStore.setState({
			prepareNewChat: vi.fn(),
		} as any)
		useWorkspaceStore.setState({ currentWorkspaceHash: '' } as any)
	})

	it('renders main layout with sidebar open', () => {
		render(<AppLayout />)
		expect(screen.getByText('sidebar-open')).toBeInTheDocument()
		expect(screen.getByText('chat-panel')).toBeInTheDocument()
	})

	it('renders collapsed sidebar and right panels when toggled', () => {
		useUIStore.setState({
			sidebarOpen: false,
			changesPanelOpen: true,
			fileTreePanelOpen: true,
		} as any)
		render(<AppLayout />)
		expect(screen.getByText('sidebar-collapsed')).toBeInTheDocument()
		expect(screen.getByText('changes-panel')).toBeInTheDocument()
		expect(screen.getByText('tree-panel')).toBeInTheDocument()
	})

	it('handles ctrl/cmd+n shortcut', () => {
		const prepareNewChat = vi.fn()
		useSessionStore.setState({ prepareNewChat } as any)
		render(<AppLayout />)
		fireEvent.keyDown(window, { key: 'n', ctrlKey: true })
		expect(prepareNewChat).toHaveBeenCalled()
	})
})

