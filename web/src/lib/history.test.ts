import { expect, test } from 'vitest'
import type { ChangeFile } from '../api/types'
import { historyLines } from './history'

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
