import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import MessageList from './MessageList'
import { useChatStore } from '@/stores/useChatStore'

vi.mock('./MessageItem', () => ({
	default: ({ message, groupedWithPrev }: any) => (
		<div data-testid={`msg-${message.id}`}>{message.id}:{groupedWithPrev ? 'group' : 'solo'}</div>
	),
}))

describe('MessageList', () => {
	beforeEach(() => {
		useChatStore.setState({ messages: [], isGenerating: false } as any)
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
})
