import type { TodoSnapshot, TodoViewItem } from '@/api/protocol'
import type { TodoHistoryEntry } from '@/stores/useRuntimeInsightStore'

export type TodoViewRow = TodoViewItem & { isStale: boolean }

/**
 * 把当前 snapshot 转成 TodoStrip 要渲染的当前任务行列表。
 *
 * 规则:
 * - 只展示当前 snapshot.items, 保持原序;
 * - history 仅作为历史轨迹保留, 不混入当前任务进度。
 */
export function deriveTodoView(
  snapshot: TodoSnapshot | null,
  _history: Record<string, TodoHistoryEntry>,
  _cap: number = 0,
): TodoViewRow[] {
  const activeItems = snapshot?.items ?? []
  void _history
  void _cap
  return activeItems.map((item) => ({ ...item, isStale: false }))
}
