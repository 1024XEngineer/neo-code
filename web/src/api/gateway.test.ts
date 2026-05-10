import { describe, it, expect, vi, beforeEach } from 'vitest'
import { GatewayAPI } from './gateway'
import { Method } from './protocol'

describe('GatewayAPI', () => {
	const call = vi.fn()
	const ws = { call } as any
	let api: GatewayAPI

	beforeEach(() => {
		call.mockReset()
		call.mockResolvedValue({ type: 'ack', payload: {} })
		api = new GatewayAPI(ws)
	})

	it('maps authenticate and run methods', async () => {
		await api.authenticate('tok')
		await api.run({ input_text: 'hello' })

		expect(call).toHaveBeenNthCalledWith(1, Method.Authenticate, { token: 'tok' })
		expect(call).toHaveBeenNthCalledWith(2, Method.Run, { input_text: 'hello' })
	})

	it('maps optional session_id in listModels', async () => {
		await api.listModels()
		await api.listModels('s1')

		expect(call).toHaveBeenNthCalledWith(1, Method.ListModels, undefined)
		expect(call).toHaveBeenNthCalledWith(2, Method.ListModels, { session_id: 's1' })
	})

	it('maps optional provider_id in setSessionModel', async () => {
		await api.setSessionModel('s1', 'm1')
		await api.setSessionModel('s1', 'm1', 'p1')

		expect(call).toHaveBeenNthCalledWith(1, Method.SetSessionModel, { session_id: 's1', model_id: 'm1' })
		expect(call).toHaveBeenNthCalledWith(2, Method.SetSessionModel, { session_id: 's1', model_id: 'm1', provider_id: 'p1' })
	})

	it('maps workspace methods and optional remove_data', async () => {
		await api.listWorkspaces()
		await api.createWorkspace('/tmp/a', 'A')
		await api.switchWorkspace('h1')
		await api.renameWorkspace('h1', 'B')
		await api.deleteWorkspace('h1', true)

		expect(call).toHaveBeenNthCalledWith(1, Method.ListWorkspaces)
		expect(call).toHaveBeenNthCalledWith(2, Method.CreateWorkspace, { path: '/tmp/a', name: 'A' })
		expect(call).toHaveBeenNthCalledWith(3, Method.SwitchWorkspace, { workspace_hash: 'h1' })
		expect(call).toHaveBeenNthCalledWith(4, Method.RenameWorkspace, { workspace_hash: 'h1', name: 'B' })
		expect(call).toHaveBeenNthCalledWith(5, Method.DeleteWorkspace, { workspace_hash: 'h1', remove_data: true })
	})

	it('maps permission and user question resolution', async () => {
		await api.resolvePermission({ request_id: 'r1', decision: 'allow_once' })
		await api.resolveUserQuestion({ request_id: 'q1', status: 'answered', message: 'ok' })

		expect(call).toHaveBeenNthCalledWith(1, Method.ResolvePermission, { request_id: 'r1', decision: 'allow_once' })
		expect(call).toHaveBeenNthCalledWith(2, Method.UserQuestionAnswer, { request_id: 'q1', status: 'answered', message: 'ok' })
	})
})

