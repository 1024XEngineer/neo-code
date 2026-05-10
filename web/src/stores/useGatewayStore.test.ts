import { describe, expect, it } from 'vitest'
import { useGatewayStore } from './useGatewayStore'

describe('useGatewayStore', () => {
	it('updates and resets gateway state', () => {
		const store = useGatewayStore.getState()
		store.setConnectionState('connected')
		store.setToken('tok')
		store.setCurrentRunId('run1')
		store.setAuthenticated(true)
		store.notifyProviderChanged()
		expect(useGatewayStore.getState().providerChangeTick).toBe(1)
		expect(useGatewayStore.getState().authenticated).toBe(true)
		store.reset()
		expect(useGatewayStore.getState().connectionState).toBe('disconnected')
		expect(useGatewayStore.getState().token).toBe('')
	})
})

