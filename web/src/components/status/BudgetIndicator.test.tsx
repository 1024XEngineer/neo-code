import { beforeEach, describe, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import BudgetIndicator from './BudgetIndicator'
import { useRuntimeInsightStore } from '@/stores/useRuntimeInsightStore'

describe('BudgetIndicator', () => {
	beforeEach(() => {
		useRuntimeInsightStore.getState().reset()
	})

	it('renders null when no budget data', () => {
		const { container } = render(<BudgetIndicator />)
		expect(container.firstChild).toBeNull()
	})

	it('shows budget and popover details', () => {
		useRuntimeInsightStore.setState({
			budgetChecked: {
				attempt_seq: 1,
				request_hash: 'h',
				action: 'allow',
				estimated_input_tokens: 100,
				prompt_budget: 200,
				context_window: 1000,
			},
			budgetUsageRatio: 0.5,
		} as any)
		render(<BudgetIndicator />)
		fireEvent.click(screen.getByTitle('点击查看预算明细'))
		expect(screen.getByText('预算明细')).toBeInTheDocument()
		expect(screen.getByText('allow')).toBeInTheDocument()
	})

	it('shows estimate failed message', () => {
		useRuntimeInsightStore.setState({
			budgetEstimateFailed: { attempt_seq: 1, request_hash: 'h', message: 'estimate failed' },
		} as any)
		render(<BudgetIndicator />)
		fireEvent.click(screen.getByTitle('点击查看预算明细'))
		expect(screen.getByText('estimate failed')).toBeInTheDocument()
	})
})

