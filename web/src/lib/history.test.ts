import { expect, test } from 'vitest'
import type { ChangeFile, LibraryChange } from '../api/types'
import { changeTags, changeTagWords, changeTitle, deletedLines, historyLines, keyWord, reversible, runNote, slotWords, stateClass, stateWord, undoSlot } from './history'

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

const ch = (over: Partial<LibraryChange> = {}) => ({ kind: 'save', state: 'done', undone: false, tags: 3, cover: false, ...over }) as LibraryChange

test('a change says its state in the app\'s words, and a queued one says Saving', () => {
  expect([ch(), ch({ state: 'queued' }), ch({ state: 'writing' }), ch({ state: 'partial' }), ch({ state: 'failed' }), ch({ undone: true })].map(stateWord)).toEqual(['Saved', 'Saving', 'Saving', 'Partial', 'Failed', 'Undone'])
  expect([ch({ kind: 'delete' }), ch({ kind: 'delete', state: 'queued' })].map(stateWord)).toEqual(['Deleted', 'Deleting'])
  expect([ch(), ch({ state: 'queued' }), ch({ undone: true })].map(stateClass)).toEqual(['done', 'run', 'canceled'])
  expect([ch(), ch({ state: 'partial' }), ch({ state: 'failed' }), ch({ kind: 'delete' })].map(reversible)).toEqual([true, true, false, false])
})

test('an album\'s History names the kind of change and counts tags and the cover', () => {
  expect(changeTitle(ch())).toBe('Save')
  expect(changeTitle(ch({ tags: 0, cover: true }))).toBe('Save: cover')
  expect(changeTitle(ch({ tags: 2, cover: true }))).toBe('Save')
  expect(changeTitle(ch({ run: { fixes: ['replaygain'], albums: 2, files: 4, skipped: 0, failed: 0 } }))).toBe('Run: ReplayGain')
  expect(changeTitle(ch({ kind: 'delete' }))).toBe('Delete')
  expect([ch(), ch({ tags: 0, cover: true }), ch({ tags: 4, cover: true }), ch({ kind: 'delete' })].map(changeTags)).toEqual(['3', 'Cover', '4 · Cover', ''])
})

const run = (albums: number) => ({ fixes: ['replaygain'], albums, files: 4, skipped: 0, failed: 0 })

test('the phone\'s History names the tags in words, as it has no Tags header', () => {
  expect([ch(), ch({ tags: 1 }), ch({ tags: 0, cover: true }), ch({ tags: 4, cover: true }), ch({ kind: 'delete' })].map(changeTagWords)).toEqual(['3 tags', '1 tag', 'Cover', '4 · Cover', ''])
})

test('a change\'s key and its note say Undo or Redo, and run for a run', () => {
  expect([ch(), ch({ undone: true }), ch({ run: run(20) }), ch({ run: run(20), undone: true })].map(keyWord)).toEqual(['Undo', 'Redo', 'Undo run', 'Redo run'])
  expect([ch(), ch({ run: run(20) }), ch({ run: run(1), undone: true })].map(runNote)).toEqual(['', 'Undoes the run on all 20 albums.', 'Redoes the run on its album.'])
})

test('the phone\'s slot holds a Save or a run, never a delete or a failed Save, and counts what it wrote', () => {
  expect([ch(), ch({ state: 'partial' }), ch({ state: 'writing' }), ch({ undone: true }), ch({ kind: 'delete' }), ch({ state: 'failed' })].map(undoSlot)).toEqual([true, true, true, true, false, false])
  const files = [1, 2].map((n) => ({ track_id: n, path: `a/${n}.flac`, kind: 'audio', written: true, revision: 'r', keys: [], cover_before: '', cover_after: '' })) as ChangeFile[]
  expect(slotWords(ch())).toBe('3 TAGS')
  expect(slotWords(ch(), files)).toBe('3 TAGS ON 2 TRACKS')
  expect(slotWords(ch({ tags: 12, cover: true }), files)).toBe('12 TAGS AND COVER')
  expect(slotWords(ch({ tags: 0, cover: true }))).toBe('COVER')
  expect(slotWords(ch({ run: run(20), tags: 44 }), files)).toBe('RUN ON 20 ALBUMS')
  expect(slotWords(ch({ state: 'queued' }))).toBe('SAVING')
  expect(slotWords(ch({ state: 'writing' }), files)).toBe('SAVING 2 FILES')
})
