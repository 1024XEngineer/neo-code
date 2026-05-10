import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import CheckpointInlineMark from './CheckpointInlineMark'
import { useSessionStore } from '@/stores/useSessionStore'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'

let gatewayAPI: any = null
vi.mock('@/context/RuntimeProvider', () => ({
	useGatewayAPI: () => gatewayAPI,
}))

describe('CheckpointInlineMark', () => {
	beforeEach(() => {
		gatewayAPI = {
			restoreCheckpoint: vi.fn().mockResolvedValue({ payload: {} }),
			undoRestore: vi.fn().mockResolvedValue({ payload: {} }),
			checkpointDiff: vi.fn().mockResolvedValue({ payload: { files: { added: [], modified: [], deleted: [] }, patch: '' } }),
		}
		useSessionStore.setState({ currentSessionId: 's1' } as any)
		useRuntimeInsightStore.getState().reset()
		vi.spyOn(window, 'confirm').mockReturnValue(true)
	})

	it('restores checkpoint from available state', async () => {
		render(<CheckpointInlineMark checkpointId="abcdef123456" status="available" />)
		fireEvent.click(screen.getByRole('button', { name: /cp_abcdef/i }))
		await waitFor(() => expect(gatewayAPI.restoreCheckpoint).toHaveBeenCalledWith({
			session_id: 's1',
			checkpoint_id: 'abcdef123456',
		}))
	})

	it('renders restored state and can undo restore', async () => {
		render(<CheckpointInlineMark checkpointId="abcdef123456" status="restored" />)
		fireEvent.click(screen.getByRole('button', { name: /已撤回/ }))
		await waitFor(() => expect(gatewayAPI.undoRestore).toHaveBeenCalledWith('s1'))
	})
})

