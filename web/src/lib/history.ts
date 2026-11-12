import type { ChangeFile, LibraryChange } from '../api/types'
import { runTitle } from './bulk'
import { count, size } from './format'

// stateWord is a change's pill. A queued change says Saving, as one that writes does.
export function stateWord(c: LibraryChange): string {
  if (c.undone) return 'Undone'
  if (c.state === 'partial') return 'Partial'
  if (c.state === 'failed') return 'Failed'
  if (c.kind === 'delete') return c.state === 'done' ? 'Deleted' : 'Deleting'
  return c.state === 'done' ? 'Saved' : 'Saving'
}

export function stateClass(c: LibraryChange): string {
  if (c.undone) return 'canceled'
  if (c.state === 'done') return 'done'
  if (c.state === 'partial') return 'partial'
  if (c.state === 'failed') return 'failed'
  return 'run'
}

// reversible is a Save that wrote. A delete has no undo.
export const reversible = (c: LibraryChange) => c.kind !== 'delete' && (c.state === 'done' || c.state === 'partial')

// changeTitle names the kind of change, for an album's History, where the
// album's name is already in the title bar.
export function changeTitle(c: LibraryChange): string {
  if (c.run) return runTitle(c.run.fixes)
  if (c.kind === 'delete') return 'Delete'
  return c.tags === 0 && c.cover ? 'Save: cover' : 'Save'
}

// changeTags is the Tags column of an album's History: 4, Cover, or 4 · Cover.
export function changeTags(c: LibraryChange): string {
  if (c.kind === 'delete') return ''
  if (c.tags > 0 && c.cover) return `${c.tags} · Cover`
  if (c.cover) return 'Cover'
  return String(c.tags)
}

// changeTagWords is the same for the phone, which has no Tags header: 33 tags, Cover, or 4 · Cover.
export function changeTagWords(c: LibraryChange): string {
  if (c.kind === 'delete') return ''
  return c.cover ? changeTags(c) : count(c.tags, 'tag')
}

// keyWord is the key on a change that can go back: Undo, Redo, Undo run or Redo run.
export const keyWord = (c: LibraryChange) => `${c.undone ? 'Redo' : 'Undo'}${c.run ? ' run' : ''}`

// runNote says that a run's key works on every album in it.
export function runNote(c: LibraryChange): string {
  if (!c.run) return ''
  const where = c.run.albums === 1 ? 'its album' : `all ${c.run.albums} albums`
  return `${c.undone ? 'Redoes' : 'Undoes'} the run on ${where}.`
}

// undoSlot says whether an open change fills the phone's slot above the tab
// bar. A Save or a run that wrote puts its key there, and one still writing
// shows Saving.
export const undoSlot = (c: LibraryChange) => c.kind !== 'delete' && c.state !== 'failed'

// slotWords is the display in that slot. The tracks come from the opened
// change's files, so they show once its lines have loaded.
export function slotWords(c: LibraryChange, files: ChangeFile[] = []): string {
  const tracks = files.filter((f) => f.kind === 'audio').length
  if (c.state === 'queued' || c.state === 'writing') return (tracks > 0 ? `Saving ${count(tracks, 'file')}` : 'Saving').toUpperCase()
  if (c.run) return `Run on ${count(c.run.albums, 'album')}`.toUpperCase()
  if (c.tags === 0) return c.cover ? 'COVER' : count(0, 'tag').toUpperCase()
  if (c.cover) return `${count(c.tags, 'tag')} and cover`.toUpperCase()
  return (tracks > 0 ? `${count(c.tags, 'tag')} on ${count(tracks, 'track')}` : count(c.tags, 'tag')).toUpperCase()
}

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
