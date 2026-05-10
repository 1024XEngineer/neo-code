import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import ChatPage from './ChatPage'

vi.mock('@/components/layout/AppLayout', () => ({
	default: () => <div>layout-mounted</div>,
}))

describe('ChatPage', () => {
	it('renders AppLayout', () => {
		render(<ChatPage />)
		expect(screen.getByText('layout-mounted')).toBeInTheDocument()
	})
})

