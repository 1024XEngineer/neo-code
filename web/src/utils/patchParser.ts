/**
 * 解析 unified diff patch 字符串，按文件路径分组返回 diff 行。
 */

export interface ParsedFileDiff {
  additions: number
  deletions: number
  lines: { type: 'add' | 'del' | 'header'; content: string }[]
}

/**
 * parseSingleFileDiff 解析单文件 diff 内容，返回 additions/deletions/lines。
 * 跳过 --- / +++ 文件头行，只处理 @@ hunk 头和 +/- 内容行。
 */
export function parseSingleFileDiff(diff: string): ParsedFileDiff {
  const result: ParsedFileDiff = { additions: 0, deletions: 0, lines: [] }
  if (!diff) return result

  for (const rawLine of diff.split('\n')) {
    const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine

    // 跳过文件头行
    if (line.startsWith('--- ') || line.startsWith('+++ ')) continue

    // hunk header
    if (line.startsWith('@@')) {
      result.lines.push({ type: 'header', content: line })
      continue
    }

    // 上下文行 — 跳过
    if (line.startsWith(' ')) continue

    // 新增行
    if (line.startsWith('+')) {
      result.additions++
      result.lines.push({ type: 'add', content: line.slice(1) })
      continue
    }

    // 删除行
    if (line.startsWith('-')) {
      result.deletions++
      result.lines.push({ type: 'del', content: line.slice(1) })
    }
  }

  return result
}

/**
 * parseUnifiedPatch 将标准 unified diff 拆为按文件索引的结构。
 * 支持 `--- a/path` / `+++ b/path` 或 `diff --git a/path b/path` 两种分隔方式。
 */
export function parseUnifiedPatch(patch: string): Record<string, ParsedFileDiff> {
  const result: Record<string, ParsedFileDiff> = {}
  if (!patch) return result

  const lines = patch.split('\n')
  let currentPath = ''
  let current: ParsedFileDiff | null = null

  for (const rawLine of lines) {
    const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine

    // 文件边界：--- a/path 或 --- path (go-difflib 不带 a/ 前缀)
    const fromMatch = line.match(/^--- (?:a\/)?(.+)$/)
    if (fromMatch && fromMatch[1] !== '/dev/null') {
      currentPath = fromMatch[1]
      current = { additions: 0, deletions: 0, lines: [] }
      result[currentPath] = current
      continue
    }

    // 文件边界：+++ b/path 或 +++ path
    const toMatch = line.match(/^\+\+\+ (?:b\/)?(.+)$/)
    if (toMatch && toMatch[1] !== '/dev/null') {
      if (!current) {
        currentPath = toMatch[1]
        current = { additions: 0, deletions: 0, lines: [] }
        result[currentPath] = current
      }
      continue
    }

    // 文件边界：diff --git a/path b/path
    const gitMatch = line.match(/^diff --git a\/\S+ b\/(.+)$/)
    if (gitMatch) {
      currentPath = gitMatch[1]
      current = result[currentPath] ?? { additions: 0, deletions: 0, lines: [] }
      result[currentPath] = current
      continue
    }

    if (!current) continue

    // hunk header
    if (line.startsWith('@@')) {
      current.lines.push({ type: 'header', content: line })
      continue
    }

    // 纯上下文行 — 跳过
    if (line.startsWith(' ')) continue

    // 新增行
    if (line.startsWith('+')) {
      current.additions++
      current.lines.push({ type: 'add', content: line.slice(1) })
      continue
    }

    // 删除行
    if (line.startsWith('-')) {
      current.deletions++
      current.lines.push({ type: 'del', content: line.slice(1) })
    }
  }

  return result
}
