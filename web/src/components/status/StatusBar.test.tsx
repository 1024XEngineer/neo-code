import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import StatusBar from './StatusBar'

const runtime = {
	mode: 'browser',
	workdir: '',
	selectWorkdir: vi.fn().mockResolvedValue(''),
}

vi.mock('@/context/RuntimeProvider', () => ({
	useRuntime: () => runtime,
}))

describe('StatusBar', () => {
	it('does not render workdir in browser mode', () => {
		runtime.mode = 'browser'
		runtime.workdir = '/a'
		render(<StatusBar />)
		expect(screen.queryByTitle('点击切换工作区')).not.toBeInTheDocument()
	})

	it('renders and triggers workdir picker in electron mode', async () => {
		runtime.mode = 'electron'
		runtime.workdir = '/repo'
		render(<StatusBar />)
		const btn = screen.getByTitle('点击切换工作区')
		fireEvent.click(btn)
		expect(runtime.selectWorkdir).toHaveBeenCalled()
	})
})

