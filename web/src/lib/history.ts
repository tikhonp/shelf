import type { ChangeFile } from '../api/types'

export interface HistoryLine {
  track: string
  key: string
  before: string
  after: string
}

const name = (path: string) => path.split('/').pop() ?? path
const shown = (values: string[]) => values.join(', ')

// historyLines turns a change's files into the before and after table. A key
// that changed the same way in every audio file is one All tracks line.
export function historyLines(files: ChangeFile[]): HistoryLine[] {
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
    else for (const p of g.paths) lines.push({ track: name(p), key: g.key, before: g.before, after: g.after })
  }
  if (audio.some((f) => f.cover_before !== f.cover_after) || files.some((f) => f.kind === 'cover')) {
    lines.push({ track: 'All tracks', key: 'Cover', before: '', after: 'Changed' })
  }
  return lines
}
