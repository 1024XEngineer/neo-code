import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createWSClient } from './wsClient'
import { JSONRPC_VERSION, Method } from './protocol'

class MockWebSocket {
	static CONNECTING = 0
	static OPEN = 1
	static CLOSING = 2
	static CLOSED = 3

	static instances: MockWebSocket[] = []

	url: string
	readyState = MockWebSocket.CONNECTING
	onopen: (() => void) | null = null
	onclose: (() => void) | null = null
	onerror: (() => void) | null = null
	onmessage: ((event: { data: string }) => void) | null = null
	sent: string[] = []

	constructor(url: string) {
		this.url = url
		MockWebSocket.instances.push(this)
	}

	send(data: string) {
		this.sent.push(data)
	}

	close() {
		this.readyState = MockWebSocket.CLOSED
		this.onclose?.()
	}

	open() {
		this.readyState = MockWebSocket.OPEN
		this.onopen?.()
	}

	emit(data: unknown) {
		this.onmessage?.({ data: typeof data === 'string' ? data : JSON.stringify(data) })
	}
}

function latestWS(): MockWebSocket {
	const ws = MockWebSocket.instances.at(-1)
	if (!ws) throw new Error('websocket not created')
	return ws
}

describe('createWSClient', () => {
	beforeEach(() => {
		vi.useFakeTimers()
		MockWebSocket.instances = []
		vi.stubGlobal('WebSocket', MockWebSocket as any)
	})

	afterEach(() => {
		vi.clearAllTimers()
		vi.useRealTimers()
		vi.unstubAllGlobals()
	})

	it('builds ws url with endpoint and token', () => {
		const client = createWSClient({
			baseURL: 'http://127.0.0.1:8080/',
			endpoint: 'gateway/ws',
			token: 'a b',
		})
		client.connect()
		expect(latestWS().url).toBe('ws://127.0.0.1:8080/gateway/ws?token=a%20b')
		client.disconnect()
	})

	it('sends rpc request and resolves rpc response', async () => {
		const client = createWSClient({ baseURL: 'http://127.0.0.1:8080', rpcTimeout: 50 })
		client.connect()
		const ws = latestWS()
		ws.open()

		const promise = client.call('gateway.ping', { x: 1 })
		const req = JSON.parse(ws.sent[0])
		expect(req).toMatchObject({
			jsonrpc: JSONRPC_VERSION,
			method: 'gateway.ping',
			params: { x: 1 },
		})

		ws.emit({ jsonrpc: JSONRPC_VERSION, id: req.id, result: { ok: true } })
		await expect(promise).resolves.toEqual({ ok: true })
		client.disconnect()
	})

	it('rejects rpc call on timeout', async () => {
		const client = createWSClient({ baseURL: 'http://127.0.0.1:8080', rpcTimeout: 20 })
		client.connect()
		latestWS().open()
		const promise = client.call('x.y')
		promise.catch(() => {})
		await vi.advanceTimersByTimeAsync(21)
		await expect(promise).rejects.toThrow('RPC timeout: x.y')
		client.disconnect()
	})

	it('dispatches gateway.event notifications to event handlers', () => {
		const client = createWSClient({ baseURL: 'http://127.0.0.1:8080' })
		const handler = vi.fn()
		client.onEvent(handler)

		client.connect()
		const ws = latestWS()
		ws.open()
		ws.emit({
			jsonrpc: JSONRPC_VERSION,
			method: Method.Event,
			params: { type: 'event', payload: { a: 1 } },
		})
		expect(handler).toHaveBeenCalledWith({ type: 'event', payload: { a: 1 } })
		client.disconnect()
	})

	it('transitions to error then reconnects and fires onReconnect', async () => {
		const client = createWSClient({
			baseURL: 'http://127.0.0.1:8080',
			reconnectBaseInterval: 10,
			reconnectMaxInterval: 10,
		})
		const states: string[] = []
		const onReconnect = vi.fn()
		client.onStateChange((s) => states.push(s))
		client.onReconnect(onReconnect)

		client.connect()
		const ws1 = latestWS()
		ws1.open()
		ws1.close()

		expect(states).toContain('error')
		await vi.advanceTimersByTimeAsync(20)
		const ws2 = latestWS()
		expect(ws2).not.toBe(ws1)
		ws2.open()
		expect(onReconnect).toHaveBeenCalledTimes(1)
		expect(client.getState()).toBe('connected')
		client.disconnect()
	})

	it('closes stale connection by heartbeat timeout', async () => {
		const client = createWSClient({
			baseURL: 'http://127.0.0.1:8080',
			heartbeatTimeout: 10,
			heartbeatCheckInterval: 5,
			reconnectBaseInterval: 50,
			reconnectMaxInterval: 50,
		})
		client.connect()
		const ws = latestWS()
		const closeSpy = vi.spyOn(ws, 'close')
		ws.open()
		await vi.advanceTimersByTimeAsync(20)
		expect(closeSpy).toHaveBeenCalled()
		client.disconnect()
	})
})
