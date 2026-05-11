import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import MessageList from './MessageList'
import { useChatStore } from '@/stores/useChatStore'

vi.mock('./MessageItem', () => ({
	default: ({ message, groupedWithPrev }: any) => (
		<div data-testid={`msg-${message.id}`}>{message.id}:{groupedWithPrev ? 'group' : 'solo'}</div>
	),
}))

describe('MessageList', () => {
	let scrollIntoViewMock: ReturnType<typeof vi.fn>

	beforeEach(() => {
		useChatStore.setState({ messages: [], isGenerating: false } as any)
		scrollIntoViewMock = vi.fn()
		window.HTMLElement.prototype.scrollIntoView = scrollIntoViewMock as unknown as typeof window.HTMLElement.prototype.scrollIntoView
	})

	it('renders empty state when no messages', () => {
		render(<MessageList />)
		expect(screen.getByText('开始你的 AI 编程之旅')).toBeInTheDocument()
	})

	it('reorders process messages before assistant text within AI turn', () => {
		useChatStore.setState({
			messages: [
				{ id: 'u1', role: 'user', type: 'text', content: 'q', timestamp: 1 },
				{ id: 'a1', role: 'assistant', type: 'text', content: 'answer', timestamp: 2 },
				{ id: 't1', role: 'tool', type: 'tool_call', content: '', timestamp: 3 },
				{ id: 'a2', role: 'assistant', type: 'thinking', content: 'thinking', timestamp: 4 },
			],
		} as any)

		render(<MessageList />)
		const ids = screen.getAllByTestId(/msg-/).map((x) => x.textContent)
		expect(ids).toEqual(['u1:solo', 't1:solo', 'a2:group', 'a1:group'])
	})

	it('scrolls instantly to the bottom when history messages first load', () => {
		useChatStore.setState({
			messages: [
				{ id: 'u1', role: 'user', type: 'text', content: 'q', timestamp: 1 },
				{ id: 'a1', role: 'assistant', type: 'text', content: 'answer', timestamp: 2 },
			],
			isGenerating: false,
		} as any)

		render(<MessageList />)

		expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'instant' })
	})

	it('smoothly scrolls when the user sends a new message', () => {
		useChatStore.setState({
			messages: [{ id: 'a1', role: 'assistant', type: 'text', content: 'answer', timestamp: 1 }],
			isGenerating: false,
		} as any)
		render(<MessageList />)
		scrollIntoViewMock.mockClear()

		act(() => {
			useChatStore.setState({
				messages: [
					{ id: 'a1', role: 'assistant', type: 'text', content: 'answer', timestamp: 1 },
					{ id: 'u1', role: 'user', type: 'text', content: 'follow-up', timestamp: 2 },
				],
			} as any)
		})

		expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'smooth' })
	})

	it('keeps following generation when the user has not scrolled up', () => {
		useChatStore.setState({
			messages: [{ id: 'u1', role: 'user', type: 'text', content: 'q', timestamp: 1 }],
			isGenerating: false,
		} as any)
		render(<MessageList />)
		scrollIntoViewMock.mockClear()

		act(() => {
			useChatStore.setState({
				messages: [
					{ id: 'u1', role: 'user', type: 'text', content: 'q', timestamp: 1 },
					{ id: 'a1', role: 'assistant', type: 'text', content: 'partial', timestamp: 2, streaming: true },
				],
				isGenerating: true,
			} as any)
		})

		expect(scrollIntoViewMock).toHaveBeenCalledWith({ behavior: 'instant' })
	})
})
