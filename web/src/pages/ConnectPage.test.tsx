import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import ConnectPage from './ConnectPage'

const runtime = {
	connectBrowser: vi.fn().mockResolvedValue(undefined),
	startLocalGateway: vi.fn().mockResolvedValue(undefined),
	retry: vi.fn().mockResolvedValue(undefined),
	error: '',
	status: 'needs_config',
	vitePluginAvailable: false,
	defaultBrowserGatewayBaseURL: 'http://127.0.0.1:8080',
}

vi.mock('@/context/RuntimeProvider', () => ({
	useRuntime: () => runtime,
}))

describe('ConnectPage', () => {
	beforeEach(() => {
		runtime.connectBrowser.mockClear()
		runtime.startLocalGateway.mockClear()
		runtime.retry.mockClear()
		runtime.error = ''
		runtime.status = 'needs_config'
		runtime.vitePluginAvailable = false
	})

	it('submits manual connect form', async () => {
		render(<ConnectPage />)
		fireEvent.change(screen.getByLabelText('Gateway 地址'), { target: { value: 'http://localhost:9000' } })
		fireEvent.change(screen.getByLabelText('Token（本地模式可留空）'), { target: { value: 'tok' } })
		fireEvent.click(screen.getByRole('button', { name: '连接' }))

		await waitFor(() => {
			expect(runtime.connectBrowser).toHaveBeenCalledWith({
				gatewayBaseURL: 'http://localhost:9000',
				token: 'tok',
			})
		})
	})

	it('validates local gateway port before start', async () => {
		runtime.vitePluginAvailable = true
		render(<ConnectPage />)
		const portInput = screen.getByLabelText('端口')
		fireEvent.change(portInput, { target: { value: '99999' } })
		fireEvent.submit(portInput.closest('form') as HTMLFormElement)
		expect(await screen.findByText('Please enter a valid port (1-65535)')).toBeInTheDocument()
		expect(runtime.startLocalGateway).not.toHaveBeenCalled()
	})

	it('shows retry button when error and triggers retry', () => {
		runtime.status = 'error'
		runtime.error = 'connect failed'
		render(<ConnectPage />)
		fireEvent.click(screen.getByRole('button', { name: '重试' }))
		expect(runtime.retry).toHaveBeenCalled()
		expect(screen.getByText('connect failed')).toBeInTheDocument()
	})
})
