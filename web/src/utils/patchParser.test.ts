import { describe, expect, it } from 'vitest'
import { parseSingleFileDiff, parseUnifiedPatch } from './patchParser'

describe('patchParser', () => {
  it('parses multiple hunks with context lines for a single file diff', () => {
    const parsed = parseSingleFileDiff(
      [
        '--- a/src/a.txt',
        '+++ b/src/a.txt',
        '@@ -1,3 +1,3 @@',
        ' line 1',
        '-line 2 old',
        '+line 2 new',
        ' line 3',
        '@@ -10,3 +10,4 @@',
        ' line 10',
        '-line 11 old',
        '+line 11 new',
        '+line 12 added',
      ].join('\n'),
    )

    expect(parsed.additions).toBe(3)
    expect(parsed.deletions).toBe(2)
    expect(parsed.hunks).toHaveLength(2)
    expect(parsed.hunks[0]?.header).toBe('@@ -1,3 +1,3 @@')
    expect(parsed.hunks[0]?.lines.map((line) => line.type)).toEqual(['header', 'context', 'del', 'add', 'context'])
    expect(parsed.hunks[1]?.lines.map((line) => line.content)).toEqual([
      '@@ -10,3 +10,4 @@',
      'line 10',
      'line 11 old',
      'line 11 new',
      'line 12 added',
    ])
  })

  it('parses unified patch for modified, added, and deleted files', () => {
    const parsed = parseUnifiedPatch(
      [
        'diff --git a/src/a.txt b/src/a.txt',
        '--- a/src/a.txt',
        '+++ b/src/a.txt',
        '@@ -1 +1 @@',
        '-before',
        '+after',
        'diff --git a/src/new.txt b/src/new.txt',
        '--- /dev/null',
        '+++ b/src/new.txt',
        '@@ -0,0 +1,2 @@',
        '+new 1',
        '+new 2',
        'diff --git a/src/old.txt b/src/old.txt',
        '--- a/src/old.txt',
        '+++ /dev/null',
        '@@ -1,2 +0,0 @@',
        '-old 1',
        '-old 2',
      ].join('\n'),
    )

    expect(Object.keys(parsed)).toEqual(['src/a.txt', 'src/new.txt', 'src/old.txt'])
    expect(parsed['src/a.txt']?.hunks).toHaveLength(1)
    expect(parsed['src/a.txt']?.additions).toBe(1)
    expect(parsed['src/a.txt']?.deletions).toBe(1)
    expect(parsed['src/new.txt']?.hunks[0]?.lines.map((line) => line.type)).toEqual(['header', 'add', 'add'])
    expect(parsed['src/old.txt']?.hunks[0]?.lines.map((line) => line.type)).toEqual(['header', 'del', 'del'])
  })

  it('falls back to an implicit hunk when diff has no @@ header', () => {
    const parsed = parseSingleFileDiff(
      [
        '--- a/src/a.txt',
        '+++ b/src/a.txt',
        '-before',
        '+after',
      ].join('\n'),
    )

    expect(parsed.additions).toBe(1)
    expect(parsed.deletions).toBe(1)
    expect(parsed.hunks).toHaveLength(1)
    expect(parsed.hunks[0]?.header).toBe('')
    expect(parsed.hunks[0]?.lines.map((line) => line.type)).toEqual(['del', 'add'])
    expect(parsed.lines.map((line) => line.content)).toEqual(['before', 'after'])
  })

  it('parses unified patch without @@ headers by creating implicit hunks per file', () => {
    const parsed = parseUnifiedPatch(
      [
        'diff --git a/src/a.txt b/src/a.txt',
        '--- a/src/a.txt',
        '+++ b/src/a.txt',
        '-before',
        '+after',
        'diff --git a/src/b.txt b/src/b.txt',
        '--- /dev/null',
        '+++ b/src/b.txt',
        '+new file',
      ].join('\n'),
    )

    expect(parsed['src/a.txt']?.hunks).toHaveLength(1)
    expect(parsed['src/a.txt']?.hunks[0]?.lines.map((line) => line.content)).toEqual(['before', 'after'])
    expect(parsed['src/b.txt']?.hunks).toHaveLength(1)
    expect(parsed['src/b.txt']?.hunks[0]?.lines.map((line) => line.type)).toEqual(['add'])
  })
})
