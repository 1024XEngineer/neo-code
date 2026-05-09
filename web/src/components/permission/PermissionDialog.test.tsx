import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import PermissionDialog from './PermissionDialog'
import { useChatStore } from '@/stores/useChatStore'

let gatewayAPI: any = null
vi.mock('@/context/RuntimeProvider', () => ({
	useGatewayAPI: () => gatewayAPI,
}))

describe('PermissionDialog', () => {
	beforeEach(() => {
		useChatStore.setState({ permissionRequests: [] } as any)
		gatewayAPI = null
	})

	it('returns null without request or gateway api', () => {
		const { container } = render(<PermissionDialog />)
		expect(container.firstChild).toBeNull()
	})

	it('renders request details and resolves decisions', async () => {
		gatewayAPI = { resolvePermission: vi.fn().mockResolvedValue(undefined) }
		useChatStore.setState({
			permissionRequests: [{
				request_id: 'r1',
				tool_name: 'bash',
				operation: 'run',
				target: '/tmp',
			}],
		} as any)

		render(<PermissionDialog />)
		expect(screen.getByText('权限请求')).toBeInTheDocument()
		fireEvent.click(screen.getByRole('button', { name: /允许一次/i }))
		expect(gatewayAPI.resolvePermission).toHaveBeenCalledWith({
			request_id: 'r1',
			decision: 'allow_once',
		})
	})
})

