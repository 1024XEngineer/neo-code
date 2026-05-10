import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import ToolCallCard from './ToolCallCard'

vi.mock('./CheckpointInlineMark', () => ({
	default: ({ checkpointId }: { checkpointId: string }) => <span>cp:{checkpointId}</span>,
}))

describe('ToolCallCard', () => {
	it('shows running state and expands/collapses', () => {
		render(
			<ToolCallCard
				message={{
					id: 't1',
					role: 'tool',
					type: 'tool_call',
					content: '',
					toolName: 'bash',
					toolStatus: 'running',
					toolArgs: JSON.stringify({ command: 'echo hi' }),
					toolResult: 'hi',
					timestamp: 1,
				} as any}
			/>,
		)
		expect(screen.getByText('bash')).toBeInTheDocument()
		expect(screen.getByText('$')).toBeInTheDocument()
		fireEvent.click(screen.getByRole('button', { expanded: true }))
	})

	it('renders file edit diff detail', () => {
		render(
			<ToolCallCard
				message={{
					id: 't2',
					role: 'tool',
					type: 'tool_call',
					content: '',
					toolName: 'filesystem_edit',
					toolStatus: 'done',
					toolArgs: JSON.stringify({
						path: 'a.ts',
						search_string: 'old',
						replace_string: 'new',
					}),
					toolResult: 'ok',
					checkpointId: 'cp1',
					timestamp: 1,
				} as any}
			/>,
		)
		fireEvent.click(screen.getByRole('button', { expanded: false }))
		expect(screen.getAllByText('a.ts').length).toBeGreaterThan(0)
		expect(screen.getByText('old')).toBeInTheDocument()
		expect(screen.getByText('new')).toBeInTheDocument()
		expect(screen.getByText('cp:cp1')).toBeInTheDocument()
	})
})
