/** 格式化时间戳为本地可读字符串 */
export function formatTime(date: Date | string): string {
  const d = typeof date === 'string' ? parseDateTime(date) : date
  if (isNaN(d.getTime())) return ''
  return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
}

/** 格式化日期为本地可读字符串 */
export function formatDate(date: Date | string): string {
  const d = typeof date === 'string' ? parseDateTime(date) : date
  if (isNaN(d.getTime())) return ''
  return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}

/** 格式化 Token 数量，带 K/M 后缀 */
export function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`
  return String(n)
}

/** 截断文本 */
export function truncate(text: string, maxLen: number): string {
  if (text.length <= maxLen) return text
  return text.slice(0, maxLen - 3) + '...'
}

/** 返回简短的相对时间文案 */
export function relativeTime(time: string): string {
  if (!time) return ''

  const d = parseDateTime(time)
  if (isNaN(d.getTime())) return time

  const now = Date.now()
  const diff = now - d.getTime()
  const sec = Math.floor(diff / 1000)
  if (sec < 60) return '刚刚'

  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m`

  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr}h`

  const day = Math.floor(hr / 24)
  if (day < 30) return `${day}d`

  const mon = Math.floor(day / 30)
  return `${mon}mo`
}

/** 解析后端时间：兼容 epoch（秒/毫秒）、ISO、以及不带时区的 UTC 字符串。 */
export function parseDateTime(raw: string): Date {
  const value = raw.trim()
  if (!value) return new Date(NaN)

  if (/^\d+$/.test(value)) {
    const numeric = Number(value)
    if (!Number.isFinite(numeric)) return new Date(NaN)
    const millis = numeric < 1_000_000_000_000 ? numeric * 1000 : numeric
    return new Date(millis)
  }

  const normalized = value.replace(' ', 'T')
  if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?$/.test(normalized)) {
    return new Date(normalized + 'Z')
  }

  const parsed = new Date(value)
  if (!isNaN(parsed.getTime())) return parsed

  return new Date(NaN)
}

/** 会话列表时间文案：当天显示时分，非当天显示月/日与时分。 */
export function formatSessionTime(time: string): string {
  if (!time) return ''

  const d = parseDateTime(time)
  if (isNaN(d.getTime())) return time

  const now = new Date()
  const sameDay = d.getFullYear() === now.getFullYear()
    && d.getMonth() === now.getMonth()
    && d.getDate() === now.getDate()

  if (sameDay) {
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
  }

  return d.toLocaleDateString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
  }) + ' ' + d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
}
