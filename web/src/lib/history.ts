import type { ChangeFile, LibraryChange } from '../api/types'
import { count, size } from './format'

export interface HistoryLine {
  track: string
  key: string
  before: string
  after: string
}

const name = (path: string) => path.split('/').pop() ?? path
// A run reaches many albums, and each has its 01.flac.
const deepName = (path: string) => path.split('/').slice(-2).join('/')
const shown = (values: string[]) => values.join(', ')

// historyLines turns a change's files into the before and after table. A key
// that changed the same way in every audio file is one All tracks line.
export function historyLines(files: ChangeFile[], run = false): HistoryLine[] {
  const label = run ? deepName : name
  const audio = files.filter((f) => f.kind === 'audio')
  const groups = new Map<string, { key: string; before: string; after: string; paths: string[] }>()
  for (const f of audio) {
    for (const k of f.keys) {
      const before = shown(k.before)
      const after = shown(k.after)
      const id = JSON.stringify([k.key, before, after])
      const g = groups.get(id) ?? { key: k.key, before, after, paths: [] }
      g.paths.push(f.path)
      groups.set(id, g)
    }
  }
  const lines: HistoryLine[] = []
  for (const g of groups.values()) {
    if (g.paths.length === audio.length && audio.length > 1) lines.push({ track: 'All tracks', key: g.key, before: g.before, after: g.after })
    else for (const p of g.paths) lines.push({ track: label(p), key: g.key, before: g.before, after: g.after })
  }
  if (audio.some((f) => f.cover_before !== f.cover_after) || files.some((f) => f.kind === 'cover')) {
    lines.push({ track: 'All tracks', key: 'Cover', before: '', after: 'Changed' })
  }
  return lines
}

export interface DeletedLine {
  label: string
  value: string
}

const navidromeWords: Record<string, string> = {
  waiting: 'Waiting for its scan',
  purges: 'Forgets missing files by itself',
  none: "Its scan marked no track missing. Remove the tracks on Navidrome's Missing Files page.",
  failed: "Didn't answer. Remove the tracks on Navidrome's Missing Files page.",
  off: 'Not set up in shelf',
}

// deletedLines says what a delete removed, for its History row.
export function deletedLines(c: LibraryChange): DeletedLine[] {
  const d = c.deleted
  if (!d) return []
  const lines: DeletedLine[] = [
    { label: 'Folder', value: c.folder },
    { label: 'Files', value: `${count(d.files, 'file')}, ${size(d.bytes)}` },
  ]
  if (d.albums.length > 0) lines.push({ label: 'Also', value: d.albums.map((a) => `${a.title} by ${a.artist}`).join('; ') })
  if (d.forgot > 0) lines.push({ label: 'Downloads', value: d.forgot === 1 ? '1 item downloads again' : `${d.forgot} items download again` })
  const nav = d.navidrome === 'removed' ? `Forgot ${count(d.navidrome_tracks, 'track')}` : navidromeWords[d.navidrome]
  if (nav) lines.push({ label: 'Navidrome', value: nav })
  return lines
}
