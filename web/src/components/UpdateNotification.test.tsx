import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { UpdateNotification } from './UpdateNotification'

describe('UpdateNotification', () => {
	beforeEach(() => {
		Object.defineProperty(window, 'electronAPI', {
			value: undefined,
			configurable: true,
			writable: true,
		})
	})

	it('renders nothing when electron api is missing', () => {
		const { container } = render(<UpdateNotification />)
		expect(container.firstChild).toBeNull()
	})

	it('shows available/downloaded updates and handles actions', async () => {
		let onAvailable: ((info: any) => void) | null = null
		let onDownloaded: ((info: any) => void) | null = null
		const quitAndInstall = vi.fn().mockResolvedValue(undefined)
		Object.defineProperty(window, 'electronAPI', {
			value: {
				onUpdateAvailable: (cb: any) => {
					onAvailable = cb
					return vi.fn()
				},
				onUpdateDownloaded: (cb: any) => {
					onDownloaded = cb
					return vi.fn()
				},
				quitAndInstall,
			},
			configurable: true,
		})
		render(<UpdateNotification />)

		act(() => {
			onAvailable?.({ version: '1.2.3' })
		})
		await waitFor(() => {
			expect(screen.getByText('A new version v1.2.3 is available, downloading...')).toBeInTheDocument()
		})

		act(() => {
			onDownloaded?.({ version: '1.2.3' })
		})
		fireEvent.click(screen.getByRole('button', { name: 'Restart Now' }))
		expect(quitAndInstall).toHaveBeenCalled()

		fireEvent.click(screen.getByTitle('Dismiss'))
		expect(screen.queryByText('NeoCode v1.2.3 is ready to install')).not.toBeInTheDocument()
	})
})
