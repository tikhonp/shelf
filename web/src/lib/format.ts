import type { DownloadStatus } from '../api/types'

const pad = (n: number) => String(n).padStart(2, '0')

// duration renders seconds as the design's lengths: 4:12, 48:12, 1:02:03.
export function duration(seconds?: number): string {
  if (!seconds || seconds < 0) return ''
  const s = Math.round(seconds)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return h > 0 ? `${h}:${pad(m)}:${pad(s % 60)}` : `${m}:${pad(s % 60)}`
}

// elapsed is a display's running time, always two digits: 02:41.
export function elapsed(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return h > 0 ? `${pad(h)}:${pad(m)}:${pad(s % 60)}` : `${pad(m)}:${pad(s % 60)}`
}

// took is how long a finished download ran: 2s, 1m 48s, 1h 02m.
export function took(from: string | null, to: string | null): string {
  if (!from || !to) return '—'
  const s = Math.max(0, Math.round((Date.parse(to) - Date.parse(from)) / 1000))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${pad(s % 60)}s`
  return `${Math.floor(s / 3600)}h ${pad(Math.floor((s % 3600) / 60))}m`
}

// size renders bytes as the design does: 612 MB, 1.9 GB. Nothing is a dash.
export function size(bytes: number): string {
  if (!bytes || bytes <= 0) return '—'
  if (bytes < 1e6) return `${Math.max(1, Math.round(bytes / 1e3))} KB`
  if (bytes < 1e9) return `${Math.round(bytes / 1e6)} MB`
  return `${(bytes / 1e9).toFixed(1)} GB`
}

export function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`
}

// ago is a past time in words: "3 minutes ago". now is in milliseconds.
export function ago(time: string, now: number): string {
  const s = Math.floor((now - Date.parse(time)) / 1000)
  if (s < 60) return 'just now'
  if (s < 3600) return `${plural(Math.floor(s / 60), 'minute')} ago`
  if (s < 86400) return `${plural(Math.floor(s / 3600), 'hour')} ago`
  return `${plural(Math.floor(s / 86400), 'day')} ago`
}

export function clock(time: Date | string | number): string {
  const d = new Date(time)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function clockSeconds(time: string | null): string {
  if (!time) return '—'
  const d = new Date(time)
  return `${clock(d)}:${pad(d.getSeconds())}`
}

const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

// finishedAt is the history's time column: the clock today, the date before.
export function finishedAt(time: string | null, now: number): string {
  if (!time) return '—'
  const d = new Date(time)
  if (d.toDateString() === new Date(now).toDateString()) return clock(d)
  return `${d.getDate()} ${months[d.getMonth()]}`
}

const longMonths = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']

// releaseDate reads 2019-03-15 as 15 March 2019, without moving it across a time zone.
export function releaseDate(date?: string): string {
  const m = date?.match(/^(\d{4})-(\d{2})-(\d{2})/)
  if (!m) return date ?? ''
  return `${Number(m[3])} ${longMonths[Number(m[2]) - 1]} ${m[1]}`
}

const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

// longDate is a moment in full, local time: "Wednesday 11 November 2026, 12:31".
export function longDate(time: string): string {
  const d = new Date(time)
  if (Number.isNaN(d.getTime())) return ''
  return `${weekdays[d.getDay()]} ${d.getDate()} ${longMonths[d.getMonth()]} ${d.getFullYear()}, ${clock(d)}`
}

const results: Record<DownloadStatus, string> = {
  queued: 'Queued',
  running: 'Downloading',
  done: 'Done',
  partial: 'Done with errors',
  failed: 'Failed',
  canceled: 'Canceled',
  skipped: 'Skipped',
}

export function resultWord(status: DownloadStatus): string {
  return results[status]
}

// originWord is who asked: Web, or the Telegram user's name when the bot knows it.
export function originWord(origin: string, user = ''): string {
  if (origin === 'web') return 'Web'
  if (origin === 'telegram') return user ? `@${user}` : 'Telegram'
  if (origin === 'cli') return 'Shell'
  return origin
}

const kinds: Record<string, string> = { album: 'Album', track: 'Track', playlist: 'Playlist', artist: 'Artist', label: 'Label' }

export function kindWord(kind: string): string {
  return kinds[kind] ?? kind
}

// count says "1 track" or "12 tracks".
export function count(n: number, word: string): string {
  return plural(n, word)
}

// shortDay is a date as "4 Nov", for when a passkey or a connection was added.
export function shortDay(time: string): string {
  return new Date(time).toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })
}
