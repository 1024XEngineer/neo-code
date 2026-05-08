import { beforeEach, describe, expect, it } from 'vitest'
import { CHANGES_PREVIEW_TAB_ID, GIT_DIFF_PREVIEW_TAB_ID, useUIStore } from './useUIStore'

describe('useUIStore preview tabs', () => {
  beforeEach(() => {
    useUIStore.setState({
      changesPanelOpen: false,
      gitDiffSummary: {
        in_git_repo: false,
        branch: '',
        ahead: 0,
        behind: 0,
        truncated: false,
        total_count: 0,
        files: [],
      },
      gitDiffLoading: false,
      gitDiffError: '',
      gitDiffRefreshToken: 0,
      previewTabs: [
        {
          id: CHANGES_PREVIEW_TAB_ID,
          kind: 'changes',
          title: '文件变更',
          closable: false,
        },
        {
          id: GIT_DIFF_PREVIEW_TAB_ID,
          kind: 'git-diff',
          title: 'Git Diff',
          closable: false,
        },
      ],
      activePreviewTabId: CHANGES_PREVIEW_TAB_ID,
    } as never)
  })

  it('opens a file tab once and focuses the existing tab on repeat opens', () => {
    const first = useUIStore.getState().openPreviewTab('src/main.go')
    const second = useUIStore.getState().openPreviewTab('src/main.go')

    expect(first.created).toBe(true)
    expect(second.created).toBe(false)
    expect(useUIStore.getState().changesPanelOpen).toBe(true)
    expect(useUIStore.getState().previewTabs.filter((tab) => tab.kind === 'file')).toHaveLength(1)
    expect(useUIStore.getState().activePreviewTabId).toBe('file:src/main.go')
  })

  it('opens a git diff tab once and keeps fixed tabs after close', () => {
    const opened = useUIStore.getState().openGitDiffTab('src/main.go')

    expect(opened.created).toBe(true)
    expect(useUIStore.getState().previewTabs.filter((tab) => tab.kind === 'git-diff-file')).toHaveLength(1)

    useUIStore.getState().closePreviewTab(opened.id)

    expect(useUIStore.getState().previewTabs.map((tab) => tab.id)).toEqual([CHANGES_PREVIEW_TAB_ID, GIT_DIFF_PREVIEW_TAB_ID])
    expect(useUIStore.getState().activePreviewTabId).toBe(GIT_DIFF_PREVIEW_TAB_ID)
  })

  it('stores preview payload and git diff payload independently', () => {
    const fileTab = useUIStore.getState().openPreviewTab('src/main.go')
    useUIStore.getState().setPreviewTabContent(fileTab.id, {
      path: 'src/main.go',
      content: 'package main',
      encoding: 'utf-8',
      size: 12,
    })

    const gitTab = useUIStore.getState().openGitDiffTab('src/main.go')
    useUIStore.getState().setGitDiffTabContent(gitTab.id, {
      path: 'src/main.go',
      status: 'modified',
      original_content: 'old',
      modified_content: 'new',
      size_original: 3,
      size_modified: 3,
    })

    const storedFileTab = useUIStore.getState().previewTabs.find((tab) => tab.id === fileTab.id)
    const storedGitTab = useUIStore.getState().previewTabs.find((tab) => tab.id === gitTab.id)
    expect(storedFileTab && storedFileTab.kind === 'file' ? storedFileTab.content : '').toBe('package main')
    expect(storedGitTab && storedGitTab.kind === 'git-diff-file' ? storedGitTab.original_content : '').toBe('old')
    expect(storedGitTab && storedGitTab.kind === 'git-diff-file' ? storedGitTab.modified_content : '').toBe('new')
  })

  it('bumps git diff refresh token and resets preview tabs', () => {
    useUIStore.getState().refreshGitDiff()
    expect(useUIStore.getState().gitDiffRefreshToken).toBe(1)

    useUIStore.getState().resetPreviewTabs()
    expect(useUIStore.getState().previewTabs.map((tab) => tab.id)).toEqual([CHANGES_PREVIEW_TAB_ID, GIT_DIFF_PREVIEW_TAB_ID])
    expect(useUIStore.getState().gitDiffRefreshToken).toBe(0)
  })
})
