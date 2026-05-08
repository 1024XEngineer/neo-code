import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import FileTreePanel from './FileTreePanel'
import { useUIStore } from '@/stores/useUIStore'
import { useWorkspaceStore } from '@/stores/useWorkspaceStore'

let mockGatewayAPI: any = null

vi.mock('@/context/RuntimeProvider', () => ({
  useGatewayAPI: () => mockGatewayAPI,
}))

describe('FileTreePanel', () => {
  beforeEach(() => {
    useUIStore.setState({
      changesPanelOpen: false,
      previewTabs: [
        {
          id: 'changes',
          kind: 'changes',
          title: '文件变更',
          closable: false,
        },
      ],
      activePreviewTabId: 'changes',
      toggleFileTreePanel: vi.fn(),
    } as never)
    useWorkspaceStore.setState({
      currentWorkspaceHash: 'ws-1',
      workspaces: [{ hash: 'ws-1', name: 'demo', path: 'D:/demo', createdAt: '', updatedAt: '' }],
    } as never)
  })

  it('expands directories without reading file preview content', async () => {
    mockGatewayAPI = {
      listFiles: vi.fn().mockImplementation(async ({ path = '' }: { path?: string }) => {
        if (!path) {
          return {
            payload: {
              files: [{ name: 'src', path: 'src', is_dir: true }],
            },
          }
        }
        return {
          payload: {
            files: [{ name: 'main.go', path: 'src/main.go', is_dir: false }],
          },
        }
      }),
      readFile: vi.fn(),
    }

    render(<FileTreePanel />)

    await waitFor(() => {
      expect(screen.getByText('src')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('src'))

    await waitFor(() => {
      expect(screen.getByText('main.go')).toBeTruthy()
    })
    expect(mockGatewayAPI.readFile).not.toHaveBeenCalled()
  })

  it('opens a file in the preview dock and reuses a single tab per path', async () => {
    mockGatewayAPI = {
      listFiles: vi.fn().mockResolvedValue({
        payload: {
          files: [{ name: 'main.go', path: 'main.go', is_dir: false }],
        },
      }),
      readFile: vi.fn().mockResolvedValue({
        payload: {
          path: 'main.go',
          content: 'package main',
          encoding: 'utf-8',
          size: 12,
        },
      }),
    }

    render(<FileTreePanel />)

    await waitFor(() => {
      expect(screen.getByText('main.go')).toBeTruthy()
    })

    fireEvent.click(screen.getByText('main.go'))

    await waitFor(() => {
      expect(mockGatewayAPI.readFile).toHaveBeenCalledWith({ path: 'main.go' })
    })
    expect(useUIStore.getState().changesPanelOpen).toBe(true)
    expect(useUIStore.getState().previewTabs.filter((tab) => tab.kind === 'file')).toHaveLength(1)

    fireEvent.click(screen.getByText('main.go'))

    await waitFor(() => {
      expect(useUIStore.getState().previewTabs.filter((tab) => tab.kind === 'file')).toHaveLength(1)
    })
  })

  it('keeps the tree list as the inner scroll container', async () => {
    mockGatewayAPI = {
      listFiles: vi.fn().mockResolvedValue({
        payload: {
          files: [{ name: 'main.go', path: 'main.go', is_dir: false }],
        },
      }),
      readFile: vi.fn(),
    }

    render(<FileTreePanel />)

    await waitFor(() => {
      expect(screen.getByText('main.go')).toBeTruthy()
    })

    const scrollArea = screen.getByTestId('file-tree-scroll-area')
    expect(scrollArea).toHaveStyle({ overflowY: 'auto', minHeight: '0px', flex: '1 1 0%' })
  })
})
