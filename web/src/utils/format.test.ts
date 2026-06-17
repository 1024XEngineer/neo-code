import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatBytes, formatSessionTime, parseDateTime, relativeTime } from './format'

describe('relativeTime', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-05-09T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns empty string for empty input', () => {
    expect(relativeTime('')).toBe('')
  })

  it('returns the original value for invalid timestamps', () => {
    expect(relativeTime('not-a-time')).toBe('not-a-time')
  })

  it('returns 刚刚 for times within one minute', () => {
    expect(relativeTime('2026-05-09T11:59:30Z')).toBe('刚刚')
  })

  it('returns minute, hour, day, and month suffixes for older timestamps', () => {
    expect(relativeTime('2026-05-09T11:55:00Z')).toBe('5m')
    expect(relativeTime('2026-05-09T09:00:00Z')).toBe('3h')
    expect(relativeTime('2026-05-06T12:00:00Z')).toBe('3d')
    expect(relativeTime('2026-03-05T12:00:00Z')).toBe('2mo')
  })

  it('treats no-timezone date strings as UTC to avoid timezone drift', () => {
    expect(parseDateTime('2026-05-09 11:59:30').toISOString()).toBe('2026-05-09T11:59:30.000Z')
  })
})

describe('formatSessionTime', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-05-09T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns HH:mm for same-day timestamps', () => {
    expect(formatSessionTime('2026-05-09T11:59:30Z')).toMatch(/^\d{2}:\d{2}$/)
  })

  it('returns date + HH:mm for non-today timestamps', () => {
    expect(formatSessionTime('2026-05-08T11:59:30Z')).toMatch(/^\d{2}\/\d{2} \d{2}:\d{2}$/)
  })

  it('returns original text for invalid input', () => {
    expect(formatSessionTime('not-a-time')).toBe('not-a-time')
  })
})

describe('formatBytes', () => {
  it('returns 0 B for zero or undefined', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(undefined)).toBe('0 B')
    expect(formatBytes(-1)).toBe('0 B')
  })

  it('formats bytes below 1024 as B', () => {
    expect(formatBytes(1)).toBe('1 B')
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1023)).toBe('1023 B')
  })

  it('formats bytes between 1 KiB and 1 MiB as KB', () => {
    expect(formatBytes(1024)).toBe('1.0 KB')
    expect(formatBytes(2048)).toBe('2.0 KB')
  })

  it('formats bytes above 1 MiB as MB', () => {
    expect(formatBytes(1024 * 1024)).toBe('1.0 MB')
    expect(formatBytes(256 * 1024)).toBe('256.0 KB')
  })
})
