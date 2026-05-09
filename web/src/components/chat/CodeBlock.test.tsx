import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import CodeBlock from './CodeBlock'

describe('CodeBlock', () => {
	beforeEach(() => {
		Object.assign(navigator, {
			clipboard: { writeText: vi.fn() },
		})
	})

	it('renders inline code and copies content', () => {
		render(<CodeBlock code={'const a = 1'} language="typescript" />)
		const container = screen.getByText('const a = 1').closest('div') as HTMLElement
		fireEvent.mouseEnter(container)
		fireEvent.click(screen.getByTitle('复制'))
		expect(navigator.clipboard.writeText).toHaveBeenCalledWith('const a = 1')
	})

	it('renders file code block with line numbers', () => {
		render(<CodeBlock filename="a.ts" language="typescript" code={'line1\nline2'} />)
		expect(screen.getByText('a.ts')).toBeInTheDocument()
		expect(screen.getByText('line1')).toBeInTheDocument()
		expect(screen.getByText('line2')).toBeInTheDocument()
	})
})

