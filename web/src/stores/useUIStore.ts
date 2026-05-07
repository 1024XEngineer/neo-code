import { create } from 'zustand'
import type { DiffHunk, DiffLine } from '@/utils/patchParser'

/** Toast 通知 */
export interface Toast {
  id: string
  message: string
  type: 'info' | 'error' | 'success'
}

/** 文件变更 */
export interface FileChange {
  id: string
  path: string
  status: 'added' | 'modified' | 'deleted' | 'accepted' | 'rejected'
  additions: number
  deletions: number
  diff?: DiffLine[]
  hunks?: DiffHunk[]
  checkpoint_id?: string
}

const TOAST_AUTO_DISMISS_MS = 5000

/** UI 状态 */
interface UIState {
  /** 侧边栏是否展开 */
  sidebarOpen: boolean
  /** 侧边栏宽度 */
  sidebarWidth: number
  /** 文件变更面板是否展开 */
  changesPanelOpen: boolean
  /** 文件变更面板宽度 */
  changesPanelWidth: number
  /** 文件树面板是否展开 */
  fileTreePanelOpen: boolean
  /** 文件树面板宽度 */
  fileTreePanelWidth: number
  /** 输入框上方 Todo 折叠条是否展开 */
  todoStripExpanded: boolean
  /** 当前主题：light / dark */
  theme: 'light' | 'dark'
  /** 搜索查询 */
  searchQuery: string
  /** 文件变更列表 */
  fileChanges: FileChange[]
  /** Toast 列表 */
  toasts: Toast[]

  // Actions
  toggleSidebar: () => void
  setSidebarOpen: (open: boolean) => void
  toggleChangesPanel: () => void
  toggleFileTreePanel: () => void
  setTodoStripExpanded: (expanded: boolean) => void
  setTheme: (theme: 'light' | 'dark') => void
  setSearchQuery: (q: string) => void
  addFileChange: (change: FileChange) => void
  replaceFileChanges: (changes: FileChange[]) => void
  acceptFileChange: (id: string) => void
  rejectFileChange: (id: string) => void
  clearFileChanges: () => void
  showToast: (message: string, type?: Toast['type']) => void
  dismissToast: (id: string) => void
}

let toastIdCounter = 0

export const useUIStore = create<UIState>((set) => ({
  sidebarOpen: true,
  sidebarWidth: 260,
  changesPanelOpen: false,
  changesPanelWidth: 320,
  fileTreePanelOpen: false,
  fileTreePanelWidth: 280,
  todoStripExpanded: false,
  theme: 'dark',
  searchQuery: '',
  fileChanges: [],
  toasts: [],

  toggleSidebar: () => set((s) => ({ sidebarOpen: !s.sidebarOpen })),
  setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
  toggleChangesPanel: () => set((s) => ({ changesPanelOpen: !s.changesPanelOpen })),
  toggleFileTreePanel: () => set((s) => ({ fileTreePanelOpen: !s.fileTreePanelOpen })),
  setTodoStripExpanded: (todoStripExpanded) => set({ todoStripExpanded }),
  setTheme: (theme) => {
    document.documentElement.setAttribute('data-theme', theme)
    set({ theme })
  },
  setSearchQuery: (searchQuery) => set({ searchQuery }),
  addFileChange: (change) =>
    set((s) => ({
      fileChanges: [...s.fileChanges, change],
    })),
  replaceFileChanges: (fileChanges) => set({ fileChanges }),
  acceptFileChange: (id) =>
    set((s) => ({
      fileChanges: s.fileChanges.map((c) => (c.id === id ? { ...c, status: 'accepted' as const } : c)),
    })),
  rejectFileChange: (id) =>
    set((s) => ({
      fileChanges: s.fileChanges.map((c) => (c.id === id ? { ...c, status: 'rejected' as const } : c)),
    })),
  clearFileChanges: () => set({ fileChanges: [] }),
  showToast: (message, type = 'info') => {
    const id = `toast_${++toastIdCounter}`
    set((s) => ({
      toasts: [...s.toasts, { id, message, type }],
    }))
    // Auto-dismiss after timeout
    setTimeout(() => {
      useUIStore.getState().dismissToast(id)
    }, TOAST_AUTO_DISMISS_MS)
  },
  dismissToast: (id) =>
    set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}))
