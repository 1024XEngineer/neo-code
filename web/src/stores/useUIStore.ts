import { create } from 'zustand'
import type { DiffHunk, DiffLine } from '@/utils/patchParser'
import type { GitDiffEntry, GitDiffSummaryPayload, ReadGitDiffFilePayload } from '@/api/protocol'

export interface Toast {
  id: string
  message: string
  type: 'info' | 'error' | 'success'
}

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

export interface GitDiffSummary {
  in_git_repo: boolean
  branch: string
  ahead: number
  behind: number
  truncated: boolean
  total_count: number
  files: GitDiffEntry[]
}

export const CHANGES_PREVIEW_TAB_ID = 'changes'
export const GIT_DIFF_PREVIEW_TAB_ID = 'git-diff'

export interface PreviewTabBase {
  id: string
  kind: 'changes' | 'git-diff' | 'file' | 'git-diff-file'
  title: string
  closable: boolean
}

export interface ChangesPreviewTab extends PreviewTabBase {
  kind: 'changes'
}

export interface GitDiffPreviewTab extends PreviewTabBase {
  kind: 'git-diff'
}

export interface PreviewTabContentPayload {
  path: string
  content: string
  encoding?: string
  size?: number
  truncated?: boolean
  is_binary?: boolean
  mod_time?: string
}

export interface FilePreviewTab extends PreviewTabBase {
  kind: 'file'
  path: string
  content: string
  loading: boolean
  loaded: boolean
  error: string
  encoding?: string
  size?: number
  truncated: boolean
  is_binary: boolean
  mod_time?: string
}

export interface GitDiffFilePreviewTab extends PreviewTabBase {
  kind: 'git-diff-file'
  path: string
  old_path?: string
  status: GitDiffEntry['status']
  original_content: string
  modified_content: string
  loading: boolean
  loaded: boolean
  error: string
  encoding?: string
  truncated: boolean
  is_binary: boolean
  size_original?: number
  size_modified?: number
}

export type PreviewTab = ChangesPreviewTab | GitDiffPreviewTab | FilePreviewTab | GitDiffFilePreviewTab

interface OpenPreviewTabResult {
  id: string
  created: boolean
}

const TOAST_AUTO_DISMISS_MS = 5000

function createChangesPreviewTab(): ChangesPreviewTab {
  return {
    id: CHANGES_PREVIEW_TAB_ID,
    kind: 'changes',
    title: '文件变更',
    closable: false,
  }
}

function createGitDiffPreviewTab(): GitDiffPreviewTab {
  return {
    id: GIT_DIFF_PREVIEW_TAB_ID,
    kind: 'git-diff',
    title: 'Git Diff',
    closable: false,
  }
}

function createDefaultPreviewTabs(): PreviewTab[] {
  return [createChangesPreviewTab(), createGitDiffPreviewTab()]
}

function createFilePreviewTab(path: string): FilePreviewTab {
  const normalizedPath = path.trim()
  const segments = normalizedPath.split('/').filter(Boolean)
  return {
    id: `file:${normalizedPath}`,
    kind: 'file',
    title: segments[segments.length - 1] || normalizedPath,
    closable: true,
    path: normalizedPath,
    content: '',
    loading: true,
    loaded: false,
    error: '',
    truncated: false,
    is_binary: false,
  }
}

function createGitDiffFilePreviewTab(path: string): GitDiffFilePreviewTab {
  const normalizedPath = path.trim()
  const segments = normalizedPath.split('/').filter(Boolean)
  return {
    id: `git-diff-file:${normalizedPath}`,
    kind: 'git-diff-file',
    title: segments[segments.length - 1] || normalizedPath,
    closable: true,
    path: normalizedPath,
    status: 'modified',
    old_path: undefined,
    original_content: '',
    modified_content: '',
    loading: true,
    loaded: false,
    error: '',
    truncated: false,
    is_binary: false,
  }
}

