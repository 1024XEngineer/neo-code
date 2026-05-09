import { describe, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import VerificationMessage from './VerificationMessage'

describe('VerificationMessage', () => {
	it('renders running summary and stage details', () => {
		render(
			<VerificationMessage
				message={{
					id: 'v1',
					role: 'assistant',
					type: 'verification',
					content: '',
					timestamp: 1,
					verificationData: {
						id: 'run1',
						startedAt: 1,
						started: { completion_passed: true },
						stages: {
							test: { name: 'test', status: 'pass', summary: 'ok' },
						},
						status: 'running',
					},
				} as any}
			/>,
		)
		expect(screen.getByText(/Verify running/)).toBeInTheDocument()
		fireEvent.click(screen.getByRole('button'))
		expect(screen.getByText('test')).toBeInTheDocument()
	})
})

