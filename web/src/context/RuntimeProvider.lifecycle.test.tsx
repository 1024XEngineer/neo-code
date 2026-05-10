import { act, render, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { RuntimeProvider, useRuntime } from './RuntimeProvider'
import { useChatStore } from '@/stores/useChatStore'
import { useSessionStore } from '@/stores/useSessionStore'
import { useWorkspaceStore } from '@/stores/useWorkspaceStore'

const clients: any[] = []

vi.mock('@/api/wsClient', () => ({
	createWSClient: vi.fn(() => {
		let onState: ((s: any) => void) | null = null
		let onEvent: ((f: any) => void) | null = null
		let onReconnect: (() => void) | null = null
		const client = {
			connect: vi.fn(() => onState?.('connected')),
			disconnect: vi.fn(() => onState?.('disconnected')),
			reconnect: vi.fn(),
			call: vi.fn(async (method: string) => {
				if (method === 'gateway.authenticate') return { payload: {} }
				if (method === 'gateway.listWorkspaces') return { payload: { workspaces: [] } }
				if (method === 'gateway.ping') return { payload: {} }
				return { payload: {} }
			}),
			onEvent: vi.fn((h: any) => {
				onEvent = h
				return () => {
					if (onEvent === h) onEvent = null
				}
			}),
			onStateChange: vi.fn((h: any) => {
				onState = h
				return () => {
					if (onState === h) onState = null
				}
			}),
			onReconnect: vi.fn((h: any) => {
				onReconnect = h
				return () => {
					if (onReconnect === h) onReconnect = null
				}
			}),
			_emitState: (s: any) => onState?.(s),
		}
		clients.push(client)
		return client
	}),
}))

function RuntimeProbe({ onReady }: { onReady: (value: ReturnType<typeof useRuntime>) => void }) {
	const runtime = useRuntime()
	onReady(runtime)
	return null
}

describe('RuntimeProvider lifecycle', () => {
	beforeEach(() => {
		clients.length = 0
		sessionStorage.clear()
		Object.defineProperty(window.navigator, 'userAgent', {
			value: 'Mozilla/5.0 Chrome/120 Safari/537.36',
			configurable: true,
		})
		Object.defineProperty(window, 'electronAPI', {
			value: undefined,
			configurable: true,
			writable: true,
		})

		useSessionStore.setState({
			fetchSessions: vi.fn().mockResolvedValue(undefined),
			initializeActiveSession: vi.fn().mockResolvedValue(undefined),
			setProjects: vi.fn(),
			setCurrentSessionId: vi.fn(),
			setCurrentProjectId: vi.fn(),
			currentSessionId: '',
		} as any)
		useWorkspaceStore.setState({
			fetchWorkspaces: vi.fn().mockResolvedValue(undefined),
			setWorkspaces: vi.fn(),
			setCurrentWorkspaceHash: vi.fn(),
			workspaces: [],
		} as any)
		useChatStore.setState({
			clearMessages: vi.fn(),
			clearPendingUserQuestion: vi.fn(),
			resetGeneratingState: vi.fn(),
		} as any)
	})

	it('connects from stored browser config and exposes connected runtime', async () => {
		sessionStorage.setItem(
			'neocode.browserRuntimeConfig',
			JSON.stringify({ mode: 'browser', gatewayBaseURL: 'http://127.0.0.1:8080', token: 'tok' }),
		)
		let runtimeSnapshot: any = null
		render(
			<RuntimeProvider>
				<RuntimeProbe onReady={(rt) => { runtimeSnapshot = rt }} />
			</RuntimeProvider>,
		)

		await waitFor(() => {
			expect(runtimeSnapshot?.status).toBe('connected')
			expect(runtimeSnapshot?.gatewayAPI).toBeTruthy()
		})
		expect(clients).toHaveLength(1)
		expect(clients[0].connect).toHaveBeenCalled()
	})

	it('retry reconnects with existing config', async () => {
		sessionStorage.setItem(
			'neocode.browserRuntimeConfig',
			JSON.stringify({ mode: 'browser', gatewayBaseURL: 'http://127.0.0.1:8080', token: 'tok' }),
		)
		let runtimeSnapshot: any = null
		render(
			<RuntimeProvider>
				<RuntimeProbe onReady={(rt) => { runtimeSnapshot = rt }} />
			</RuntimeProvider>,
		)
		await waitFor(() => expect(runtimeSnapshot?.status).toBe('connected'))

		await act(async () => {
			await runtimeSnapshot.retry()
		})
		expect(clients.length).toBeGreaterThanOrEqual(2)
	})

	it('resetBrowserConfig clears store-facing runtime state', async () => {
		sessionStorage.setItem(
			'neocode.browserRuntimeConfig',
			JSON.stringify({ mode: 'browser', gatewayBaseURL: 'http://127.0.0.1:8080', token: 'tok' }),
		)
		let runtimeSnapshot: any = null
		const chatClear = useChatStore.getState().clearMessages as any
		render(
			<RuntimeProvider>
				<RuntimeProbe onReady={(rt) => { runtimeSnapshot = rt }} />
			</RuntimeProvider>,
		)
		await waitFor(() => expect(runtimeSnapshot?.status).toBe('connected'))

		act(() => {
			runtimeSnapshot.resetBrowserConfig()
		})

		expect(sessionStorage.getItem('neocode.browserRuntimeConfig')).toBeNull()
		expect(chatClear).toHaveBeenCalled()
		expect(runtimeSnapshot.status).toBe('needs_config')
	})
})

