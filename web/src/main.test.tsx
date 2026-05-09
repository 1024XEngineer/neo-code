import { describe, expect, it, vi } from 'vitest'

describe('main entry', () => {
	it('mounts app with createRoot', async () => {
		document.body.innerHTML = '<div id="root"></div>'
		const render = vi.fn()
		const createRoot = vi.fn(() => ({ render }))

		vi.doMock('react-dom/client', () => ({
			default: { createRoot },
			createRoot,
		}))
		vi.doMock('./App', () => ({ default: () => null }))
		vi.doMock('./context/RuntimeProvider', () => ({
			RuntimeProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
		}))

		await import('./main')
		expect(createRoot).toHaveBeenCalledWith(document.getElementById('root'))
		expect(render).toHaveBeenCalled()
	})
})