function getFallbackPreviewTabID(tabs: PreviewTab[], closingID: string): string {
  const index = tabs.findIndex((tab) => tab.id === closingID)
  if (index <= 0) {
    return CHANGES_PREVIEW_TAB_ID
  }

  const previous = tabs[index - 1]
  if (previous) {
    return previous.id
  }

  const next = tabs[index + 1]
  return next?.id || CHANGES_PREVIEW_TAB_ID
}

function createEmptyGitDiffSummary(): GitDiffSummary {
  return {
    in_git_repo: false,
    branch: '',
    ahead: 0,
    behind: 0,
    truncated: false,
    total_count: 0,
    files: [],
  }
}

interface UIState {
  sidebarOpen: boolean
  sidebarWidth: number
  changesPanelOpen: boolean
  changesPanelWidth: number
  fileTreePanelOpen: boolean
  fileTreePanelWidth: number
  todoStripExpanded: boolean
  theme: 'light' | 'dark'
  searchQuery: string
  fileChanges: FileChange[]
  gitDiffSummary: GitDiffSummary
  gitDiffLoading: boolean
  gitDiffError: string
  gitDiffRefreshToken: number
  previewTabs: PreviewTab[]
  activePreviewTabId: string
  toasts: Toast[]

  toggleSidebar: () => void
  setSidebarOpen: (open: boolean) => void
  setSidebarWidth: (width: number) => void
  toggleChangesPanel: () => void
  setChangesPanelWidth: (width: number) => void
  toggleFileTreePanel: () => void
  setFileTreePanelWidth: (width: number) => void
  setTodoStripExpanded: (expanded: boolean) => void
  setTheme: (theme: 'light' | 'dark') => void
  setSearchQuery: (query: string) => void
  addFileChange: (change: FileChange) => void
  replaceFileChanges: (changes: FileChange[]) => void
  acceptFileChange: (id: string) => void
  rejectFileChange: (id: string) => void
  clearFileChanges: () => void
  openPreviewTab: (path: string) => OpenPreviewTabResult
  openGitDiffTab: (path: string) => OpenPreviewTabResult
  activatePreviewTab: (id: string) => void
  closePreviewTab: (id: string) => void
  setPreviewTabLoading: (id: string) => void
  setPreviewTabContent: (id: string, payload: PreviewTabContentPayload) => void
  setPreviewTabError: (id: string, error: string) => void
  setGitDiffLoading: (loading: boolean) => void
  setGitDiffSummary: (payload: GitDiffSummaryPayload) => void
  setGitDiffError: (error: string) => void
  refreshGitDiff: () => void
  setGitDiffTabLoading: (id: string) => void
  setGitDiffTabContent: (id: string, payload: ReadGitDiffFilePayload) => void
  setGitDiffTabError: (id: string, error: string) => void
  resetPreviewTabs: () => void
  showToast: (message: string, type?: Toast['type']) => void
  dismissToast: (id: string) => void
}

let toastIdCounter = 0

