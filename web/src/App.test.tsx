import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import App from './App'

const runtimeState = {
	status: 'connected',
	mode: 'browser',
	error: '',
	loadingMessage: '',
	retry: vi.fn(),
}

vi.mock('./context/RuntimeProvider', () => ({
	useRuntime: () => runtimeState,
}))

vi.mock('./pages/ChatPage', () => ({
	default: () => <div>chat-page</div>,
}))

vi.mock('./pages/ConnectPage', () => ({
	default: () => <div>connect-page</div>,
}))

describe('App routes by runtime status', () => {
	it('renders loading screen', () => {
		runtimeState.status = 'loading'
		runtimeState.loadingMessage = 'booting'
		render(<App />)
		expect(screen.getByText('booting')).toBeInTheDocument()
	})

	it('renders connect page for browser needs_config', () => {
		runtimeState.status = 'needs_config'
		runtimeState.mode = 'browser'
		render(<App />)
		expect(screen.getByText('connect-page')).toBeInTheDocument()
	})

	it('renders electron error screen with retry', () => {
		runtimeState.status = 'error'
		runtimeState.mode = 'electron'
		runtimeState.error = 'boom'
		render(<App />)
		expect(screen.getByText('Gateway connection failed')).toBeInTheDocument()
		fireEvent.click(screen.getByRole('button', { name: 'Retry connection' }))
		expect(runtimeState.retry).toHaveBeenCalled()
	})

	it('renders chat routes when connected', () => {
		runtimeState.status = 'connected'
		runtimeState.mode = 'browser'
		render(<App />)
		expect(screen.getByText('chat-page')).toBeInTheDocument()
	})
})

