import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import TodoStrip from './TodoStrip'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'
import { useUIStore } from '@/stores/useUIStore'

describe('TodoStrip', () => {
	beforeEach(() => {
		useRuntimeInsightStore.getState().reset()
		useUIStore.setState({
			todoStripExpanded: false,
			setTodoStripExpanded: vi.fn(),
		} as any)
	})

	it('renders nothing when no snapshot and no conflict', () => {
		const { container } = render(<TodoStrip />)
		expect(container.firstChild).toBeNull()
	})

	it('renders summary and items from snapshot', () => {
		useRuntimeInsightStore.setState({
			todoSnapshot: {
				items: [
					{ id: '1', content: 'Task 1', status: 'in_progress', required: true, revision: 1 },
					{ id: '2', content: 'Task 2', status: 'completed', required: true, revision: 1 },
				],
				summary: { total: 2, required_total: 2, required_completed: 1, required_failed: 0, required_open: 1 },
			},
			todoHistory: {
				'1': { id: '1', content: 'Task 1', status: 'in_progress', required: true, revision: 1, firstSeenAt: 1, lastSeenAt: 1 },
				'2': { id: '2', content: 'Task 2', status: 'completed', required: true, revision: 1, firstSeenAt: 1, lastSeenAt: 1 },
			},
		} as any)
		render(<TodoStrip />)
		expect(screen.getByText('Task 1')).toBeInTheDocument()
		fireEvent.click(screen.getByRole('button', { expanded: false }))
		expect(useUIStore.getState().setTodoStripExpanded).toHaveBeenCalled()
	})

	it('forces expanded conflict state', () => {
		useRuntimeInsightStore.setState({
			todoConflict: { action: 'conflict', reason: 'manual check needed' },
		} as any)
		render(<TodoStrip />)
		expect(screen.getByText(/Todo 冲突/)).toBeInTheDocument()
	})
})

