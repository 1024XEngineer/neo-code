import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useWorkspaceStore } from './useWorkspaceStore'
import { useChatStore } from './useChatStore'
import { useSessionStore } from './useSessionStore'
import { useUIStore } from './useUIStore'
import { useGatewayStore } from './useGatewayStore'

function flushPromises() {
	return new Promise((resolve) => setTimeout(resolve, 0))
}

describe('useWorkspaceStore', () => {
	beforeEach(() => {
		useWorkspaceStore.setState({
			workspaces: [],
			currentWorkspaceHash: '',
			loading: false,
		} as any)

		useChatStore.setState({
			isGenerating: false,
			clearMessages: vi.fn(),
			setTransitioning: vi.fn(),
		} as any)
		useSessionStore.setState({
			fetchSessions: vi.fn().mockResolvedValue(undefined),
			resetForWorkspaceSwitch: vi.fn(),
			currentSessionId: '',
			currentProjectId: '',
			projects: [],
		} as any)
		useUIStore.setState({
			showToast: vi.fn(),
			clearFileChanges: vi.fn(),
			resetPreviewTabs: vi.fn(),
			setSearchQuery: vi.fn(),
		} as any)
		useGatewayStore.setState({
			setCurrentRunId: vi.fn(),
			notifyProviderChanged: vi.fn(),
		} as any)
	})

	it('deduplicates concurrent fetchWorkspaces calls', async () => {
		let resolveList: ((value: any) => void) | null = null
		const gatewayAPI = {
			listWorkspaces: vi.fn(() => new Promise((resolve) => { resolveList = resolve })),
		} as any

		const p1 = useWorkspaceStore.getState().fetchWorkspaces(gatewayAPI)
		const p2 = useWorkspaceStore.getState().fetchWorkspaces(gatewayAPI)
		expect(gatewayAPI.listWorkspaces).toHaveBeenCalledTimes(1)

		resolveList?.({
			payload: {
				workspaces: [{ hash: 'w1', path: '/a', name: 'A', created_at: '1', updated_at: '2' }],
			},
		})
		await Promise.all([p1, p2])
		expect(useWorkspaceStore.getState().currentWorkspaceHash).toBe('w1')
	})

	it('blocks switchWorkspace while generating', async () => {
		const showToast = vi.fn()
		useChatStore.setState({ isGenerating: true } as any)
		useUIStore.setState({ showToast } as any)
		const gatewayAPI = { switchWorkspace: vi.fn() } as any

		await useWorkspaceStore.getState().switchWorkspace('w2', gatewayAPI)

		expect(gatewayAPI.switchWorkspace).not.toHaveBeenCalled()
		expect(showToast).toHaveBeenCalledWith('Cannot switch workspace while generating; stop the current run first.', 'info')
	})

	it('switchWorkspace clears session/UI state then fetches sessions', async () => {
		const gatewayAPI = { switchWorkspace: vi.fn().mockResolvedValue(undefined) } as any
		const fetchSessions = useSessionStore.getState().fetchSessions as any

		await useWorkspaceStore.getState().switchWorkspace('w2', gatewayAPI)

		expect(useChatStore.getState().clearMessages).toHaveBeenCalled()
		expect(useSessionStore.getState().resetForWorkspaceSwitch).toHaveBeenCalled()
		expect(useUIStore.getState().clearFileChanges).toHaveBeenCalled()
		expect(useUIStore.getState().resetPreviewTabs).toHaveBeenCalled()
		expect(gatewayAPI.switchWorkspace).toHaveBeenCalledWith('w2')
		expect(useGatewayStore.getState().notifyProviderChanged).toHaveBeenCalled()
		expect(fetchSessions).toHaveBeenCalledWith(gatewayAPI, true)
		expect(useWorkspaceStore.getState().currentWorkspaceHash).toBe('w2')
	})

	it('createWorkspace failure reports toast', async () => {
		const showToast = vi.fn()
		useUIStore.setState({ showToast } as any)
		const gatewayAPI = {
			createWorkspace: vi.fn().mockRejectedValue(new Error('boom')),
		} as any

		await useWorkspaceStore.getState().createWorkspace('/x', gatewayAPI)
		expect(showToast).toHaveBeenCalledWith('Failed to create workspace', 'error')
	})

	it('deleteWorkspace switches to remaining first workspace when current is removed', async () => {
		const switchWorkspace = vi.spyOn(useWorkspaceStore.getState(), 'switchWorkspace')
		useWorkspaceStore.setState({
			workspaces: [
				{ hash: 'w1', path: '/1', name: '1', createdAt: '1', updatedAt: '1' },
				{ hash: 'w2', path: '/2', name: '2', createdAt: '1', updatedAt: '1' },
			],
			currentWorkspaceHash: 'w1',
		} as any)
		const gatewayAPI = { deleteWorkspace: vi.fn().mockResolvedValue(undefined) } as any

		await useWorkspaceStore.getState().deleteWorkspace('w1', gatewayAPI)
		await flushPromises()
		expect(gatewayAPI.deleteWorkspace).toHaveBeenCalledWith('w1')
		expect(useWorkspaceStore.getState().workspaces.map((w) => w.hash)).toEqual(['w2'])
		expect(switchWorkspace).toHaveBeenCalledWith('w2', gatewayAPI)
	})
})
