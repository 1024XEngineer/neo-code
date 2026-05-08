import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import FileChangePanel from './FileChangePanel'
import { CHANGES_PREVIEW_TAB_ID, GIT_DIFF_PREVIEW_TAB_ID, useUIStore } from '@/stores/useUIStore'

const mockGatewayAPI = {
  listGitDiffFiles: vi.fn(),
  readGitDiffFile: vi.fn(),
}

vi.mock('@/context/RuntimeProvider', () => ({
  useGatewayAPI: () => mockGatewayAPI,
}))

vi.mock('./CodePreviewEditor', () => ({
  default: ({ path, content, theme }: { path: string; content: string; theme: string }) => (
    <div data-testid="code-preview-editor" data-path={path} data-theme={theme}>
      {content}
    </div>
  ),
}))

vi.mock('./GitDiffPreviewEditor', () => ({
  default: ({
    path,
    originalContent,
    modifiedContent,
    renderSideBySide,
  }: {
    path: string
    originalContent: string
    modifiedContent: string
    renderSideBySide: boolean
  }) => (
    <div
      data-testid="git-diff-preview-editor"
      data-path={path}
      data-side-by-side={String(renderSideBySide)}
    >
      {originalContent}::{modifiedContent}
    </div>
  ),
}))

describe('FileChangePanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGatewayAPI.listGitDiffFiles.mockResolvedValue({
      payload: {
        in_git_repo: true,
        branch: 'main',
        ahead: 1,
        behind: 0,
        truncated: false,
        total_count: 1,
        files: [
          {
            path: 'src/main.go',
            status: 'modified',
          },
        ],
      },
    })
    mockGatewayAPI.readGitDiffFile.mockResolvedValue({
      payload: {
        path: 'src/main.go',
        status: 'modified',
        original_content: 'before',
        modified_content: 'after',
        size_original: 6,
        size_modified: 5,
      },
    })
    useUIStore.setState({
      fileChanges: [
        {
          id: 'fc-1',
          path: 'src/a.txt',
          status: 'modified',
          additions: 2,
          deletions: 2,
          hunks: [
            {
              header: '@@ -1,3 +1,3 @@',
              additions: 1,
              deletions: 1,
              lines: [
                { type: 'header', content: '@@ -1,3 +1,3 @@' },
                { type: 'context', content: 'line 1' },
                { type: 'del', content: 'line 2 old' },
                { type: 'add', content: 'line 2 new' },
              ],
            },
          ],
        },
      ],
      gitDiffSummary: {
        in_git_repo: true,
        branch: 'main',
        ahead: 1,
        behind: 0,
        truncated: false,
        total_count: 1,
        files: [
          {
            path: 'src/main.go',
            status: 'modified',
          },
        ],
      },
      gitDiffLoading: false,
      gitDiffError: '',
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
      changesPanelOpen: true,
      changesPanelWidth: 760,
      theme: 'dark',
    } as never)
  })

  it('renders change diff blocks and keeps accept as a UI-only review marker', () => {
    render(<FileChangePanel />)

    fireEvent.click(screen.getByText('src/a.txt'))

    expect(screen.getByText('接受')).toBeTruthy()
    expect(screen.getAllByTestId(/diff-hunk-fc-1-/)).toHaveLength(1)
    expect(screen.getByText('line 1')).toBeTruthy()
    expect(screen.getByText('line 2 new')).toBeTruthy()

    fireEvent.click(screen.getByText('接受'))

    expect(useUIStore.getState().fileChanges[0]?.status).toBe('accepted')
  })

  it('keeps both fixed tabs visible', () => {
    render(<FileChangePanel />)

    expect(screen.getByTestId(`preview-tab-${CHANGES_PREVIEW_TAB_ID}`)).toBeTruthy()
    expect(screen.getByTestId(`preview-tab-${GIT_DIFF_PREVIEW_TAB_ID}`)).toBeTruthy()
  })

  it('opens a git diff file tab from the fixed git diff view', async () => {
    render(<FileChangePanel />)

    fireEvent.click(screen.getByTestId(`preview-tab-${GIT_DIFF_PREVIEW_TAB_ID}`))
    fireEvent.click(await screen.findByTestId('git-diff-entry-src/main.go'))

    await waitFor(() => {
      expect(mockGatewayAPI.readGitDiffFile).toHaveBeenCalledWith({ path: 'src/main.go' })
    })

    const preview = await screen.findByTestId('git-diff-preview-editor')
    expect(preview).toHaveAttribute('data-path', 'src/main.go')
    expect(preview).toHaveAttribute('data-side-by-side', 'true')
    expect(preview.textContent).toContain('before::after')
  })

  it('renders the Monaco-based preview host for loaded text files', async () => {
    useUIStore.setState({
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
        {
          id: 'file:cmd/neocode/main.go',
          kind: 'file',
          title: 'main.go',
          closable: true,
          path: 'cmd/neocode/main.go',
          content: 'package main',
          loading: false,
          loaded: true,
          error: '',
          truncated: false,
          is_binary: false,
        },
      ],
      activePreviewTabId: 'file:cmd/neocode/main.go',
      theme: 'light',
    } as never)

    render(<FileChangePanel />)

    const preview = await screen.findByTestId('code-preview-editor')
    expect(preview).toHaveAttribute('data-path', 'cmd/neocode/main.go')
    expect(preview).toHaveAttribute('data-theme', 'light')
    expect(preview.textContent).toContain('package main')
  })
})
