import { create } from 'zustand'
import { type WSConnectionState } from '@/api/wsClient'

/** Gateway 连接状态 */
interface GatewayState {
  /** WS 连接状态 */
  connectionState: WSConnectionState
  /** 认证 Token */
  token: string
  /** 当前 run_id */
  currentRunId: string
  /** 是否已认证 */
  authenticated: boolean
  /** Provider 切换计数器，用于通知模型列表刷新 */
  providerChangeTick: number

  // Actions
  setConnectionState: (state: WSConnectionState) => void
  setToken: (token: string) => void
  setCurrentRunId: (runId: string) => void
  setAuthenticated: (v: boolean) => void
  notifyProviderChanged: () => void
  reset: () => void
}

const initialState = {
  connectionState: 'disconnected' as WSConnectionState,
  token: '',
  currentRunId: '',
  authenticated: false,
  providerChangeTick: 0,
}

export const useGatewayStore = create<GatewayState>((set) => ({
  ...initialState,

  setConnectionState: (connectionState) => set({ connectionState }),
  setToken: (token) => set({ token }),
  setCurrentRunId: (currentRunId) => set({ currentRunId }),
  setAuthenticated: (authenticated) => set({ authenticated }),
  notifyProviderChanged: () => set((state) => ({ providerChangeTick: state.providerChangeTick + 1 })),
  reset: () => set(initialState),
}))
