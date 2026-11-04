import { expect, test } from 'vitest'
import type { ChangeFile, LibraryChange } from '../api/types'
import { deletedLines, historyLines } from './history'

const file = (path: string, keys: ChangeFile['keys'], over: Partial<ChangeFile> = {}): ChangeFile => ({ track_id: 1, path, kind: 'audio', written: true, revision: 'r', keys, cover_before: '', cover_after: '', ...over })
const k = (key: string, before: string[], after: string[]) => ({ key, before, after })

test('a key changed the same way on every track is one line', () => {
  expect(historyLines([file('a/1.flac', [k('GENRE', ['Rock'], ['Pop'])]), file('a/2.flac', [k('GENRE', ['Rock'], ['Pop'])])])).toEqual([{ track: 'All tracks', key: 'GENRE', before: 'Rock', after: 'Pop' }])
})

test('a key that differs names each track', () => {
  const lines = historyLines([file('a/1.flac', [k('GENRE', ['Rock'], ['Pop'])]), file('a/2.flac', [k('GENRE', ['Jazz'], ['Pop'])])])
  expect(lines.map((l) => [l.track, l.before])).toEqual([['1.flac', 'Rock'], ['2.flac', 'Jazz']])
})

test('a cover edit is a line of its own, and a folder cover alone counts', () => {
  expect(historyLines([file('a/1.flac', [], { cover_before: 'x', cover_after: 'y' })]).map((l) => l.key)).toEqual(['Cover'])
  expect(historyLines([file('a/1.flac', []), file('a/cover.jpg', [], { kind: 'cover' })]).map((l) => l.key)).toEqual(['Cover'])
  expect(historyLines([file('a/1.flac', [])])).toEqual([])
})

test('a run names the album folder too', () => {
  const lines = historyLines([file('rock/Blue/01.flac', [k('DATE', ['1'], ['2'])]), file('rock/Hejira/01.flac', [k('DATE', ['3'], ['4'])])], true)
  expect(lines.map((l) => l.track)).toEqual(['Blue/01.flac', 'Hejira/01.flac'])
})

test('a delete says what it removed and how Navidrome took it', () => {
  const c = {
    folder: 'qobuz/Blue',
    deleted: { files: 12, bytes: 412_000_000, albums: [{ title: 'Hejira', artist: 'Joni Mitchell' }], forgot: 1, navidrome: 'removed', navidrome_tracks: 10 },
  } as unknown as LibraryChange
  expect(deletedLines(c)).toEqual([
    { label: 'Folder', value: 'qobuz/Blue' },
    { label: 'Files', value: '12 files, 412 MB' },
    { label: 'Also', value: 'Hejira by Joni Mitchell' },
    { label: 'Downloads', value: '1 item downloads again' },
    { label: 'Navidrome', value: 'Forgot 10 tracks' },
  ])
  const quiet = { folder: 'qobuz/Blue', deleted: { files: 1, bytes: 2000, albums: [], forgot: 0, navidrome: 'none', navidrome_tracks: 0 } } as unknown as LibraryChange
  expect(deletedLines(quiet).map((l) => l.label)).toEqual(['Folder', 'Files', 'Navidrome'])
  expect(deletedLines({ folder: 'x' } as LibraryChange)).toEqual([])
})
