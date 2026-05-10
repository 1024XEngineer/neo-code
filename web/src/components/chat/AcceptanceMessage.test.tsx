import { describe, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import AcceptanceMessage from './AcceptanceMessage'

describe('AcceptanceMessage', () => {
	it('renders accepted summary and expandable details', () => {
		render(
			<AcceptanceMessage
				message={{
					id: 'a1',
					role: 'assistant',
					type: 'acceptance',
					content: '',
					timestamp: 1,
					acceptanceData: {
						status: 'accepted',
						stop_reason: 'accepted',
						user_visible_summary: 'all good',
						internal_summary: 'detail',
						completion_blocked_reason: 'none',
					},
				} as any}
			/>,
		)
		expect(screen.getByText('已接受')).toBeInTheDocument()
		expect(screen.getByText('all good')).toBeInTheDocument()
		fireEvent.click(screen.getByRole('button', { name: '展开详情' }))
		expect(screen.getByText('detail')).toBeInTheDocument()
	})
})

