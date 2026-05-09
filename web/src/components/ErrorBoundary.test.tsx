import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import type { ReactElement } from 'react'
import { ErrorBoundary } from './ErrorBoundary'

function Crash(): ReactElement {
	throw new Error('boom')
}

describe('ErrorBoundary', () => {
	it('renders fallback UI and supports retry', () => {
		const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
		render(
			<ErrorBoundary>
				<Crash />
			</ErrorBoundary>,
		)
		expect(screen.getByText('The application encountered an error')).toBeInTheDocument()
		expect(screen.getByText('boom')).toBeInTheDocument()
		fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
		errSpy.mockRestore()
	})

	it('uses custom fallback render prop', () => {
		const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
		render(
			<ErrorBoundary fallback={(error, retry) => <button onClick={retry}>custom:{error.message}</button>}>
				<Crash />
			</ErrorBoundary>,
		)
		expect(screen.getByRole('button', { name: 'custom:boom' })).toBeInTheDocument()
		errSpy.mockRestore()
	})
})