export const useUIStore = create<UIState>((set) => ({
  sidebarOpen: true,
  sidebarWidth: 280,
  changesPanelOpen: false,
  changesPanelWidth: 380,
  fileTreePanelOpen: false,
  fileTreePanelWidth: 340,
  todoStripExpanded: false,
  theme: (localStorage.getItem('neocode-theme') as 'light' | 'dark') || 'dark',
  searchQuery: '',
  fileChanges: [],
  gitDiffSummary: createEmptyGitDiffSummary(),
  gitDiffLoading: false,
  gitDiffError: '',
  gitDiffRefreshToken: 0,
  previewTabs: createDefaultPreviewTabs(),
  activePreviewTabId: CHANGES_PREVIEW_TAB_ID,
  toasts: [],

  toggleSidebar: () => set((state) => ({ sidebarOpen: !state.sidebarOpen })),
  setSidebarOpen: (sidebarOpen) => set({ sidebarOpen }),
  setSidebarWidth: (sidebarWidth) => set({ sidebarWidth }),
  toggleChangesPanel: () => set((state) => ({ changesPanelOpen: !state.changesPanelOpen })),
  setChangesPanelWidth: (changesPanelWidth) => set({ changesPanelWidth }),
  toggleFileTreePanel: () => set((state) => ({ fileTreePanelOpen: !state.fileTreePanelOpen })),
  setFileTreePanelWidth: (fileTreePanelWidth) => set({ fileTreePanelWidth }),
  setTodoStripExpanded: (todoStripExpanded) => set({ todoStripExpanded }),
  setTheme: (theme) => {
    localStorage.setItem('neocode-theme', theme)
    document.documentElement.setAttribute('data-theme', theme)
    set({ theme })
  },
  setSearchQuery: (searchQuery) => set({ searchQuery }),
  addFileChange: (change) =>
    set((state) => ({
      fileChanges: [...state.fileChanges, change],
    })),
  replaceFileChanges: (fileChanges) => set({ fileChanges }),
  acceptFileChange: (id) =>
    set((state) => ({
      fileChanges: state.fileChanges.map((change) => (change.id === id ? { ...change, status: 'accepted' as const } : change)),
    })),
  rejectFileChange: (id) =>
    set((state) => ({
      fileChanges: state.fileChanges.map((change) => (change.id === id ? { ...change, status: 'rejected' as const } : change)),
    })),
  clearFileChanges: () => set({ fileChanges: [] }),
  openPreviewTab: (path) => {
    const normalizedPath = path.trim()
    const tabID = `file:${normalizedPath}`
    let created = false

    set((state) => {
      const existing = state.previewTabs.find((tab) => tab.id === tabID)
      if (existing) {
        return {
          activePreviewTabId: tabID,
          changesPanelOpen: true,
        }
      }

      created = true
      return {
        previewTabs: [...state.previewTabs, createFilePreviewTab(normalizedPath)],
        activePreviewTabId: tabID,
        changesPanelOpen: true,
      }
    })

    return { id: tabID, created }
  },
  openGitDiffTab: (path) => {
    const normalizedPath = path.trim()
    const tabID = `git-diff-file:${normalizedPath}`
    let created = false

    set((state) => {
      const existing = state.previewTabs.find((tab) => tab.id === tabID)
      if (existing) {
        return {
          activePreviewTabId: tabID,
          changesPanelOpen: true,
        }
      }

      created = true
      return {
        previewTabs: [...state.previewTabs, createGitDiffFilePreviewTab(normalizedPath)],
        activePreviewTabId: tabID,
        changesPanelOpen: true,
      }
    })

    return { id: tabID, created }
  },
  activatePreviewTab: (id) =>
    set((state) => ({
      activePreviewTabId: state.previewTabs.some((tab) => tab.id === id) ? id : CHANGES_PREVIEW_TAB_ID,
      changesPanelOpen: true,
    })),
  closePreviewTab: (id) =>
    set((state) => {
      if (id === CHANGES_PREVIEW_TAB_ID || id === GIT_DIFF_PREVIEW_TAB_ID) {
        return state
      }

      const nextTabs = state.previewTabs.filter((tab) => tab.id !== id)
      if (nextTabs.length === state.previewTabs.length) {
        return state
      }

      return {
        previewTabs: nextTabs.length > 0 ? nextTabs : createDefaultPreviewTabs(),
        activePreviewTabId: state.activePreviewTabId === id
          ? getFallbackPreviewTabID(state.previewTabs, id)
          : state.activePreviewTabId,
      }
    }),
  setPreviewTabLoading: (id) =>
    set((state) => ({
      previewTabs: state.previewTabs.map((tab) =>
        tab.id === id && tab.kind === 'file'
          ? { ...tab, loading: true, error: '' }
          : tab,
      ),
      activePreviewTabId: id,
      changesPanelOpen: true,
    })),
  setPreviewTabContent: (id, payload) =>
    set((state) => ({
      previewTabs: state.previewTabs.map((tab) =>
        tab.id === id && tab.kind === 'file'
          ? {
              ...tab,
              title: tab.title || createFilePreviewTab(payload.path).title,
              path: payload.path,
              content: payload.content,
              loading: false,
              loaded: true,
              error: '',
              encoding: payload.encoding,
              size: payload.size,
              truncated: Boolean(payload.truncated),
              is_binary: Boolean(payload.is_binary),
              mod_time: payload.mod_time,
            }
          : tab,
      ),
    })),
  setPreviewTabError: (id, error) =>
    set((state) => ({
      previewTabs: state.previewTabs.map((tab) =>
        tab.id === id && tab.kind === 'file'
          ? { ...tab, loading: false, loaded: false, error, content: '' }
          : tab,
      ),
    })),
  setGitDiffLoading: (gitDiffLoading) => set({ gitDiffLoading, gitDiffError: gitDiffLoading ? '' : '' }),
  setGitDiffSummary: (payload) =>
    set({
      gitDiffSummary: {
        in_git_repo: Boolean(payload.in_git_repo),
        branch: payload.branch || '',
        ahead: payload.ahead || 0,
        behind: payload.behind || 0,
        truncated: Boolean(payload.truncated),
        total_count: payload.total_count || 0,
        files: payload.files || [],
      },
      gitDiffLoading: false,
      gitDiffError: '',
    }),
  setGitDiffError: (gitDiffError) => set({ gitDiffError, gitDiffLoading: false }),
  refreshGitDiff: () => set((state) => ({ gitDiffRefreshToken: state.gitDiffRefreshToken + 1 })),
  setGitDiffTabLoading: (id) =>
    set((state) => ({
      previewTabs: state.previewTabs.map((tab) =>
        tab.id === id && tab.kind === 'git-diff-file'
          ? { ...tab, loading: true, error: '' }
          : tab,
      ),
      activePreviewTabId: id,
      changesPanelOpen: true,
    })),
  setGitDiffTabContent: (id, payload) =>
    set((state) => ({
      previewTabs: state.previewTabs.map((tab) =>
        tab.id === id && tab.kind === 'git-diff-file'
          ? {
              ...tab,
              title: tab.title || createGitDiffFilePreviewTab(payload.path).title,
              path: payload.path,
              old_path: payload.old_path,
              status: payload.status,
              original_content: payload.original_content,
              modified_content: payload.modified_content,
              loading: false,
              loaded: true,
              error: '',
              encoding: payload.encoding,
              truncated: Boolean(payload.truncated),
              is_binary: Boolean(payload.is_binary),
              size_original: payload.size_original,
              size_modified: payload.size_modified,
            }
          : tab,
      ),
    })),
  setGitDiffTabError: (id, error) =>
    set((state) => ({
      previewTabs: state.previewTabs.map((tab) =>
        tab.id === id && tab.kind === 'git-diff-file'
          ? { ...tab, loading: false, loaded: false, error, original_content: '', modified_content: '' }
          : tab,
      ),
    })),
  resetPreviewTabs: () =>
    set({
      gitDiffSummary: createEmptyGitDiffSummary(),
      gitDiffLoading: false,
      gitDiffError: '',
      gitDiffRefreshToken: 0,
      previewTabs: createDefaultPreviewTabs(),
      activePreviewTabId: CHANGES_PREVIEW_TAB_ID,
    }),
  showToast: (message, type = 'info') => {
    const id = `toast_${++toastIdCounter}`
    set((state) => ({
      toasts: [...state.toasts, { id, message, type }],
    }))
    setTimeout(() => {
      useUIStore.getState().dismissToast(id)
    }, TOAST_AUTO_DISMISS_MS)
  },
  dismissToast: (id) =>
    set((state) => ({
      toasts: state.toasts.filter((toast) => toast.id !== id),
    })),
}))
