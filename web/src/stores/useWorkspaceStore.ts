import { create } from 'zustand'
import { type GatewayAPI } from '@/api/gateway'
import { type Workspace as APIWorkspace } from '@/api/protocol'
import { useSessionStore } from '@/stores/useSessionStore'
import { useChatStore } from '@/stores/useChatStore'
import { useUIStore } from '@/stores/useUIStore'
import { useGatewayStore } from '@/stores/useGatewayStore'

/** 工作区记录 */
export interface Workspace {
  hash: string
  path: string
  name: string
  createdAt: string
  updatedAt: string
}

/** 工作区状态 */
interface WorkspaceState {
  workspaces: Workspace[]
  currentWorkspaceHash: string
  loading: boolean

  setWorkspaces: (workspaces: Workspace[]) => void
  setCurrentWorkspaceHash: (hash: string) => void
  setLoading: (loading: boolean) => void
  fetchWorkspaces: (gatewayAPI: GatewayAPI) => Promise<void>
  switchWorkspace: (hash: string, gatewayAPI: GatewayAPI) => Promise<void>
  createWorkspace: (path: string, gatewayAPI: GatewayAPI, name?: string) => Promise<void>
  renameWorkspace: (hash: string, name: string, gatewayAPI: GatewayAPI) => Promise<void>
  deleteWorkspace: (hash: string, gatewayAPI: GatewayAPI) => Promise<void>
}

function mapAPIWorkspace(w: APIWorkspace): Workspace {
  return {
    hash: w.hash,
    path: w.path,
    name: w.name || w.path,
    createdAt: w.created_at,
    updatedAt: w.updated_at,
  }
}

let _fetchWorkspacesPromise: Promise<void> | null = null

export const useWorkspaceStore = create<WorkspaceState>((set, get) => ({
  workspaces: [],
  currentWorkspaceHash: '',
  loading: false,

  setWorkspaces: (workspaces) => set({ workspaces }),
  setCurrentWorkspaceHash: (currentWorkspaceHash) => set({ currentWorkspaceHash }),
  setLoading: (loading) => set({ loading }),

  fetchWorkspaces: async (gatewayAPI) => {
    if (_fetchWorkspacesPromise) return _fetchWorkspacesPromise

    _fetchWorkspacesPromise = (async () => {
      set({ loading: true })
      try {
        const result = await gatewayAPI.listWorkspaces()
        const list = result.payload.workspaces.map(mapAPIWorkspace)
        set({ workspaces: list, loading: false })

        // 若当前无选中工作区且列表非空，默认选中第一个
        const state = get()
        if (!state.currentWorkspaceHash && list.length > 0) {
          set({ currentWorkspaceHash: list[0].hash })
        }
      } catch (err) {
        console.error('fetchWorkspaces failed:', err)
        set({ loading: false })
        useUIStore.getState().showToast('Failed to load workspaces', 'error')
      } finally {
        _fetchWorkspacesPromise = null
      }
    })()

    return _fetchWorkspacesPromise
  },

  switchWorkspace: async (hash, gatewayAPI) => {
    if (!hash || hash === get().currentWorkspaceHash) return

    if (useChatStore.getState().isGenerating) {
      useUIStore.getState().showToast('Cannot switch workspace while generating; stop the current run first.', 'info')
      return
    }

    set({ loading: true })

    // 先清空所有前端状态（防止重连 handler 读到旧 sessionId 竞态）
    useChatStore.getState().clearMessages()
    useChatStore.getState().setTransitioning(true)
    useSessionStore.setState({
      currentSessionId: '',
      currentProjectId: '',
      projects: [],
    })
    useSessionStore.getState().resetForWorkspaceSwitch()
    useGatewayStore.getState().setCurrentRunId('')
    useUIStore.getState().clearFileChanges()
    useUIStore.getState().setSearchQuery('')

    try {
      await gatewayAPI.switchWorkspace(hash)
      set({ currentWorkspaceHash: hash })

      // 加载新工作区的会话列表
      await useSessionStore.getState().fetchSessions(gatewayAPI, true)
    } catch (err) {
      console.error('switchWorkspace failed:', err)
      useUIStore.getState().showToast('Failed to switch workspace', 'error')
    } finally {
      useChatStore.getState().setTransitioning(false)
      set({ loading: false })
    }
  },

  createWorkspace: async (path, gatewayAPI, name) => {
    if (useChatStore.getState().isGenerating) {
      useUIStore.getState().showToast('Cannot switch workspace while generating; stop the current run first.', 'info')
      return
    }

    set({ loading: true })

    // 先清空所有前端状态（与 switchWorkspace 保持一致）
    useChatStore.getState().clearMessages()
    useChatStore.getState().setTransitioning(true)
    useSessionStore.setState({
      currentSessionId: '',
      currentProjectId: '',
      projects: [],
    })
    useSessionStore.getState().resetForWorkspaceSwitch()
    useGatewayStore.getState().setCurrentRunId('')
    useUIStore.getState().clearFileChanges()
    useUIStore.getState().setSearchQuery('')

    try {
      const result = await gatewayAPI.createWorkspace(path, name)
      const w = mapAPIWorkspace(result.payload.workspace)
      set((state) => ({
        workspaces: [w, ...state.workspaces.filter((x) => x.hash !== w.hash)],
      }))

      // 通知后端切换到新工作区
      await gatewayAPI.switchWorkspace(w.hash)
      set({ currentWorkspaceHash: w.hash })

      // 加载新工作区的会话列表
      await useSessionStore.getState().fetchSessions(gatewayAPI, true)
      useUIStore.getState().showToast('Workspace created', 'success')
    } catch (err) {
      console.error('createWorkspace failed:', err)
      set({ loading: false })
      useUIStore.getState().showToast('Failed to create workspace', 'error')
    } finally {
      useChatStore.getState().setTransitioning(false)
      set({ loading: false })
    }
  },

  renameWorkspace: async (hash, name, gatewayAPI) => {
    try {
      await gatewayAPI.renameWorkspace(hash, name)
      set((state) => ({
        workspaces: state.workspaces.map((w) =>
          w.hash === hash ? { ...w, name } : w
        ),
      }))
    } catch (err) {
      console.error('renameWorkspace failed:', err)
      useUIStore.getState().showToast('Failed to rename workspace', 'error')
    }
  },

  deleteWorkspace: async (hash, gatewayAPI) => {
    try {
      await gatewayAPI.deleteWorkspace(hash)
      const remaining = get().workspaces.filter((w) => w.hash !== hash)
      set({ workspaces: remaining })

      // 若删除的是当前工作区，切换到剩余的第一个
      if (get().currentWorkspaceHash === hash && remaining.length > 0) {
        await get().switchWorkspace(remaining[0].hash, gatewayAPI)
      }
    } catch (err) {
      console.error('deleteWorkspace failed:', err)
      useUIStore.getState().showToast('Failed to delete workspace', 'error')
    }
  },
}))
