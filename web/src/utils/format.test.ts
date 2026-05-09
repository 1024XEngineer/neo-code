import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { relativeTime } from './format'

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
})
