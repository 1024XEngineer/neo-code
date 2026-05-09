import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { formatSessionTime, parseDateTime, relativeTime } from './format'

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
