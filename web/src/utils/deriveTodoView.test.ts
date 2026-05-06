import { describe, it, expect } from 'vitest'
import { deriveTodoView } from './deriveTodoView'
import type { TodoHistoryEntry } from '@/stores/useRuntimeInsightStore'
import type { TodoViewItem } from '@/api/protocol'

function active(id: string, status = 'pending', content = id): TodoViewItem {
  return { id, content, status, required: true, revision: 1 }
}

function historyEntry(id: string, lastSeenAt: number, status = 'completed'): TodoHistoryEntry {
  return {
    id,
    content: id,
    status,
    required: true,
    revision: 1,
    lastSeenAt,
    firstSeenAt: lastSeenAt - 1000,
  }
}

describe('deriveTodoView', () => {
  it('returns empty list when snapshot is null and history is empty', () => {
    expect(deriveTodoView(null, {})).toEqual([])
  })

  it('shows active items in original order with isStale=false', () => {
    const snapshot = { items: [active('a'), active('b'), active('c')] }
    const rows = deriveTodoView(snapshot, {})
    expect(rows.map((r) => r.id)).toEqual(['a', 'b', 'c'])
    expect(rows.every((r) => r.isStale === false)).toBe(true)
  })

  it('does not mix stale history into current todo rows', () => {
    const snapshot = { items: [active('a'), active('b'), active('c')] }
    const history: Record<string, TodoHistoryEntry> = {
      a: { ...historyEntry('a', 100), id: 'a' },
      old1: historyEntry('old1', 50),
      old2: historyEntry('old2', 80),
      old3: historyEntry('old3', 30),
    }
    const rows = deriveTodoView(snapshot, history)
    expect(rows.map((r) => r.id)).toEqual(['a', 'b', 'c'])
    expect(rows.every((r) => r.isStale === false)).toBe(true)
  })

  it('hides all stale entries when activeCount >= cap', () => {
    const snapshot = {
      items: [active('a'), active('b'), active('c'), active('d'), active('e'), active('f'), active('g')],
    }
    const history: Record<string, TodoHistoryEntry> = {
      old1: historyEntry('old1', 100),
      old2: historyEntry('old2', 200),
    }
    const rows = deriveTodoView(snapshot, history)
    expect(rows.length).toBe(7)
    expect(rows.every((r) => !r.isStale)).toBe(true)
  })

  it('ignores history when active items are fewer than cap', () => {
    const snapshot = { items: [active('a')] }
    const history: Record<string, TodoHistoryEntry> = {
      old1: historyEntry('old1', 100),
    }
    const rows = deriveTodoView(snapshot, history)
    expect(rows.length).toBe(1)
    expect(rows[0].id).toBe('a')
  })

  it('does not duplicate ids that exist both in snapshot and history', () => {
    const snapshot = { items: [active('a', 'in_progress')] }
    const history: Record<string, TodoHistoryEntry> = {
      a: historyEntry('a', 200, 'pending'),
      old1: historyEntry('old1', 100),
    }
    const rows = deriveTodoView(snapshot, history)
    expect(rows.length).toBe(1)
    expect(rows.filter((r) => r.id === 'a').length).toBe(1)
    expect(rows.find((r) => r.id === 'a')!.isStale).toBe(false)
    expect(rows.find((r) => r.id === 'a')!.status).toBe('in_progress')
  })

  it('returns empty rows when active is empty even if history exists', () => {
    const snapshot = { items: [] }
    const history: Record<string, TodoHistoryEntry> = {
      h1: historyEntry('h1', 10),
      h2: historyEntry('h2', 20),
      h3: historyEntry('h3', 30),
      h4: historyEntry('h4', 40),
      h5: historyEntry('h5', 50),
      h6: historyEntry('h6', 60),
      h7: historyEntry('h7', 70),
    }
    const rows = deriveTodoView(snapshot, history)
    expect(rows).toEqual([])
  })
})
