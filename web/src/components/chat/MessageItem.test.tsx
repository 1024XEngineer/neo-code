import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import MessageItem from './MessageItem'

vi.mock('./ToolCallCard', () => ({ default: () => <div>tool-card</div> }))
vi.mock('./VerificationMessage', () => ({ default: () => <div>verification-card</div> }))
vi.mock('./AcceptanceMessage', () => ({ default: () => <div>acceptance-card</div> }))
vi.mock('./CodeBlock', () => ({ default: ({ code }: { code: string }) => <pre>{code}</pre> }))
vi.mock('./MarkdownContent', () => ({ default: ({ content }: { content: string }) => <span>{content}</span> }))
vi.mock('@/context/RuntimeProvider', () => ({ useGatewayAPI: () => null }))

describe('MessageItem', () => {
	it('renders system message', () => {
		render(<MessageItem message={{ id: 's1', role: 'assistant', type: 'system', content: 'sys', timestamp: 1 } as any} />)
		expect(screen.getByText('sys')).toBeInTheDocument()
	})

	it('renders welcome message', () => {
		render(<MessageItem message={{ id: 'w1', role: 'assistant', type: 'welcome', content: 'hello', timestamp: 1 } as any} />)
		expect(screen.getByText('hello')).toBeInTheDocument()
	})

	it('renders thinking message and toggles details', () => {
		render(
			<MessageItem
				message={{ id: 't1', role: 'assistant', type: 'thinking', content: 'reasoning', timestamp: 1, streaming: false, thinkingData: { collapsed: true } } as any}
			/>,
		)
		fireEvent.click(screen.getByText('AI 思考过程'))
		expect(screen.getByText('reasoning')).toBeInTheDocument()
	})

	it('renders tool/verification/acceptance delegates', () => {
		const { rerender } = render(<MessageItem message={{ id: 'm1', role: 'tool', type: 'tool_call', content: '', timestamp: 1 } as any} />)
		expect(screen.getByText('tool-card')).toBeInTheDocument()
		rerender(<MessageItem message={{ id: 'm2', role: 'assistant', type: 'verification', content: '', timestamp: 1 } as any} />)
		expect(screen.getByText('verification-card')).toBeInTheDocument()
		rerender(<MessageItem message={{ id: 'm3', role: 'assistant', type: 'acceptance', content: '', timestamp: 1 } as any} />)
		expect(screen.getByText('acceptance-card')).toBeInTheDocument()
	})

	it('renders code and plain assistant messages', () => {
		const { rerender } = render(<MessageItem message={{ id: 'c1', role: 'assistant', type: 'code', content: 'const a=1', timestamp: 1 } as any} />)
		expect(screen.getByText('const a=1')).toBeInTheDocument()
		rerender(<MessageItem message={{ id: 'a1', role: 'assistant', type: 'text', content: 'answer', timestamp: 1 } as any} />)
		expect(screen.getByText('answer')).toBeInTheDocument()
	})
})

