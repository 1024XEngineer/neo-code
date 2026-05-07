/**
 * 解析 unified diff patch，保留文件级统计与 hunk 结构。
 */

export type DiffLineType = 'add' | 'del' | 'header' | 'context'

export interface DiffLine {
  type: DiffLineType
  content: string
}

export interface DiffHunk {
  header: string
  lines: DiffLine[]
  additions: number
  deletions: number
}

export interface ParsedFileDiff {
  additions: number
  deletions: number
  lines: DiffLine[]
  hunks: DiffHunk[]
}

function createParsedFileDiff(): ParsedFileDiff {
  return { additions: 0, deletions: 0, lines: [], hunks: [] }
}

function pushLine(target: ParsedFileDiff, hunk: DiffHunk | null, line: DiffLine) {
  target.lines.push(line)
  if (hunk) hunk.lines.push(line)
}

function startHunk(target: ParsedFileDiff, header: string): DiffHunk {
  const hunk: DiffHunk = {
    header,
    lines: [{ type: 'header', content: header }],
    additions: 0,
    deletions: 0,
  }
  target.hunks.push(hunk)
  target.lines.push(hunk.lines[0])
  return hunk
}

function startImplicitHunk(target: ParsedFileDiff): DiffHunk {
  const hunk: DiffHunk = {
    header: '',
    lines: [],
    additions: 0,
    deletions: 0,
  }
  target.hunks.push(hunk)
  return hunk
}

function parseDiffLine(target: ParsedFileDiff, currentHunk: DiffHunk | null, line: string): DiffHunk | null {
  if (line.startsWith('@@')) {
    return startHunk(target, line)
  }
  const hunk = currentHunk ?? startImplicitHunk(target)
  if (line.startsWith('+')) {
    const nextLine: DiffLine = { type: 'add', content: line.slice(1) }
    target.additions += 1
    hunk.additions += 1
    pushLine(target, hunk, nextLine)
    return hunk
  }
  if (line.startsWith('-')) {
    const nextLine: DiffLine = { type: 'del', content: line.slice(1) }
    target.deletions += 1
    hunk.deletions += 1
    pushLine(target, hunk, nextLine)
    return hunk
  }
  if (line.startsWith(' ')) {
    pushLine(target, hunk, { type: 'context', content: line.slice(1) })
  }
  return hunk
}

/**
 * parseSingleFileDiff 解析单文件 diff 内容，返回 additions/deletions/lines/hunks。
 * 跳过 `---` / `+++` 文件头，仅保留 hunk 头、上下文行和增删行。
 */
export function parseSingleFileDiff(diff: string): ParsedFileDiff {
  const result = createParsedFileDiff()
  if (!diff) return result

  let currentHunk: DiffHunk | null = null
  for (const rawLine of diff.split('\n')) {
    const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine
    if (line.startsWith('--- ') || line.startsWith('+++ ') || line.startsWith('\\ ')) continue
    currentHunk = parseDiffLine(result, currentHunk, line)
  }

  return result
}

function ensureParsedFile(
  result: Record<string, ParsedFileDiff>,
  currentPath: string,
): ParsedFileDiff | null {
  if (!currentPath) return null
  const existing = result[currentPath]
  if (existing) return existing
  const created = createParsedFileDiff()
  result[currentPath] = created
  return created
}

/**
 * parseUnifiedPatch 将标准 unified diff 拆为按文件索引的结构。
 * 支持 `--- a/path` / `+++ b/path` 与 `diff --git a/path b/path` 两种文件边界。
 */
export function parseUnifiedPatch(patch: string): Record<string, ParsedFileDiff> {
  const result: Record<string, ParsedFileDiff> = {}
  if (!patch) return result

  let currentPath = ''
  let current: ParsedFileDiff | null = null
  let currentHunk: DiffHunk | null = null

  for (const rawLine of patch.split('\n')) {
    const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine

    const gitMatch = line.match(/^diff --git a\/\S+ b\/(.+)$/)
    if (gitMatch) {
      currentPath = gitMatch[1]
      current = ensureParsedFile(result, currentPath)
      currentHunk = null
      continue
    }

    const fromMatch = line.match(/^--- (?:a\/)?(.+)$/)
    if (fromMatch) {
      if (fromMatch[1] !== '/dev/null') {
        currentPath = fromMatch[1]
        current = ensureParsedFile(result, currentPath)
      }
      currentHunk = null
      continue
    }

    const toMatch = line.match(/^\+\+\+ (?:b\/)?(.+)$/)
    if (toMatch) {
      if (toMatch[1] !== '/dev/null') {
        currentPath = toMatch[1]
        current = ensureParsedFile(result, currentPath)
      }
      currentHunk = null
      continue
    }

    if (line.startsWith('\\ ')) continue
    if (!current) continue

    currentHunk = parseDiffLine(current, currentHunk, line)
  }

  return result
}
