import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import SkillPicker from './SkillPicker'
import { useChatStore } from '@/stores/useChatStore'
import { useUIStore } from '@/stores/useUIStore'

describe('SkillPicker', () => {
	beforeEach(() => {
		useChatStore.setState({ isGenerating: false } as any)
		useUIStore.setState({ showToast: vi.fn() } as any)
	})

	it('renders empty state when no skills', async () => {
		const api = {
			listAvailableSkills: vi.fn().mockResolvedValue({ payload: { skills: [] } }),
		} as any
		render(<SkillPicker gatewayAPI={api} sessionId="s1" onClose={vi.fn()} />)
		await screen.findByText('暂无可用技能')
	})

	it('toggles skill activation and reloads list', async () => {
		const api = {
			listAvailableSkills: vi
				.fn()
				.mockResolvedValueOnce({
					payload: {
						skills: [{
							active: false,
							descriptor: { id: 'sk1', name: 'Skill 1', description: 'desc', scope: 'explicit' },
						}],
					},
				})
				.mockResolvedValueOnce({
					payload: {
						skills: [{
							active: true,
							descriptor: { id: 'sk1', name: 'Skill 1', description: 'desc', scope: 'explicit' },
						}],
					},
				}),
			activateSessionSkill: vi.fn().mockResolvedValue({ payload: {} }),
			deactivateSessionSkill: vi.fn().mockResolvedValue({ payload: {} }),
		} as any

		render(<SkillPicker gatewayAPI={api} sessionId="s1" onClose={vi.fn()} />)
		const activateBtn = await screen.findByRole('button', { name: '激活' })
		fireEvent.click(activateBtn)

		await waitFor(() => expect(api.activateSessionSkill).toHaveBeenCalledWith('s1', 'sk1'))
		expect(api.listAvailableSkills).toHaveBeenCalledTimes(2)
	})

	it('blocks toggle when session is missing', async () => {
		const showToast = vi.fn()
		useUIStore.setState({ showToast } as any)
		const api = {
			listAvailableSkills: vi.fn().mockResolvedValue({
				payload: {
					skills: [{ active: false, descriptor: { id: 'sk1', name: 'Skill 1' } }],
				},
			}),
			activateSessionSkill: vi.fn(),
		} as any

		render(<SkillPicker gatewayAPI={api} sessionId="" onClose={vi.fn()} />)
		const activateBtn = await screen.findByRole('button', { name: '激活' })
		fireEvent.click(activateBtn)

		expect(api.activateSessionSkill).not.toHaveBeenCalled()
		expect(showToast).toHaveBeenCalledWith('Send a message first to start a session', 'error')
	})

	it('disables operation while generating', async () => {
		useChatStore.setState({ isGenerating: true } as any)
		const api = {
			listAvailableSkills: vi.fn().mockResolvedValue({
				payload: {
					skills: [{ active: false, descriptor: { id: 'sk1', name: 'Skill 1' } }],
				},
			}),
			activateSessionSkill: vi.fn(),
		} as any

		render(<SkillPicker gatewayAPI={api} sessionId="s1" onClose={vi.fn()} />)
		const activateBtn = await screen.findByRole('button', { name: '激活' })
		expect(activateBtn).toBeDisabled()
	})
})

