import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import ToastContainer from './ToastContainer'
import { useUIStore } from '@/stores/useUIStore'

describe('ToastContainer', () => {
	beforeEach(() => {
		useUIStore.setState({
			toasts: [],
			dismissToast: vi.fn(),
		} as any)
	})

	it('renders null when no toasts', () => {
		const { container } = render(<ToastContainer />)
		expect(container.firstChild).toBeNull()
	})

	it('renders toasts and dismisses on close click', () => {
		const dismissToast = vi.fn()
		useUIStore.setState({
			toasts: [{ id: 't1', message: 'ok', type: 'success' }],
			dismissToast,
		} as any)
		render(<ToastContainer />)
		expect(screen.getByText('ok')).toBeInTheDocument()
		fireEvent.click(screen.getByRole('button'))
		expect(dismissToast).toHaveBeenCalledWith('t1')
	})
})

