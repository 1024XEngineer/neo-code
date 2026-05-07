import { beforeEach, describe, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import FileChangePanel from './FileChangePanel'
import { useUIStore } from '@/stores/useUIStore'

describe('FileChangePanel', () => {
  beforeEach(() => {
    cleanup()
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
            {
              header: '@@ -10,3 +10,3 @@',
              additions: 1,
              deletions: 1,
              lines: [
                { type: 'header', content: '@@ -10,3 +10,3 @@' },
                { type: 'context', content: 'line 10' },
                { type: 'del', content: 'line 11 old' },
                { type: 'add', content: 'line 11 new' },
              ],
            },
          ],
          diff: [
            { type: 'header', content: '@@ -1,3 +1,3 @@' },
            { type: 'context', content: 'line 1' },
          ],
        },
      ],
      changesPanelOpen: true,
    } as any)
  })

  it('renders separate hunk blocks and keeps accept as a UI-only review marker', () => {
    render(<FileChangePanel />)

    fireEvent.click(screen.getByText('src/a.txt'))

    expect(screen.getByText('接受')).toBeTruthy()
    expect(screen.queryByText('拒绝')).toBeNull()
    expect(screen.getAllByTestId(/diff-hunk-fc-1-/)).toHaveLength(2)
    expect(screen.getByText('line 1')).toBeTruthy()
    expect(screen.getByText('line 10')).toBeTruthy()
    expect(screen.getByText('line 2 new')).toBeTruthy()
    expect(screen.getByText('line 11 old')).toBeTruthy()

    fireEvent.click(screen.getByText('接受'))

    expect(useUIStore.getState().fileChanges[0]?.status).toBe('accepted')
  })

  it('renders all added lines for an expanded +6 -0 hunk without relying on inner scrolling', () => {
    useUIStore.setState({
      fileChanges: [
        {
          id: 'fc-2',
          path: 'src/b.txt',
          status: 'modified',
          additions: 6,
          deletions: 0,
          hunks: [
            {
              header: '@@ -3,0 +4,6 @@',
              additions: 6,
              deletions: 0,
              lines: [
                { type: 'header', content: '@@ -3,0 +4,6 @@' },
                { type: 'add', content: 'line add 1' },
                { type: 'add', content: 'line add 2' },
                { type: 'add', content: 'line add 3' },
                { type: 'add', content: 'line add 4' },
                { type: 'add', content: 'line add 5' },
                { type: 'add', content: 'line add 6' },
              ],
            },
          ],
        },
      ],
      changesPanelOpen: true,
    } as any)

    render(<FileChangePanel />)

    fireEvent.click(screen.getByText('src/b.txt'))

    for (let i = 1; i <= 6; i += 1) {
      expect(screen.getByText(`line add ${i}`)).toBeTruthy()
    }

    const diffScroller = screen.getByTestId('diff-scroller-fc-2') as HTMLDivElement
    expect(diffScroller.style.maxHeight).toBe('')
    expect(diffScroller.style.overflowY).toBe('visible')
  })
})
