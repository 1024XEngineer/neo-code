/** 格式化时间戳为本地可读字符串 */
export function formatTime(date: Date | string): string {
  const d = typeof date === 'string' ? new Date(date) : date
  return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
}

/** 格式化日期为本地可读字符串 */
export function formatDate(date: Date | string): string {
  const d = typeof date === 'string' ? new Date(date) : date
  return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}

/** 格式化 Token 数量（带 K/M 后缀） */
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

/** 绠€鐭浉瀵规椂闂?*/
export function relativeTime(time: string): string {
	if (!time) return ''
	const d = new Date(time)
	if (isNaN(d.getTime())) return time
	const now = Date.now()
	const diff = now - d.getTime()
	const sec = Math.floor(diff / 1000)
	if (sec < 60) return '鍒氬垰'
	const min = Math.floor(sec / 60)
	if (min < 60) return `${min}m`
	const hr = Math.floor(min / 60)
	if (hr < 24) return `${hr}h`
	const day = Math.floor(hr / 24)
	if (day < 30) return `${day}d`
	const mon = Math.floor(day / 30)
	return `${mon}mo`
}
