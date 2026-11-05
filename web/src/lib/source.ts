import type { LibrarySource } from '../api/types'

const months = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']

function day(time?: string): string {
  if (!time) return ''
  const d = new Date(time)
  if (Number.isNaN(d.getTime())) return ''
  // No-break spaces keep the date on one line on the phone.
  return `${d.getDate()}\u00a0${months[d.getMonth()]}\u00a0${d.getFullYear()}`
}

// sourceWords says where an album came from and how shelf knows it.
export function sourceWords(s: LibrarySource): string {
  if (!s.id) return 'No known source'
  const on = day(s.at)
  const when = on ? ` on ${on}` : ''
  switch (s.by) {
    case 'download':
      return `Downloaded from ${s.name}${when}`
    case 'import':
      return `From ${s.name}, in the old bot's history`
    case 'file':
      return `Downloaded from ${s.name}${when}, says its shelf.txt`
    case 'folder':
      return `From ${s.name}, going by its folder`
    default:
      return s.name
  }
}
