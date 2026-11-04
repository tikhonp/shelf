import { expect, test } from 'vitest'
import type { LibraryTrack, Suggestion, Suggestions, TagKey } from '../api/types'
import { accept, addValue, changes, fieldView, fix, isLong, keyTarget, pastedRelease, revert, saveBody, stageEdit, suggestionsFor, tagKey, tagRows, type Stage } from './tags'

function track(id: number, tags: Record<string, string[]>, standard: TagKey[] = []): LibraryTrack {
  return {
    id, path: `a/${id}.flac`, disc: 1, track: id, title: `T${id}`, artist: 'A', duration: 60, quality: 'CD', format: 'flac', size: 1,
    revision: `r${id}`, error: '', issues: [], extended: [], pictures: [],
    standard: [...Object.entries(tags).map(([k, values]) => ({ key: k.toUpperCase(), fields: [k], values })), ...standard],
  }
}
const sug = (over: Partial<Suggestion>): Suggestion => ({ field: 'genre', source: 'Qobuz', sources: ['Qobuz'], confidence: 70, ...over })
const answer = (album: Suggestion[], tracks: Suggestions['tracks'] = []): Suggestions => ({ anchor: null, album, tracks, covers: [], notes: [], unavailable: [] })

test('a field whose tracks differ is mixed, and a staged edit keeps what it was', () => {
  const ts = [track(1, { genre: ['Rock'] }), track(2, { genre: ['Jazz'] })]
  expect(fieldView(ts, 'genre', {})).toMatchObject({ mixed: true, values: [], staged: false, was: null })
  const v = fieldView(ts, 'genre', stageEdit({}, ts, 'genre', ['Pop']))
  expect(v).toMatchObject({ mixed: false, values: ['Pop'], staged: true, was: { mixed: true } })
})

test('a track that already holds the value stages nothing', () => {
  const ts = [track(1, { genre: ['Rock'] }), track(2, { genre: ['Jazz'] })]
  expect(Object.keys(stageEdit({}, ts, 'genre', ['Rock']))).toEqual(['2'])
  expect(stageEdit({}, [ts[0]!], 'genre', ['Rock'])).toEqual({})
})

test('retyping the old value drops the edit', () => {
  const t = track(1, { album: ['X'] })
  const s = stageEdit({}, [t], 'album', ['Y'])
  expect(stageEdit(s, [t], 'album', ['X'])).toEqual({})
})

test('revert drops one field and leaves the others', () => {
  const ts = [track(1, { album: ['X'], genre: ['Rock'] })]
  const s = stageEdit(stageEdit({}, ts, 'album', ['Y']), ts, 'genre', ['Jazz'])
  expect(changes(s)).toBe(2)
  expect(Object.keys(revert(s, ts, 'album')[1]!)).toEqual(['genre'])
})

test('changes counts a field once however many tracks it reaches', () => {
  const ts = [track(1, {}), track(2, {}), track(3, {})]
  expect(changes(stageEdit({}, ts, 'album', ['Y']))).toBe(1)
})

test('a multi-value suggestion adds to each track and loses nothing', () => {
  const ts = [track(1, { genre: ['Rock'] }), track(2, { genre: ['Jazz'] }), track(3, { genre: ['Pop'] })]
  const s = accept({}, ts, ts, sug({ values: ['Pop'] }), true)
  expect(s[1]!.genre!.values).toEqual(['Rock', 'Pop'])
  expect(s[2]!.genre!.values).toEqual(['Jazz', 'Pop'])
  expect(s[3]).toBeUndefined()
  expect(addValue(s, ts, 'genre', 'Pop')).toEqual(s)
})

test('a fill gives every track its own value even with one selected', () => {
  const ts = [track(1, {}), track(2, {})]
  const s = accept({}, ts, [ts[0]!], sug({ field: 'musicbrainz_trackid', tracks: { 1: ['a'], 2: ['b'] }, anchor: 'rel' }), false)
  expect(s[1]!.musicbrainz_trackid).toEqual({ values: ['a'], anchor: 'rel' })
  expect(s[2]!.musicbrainz_trackid).toEqual({ values: ['b'], anchor: 'rel' })
})

test('a Save sends fields, raw keys and anchors, and only the tracks with edits', () => {
  const ts = [track(1, {}), track(2, {})]
  const s: Stage = { 1: { genre: { values: ['X'], anchor: 'rel' }, [keyTarget('MOOD')]: { values: [] } } }
  expect(saveBody(ts, s, ['split'])).toEqual({
    confirm: ['split'],
    tracks: [{ id: 1, revision: 'r1', edits: [{ field: 'genre', values: ['X'], anchor: 'rel' }, { key: 'MOOD', values: [] }] }],
  })
})

test('a cover edit names every track, with no edits on those without any', () => {
  const body = saveBody([track(1, {}), track(2, {})], {}, [], { upload: 'h' })
  expect(body.tracks).toEqual([{ id: 1, revision: 'r1', edits: [] }, { id: 2, revision: 'r2', edits: [] }])
  expect(body.cover).toEqual({ upload: 'h' })
})

test('a selected track sees its own suggestions first, then the album', () => {
  const a = answer([sug({ values: ['Album'] })], [{ id: 1, suggestions: [sug({ values: ['Own'] })] }])
  expect(suggestionsFor(a, 'genre', 1).map((s) => s.values![0])).toEqual(['Own', 'Album'])
  expect(suggestionsFor(a, 'genre', null).map((s) => s.values![0])).toEqual(['Album'])
  expect(suggestionsFor(null, 'genre', null)).toEqual([])
})

test('the RELEASEDATE fix writes the pair under the legacy rule', () => {
  const t = track(1, { date: ['2006'], originaldate: ['1958'] })
  const s = fix({ kind: 'no_releasedate' }, [t], null, {})!
  expect(s[1]).toEqual({ 'key:RELEASEDATE': { values: ['2006'] }, 'key:DATE': { values: ['1958'] } })
})

test('the RELEASEDATE fix copies a key that already feeds the release date', () => {
  const t = track(1, { date: ['2006'], originaldate: ['1958'] }, [{ key: 'YEAR', fields: ['releasedate'], values: ['1999'] }])
  expect(fix({ kind: 'no_releasedate' }, [t], null, {})![1]).toEqual({ 'key:RELEASEDATE': { values: ['1999'] } })
})

test('the RELEASEDATE fix copies DATE when nothing else applies, and skips a track without one', () => {
  const ts = [track(1, { date: ['2006'] }), track(2, {})]
  const s = fix({ kind: 'no_releasedate' }, ts, null, {})!
  expect(s[1]).toEqual({ 'key:RELEASEDATE': { values: ['2006'] } })
  expect(s[2]).toBeUndefined()
})

test('a Fix for differing values prefers a suggestion, else what most tracks hold', () => {
  const ts = [track(1, { albumartist: ['A'] }), track(2, { albumartist: ['A'] }), track(3, { albumartist: ['B'] })]
  const issue = { kind: 'values_differ', fields: ['albumartist'] }
  expect(fix(issue, ts, null, {})![3]!.albumartist!.values).toEqual(['A'])
  const s = fix(issue, ts, answer([sug({ field: 'albumartist', values: ['C'], anchor: 'rel' })]), {})!
  expect(s[1]!.albumartist).toEqual({ values: ['C'], anchor: 'rel' })
})

test('Various Artists sets the flag, and issues with no rule or no suggestion have no Fix', () => {
  const ts = [track(1, {})]
  expect(fix({ kind: 'various_artists' }, ts, null, {})![1]!.compilation!.values).toEqual(['1'])
  expect(fix({ kind: 'reissue_year' }, ts, null, {})).toBeNull()
  expect(fix({ kind: 'no_cover' }, ts, null, {})).toBeNull()
  expect(fix({ kind: 'missing_tag', fields: ['title'] }, ts, null, {})).toBeNull()
})

test('a Fix for disagreeing tags rewrites the field even when the file holds the value', () => {
  const ts = [track(1, { albumartist: ['A'] })]
  expect(fix({ kind: 'tags_disagree', fields: ['albumartist'] }, ts, null, {})![1]!.albumartist).toEqual({ values: ['A'] })
})

test('a Fix that stages a raw key shows on the field it feeds, and Revert takes it back', () => {
  const t = track(1, { date: ['2006'], originaldate: ['1958'] })
  const s = fix({ kind: 'no_releasedate' }, [t], null, {})!
  expect(fieldView([t], 'releasedate', s)).toMatchObject({ values: ['2006'], staged: true })
  expect(revert(s, [t], 'releasedate')[1]).toEqual({ 'key:DATE': { values: ['1958'] } })
})

test('All fields lists each key once, and a row and a main field edit one tag', () => {
  const ts = [track(1, { genre: ['Rock'] }), track(2, { genre: ['Jazz'], mood: ['Calm'] })]
  ts[0]!.extended = [{ key: 'UPC', values: ['1'] }]
  const rows = tagRows(ts, {}, [])
  expect(rows.map((r) => r.key)).toEqual(['GENRE', 'MOOD', 'UPC'])
  expect(rows[0]).toMatchObject({ group: 'standard', target: 'genre', multi: true })
  expect(rows[2]).toMatchObject({ group: 'extended', target: 'key:UPC', custom: false })
  expect(changes(stageEdit(stageEdit({}, ts, 'genre', ['Pop']), ts, rows[0]!.target, ['Soul']))).toBe(1)
})

test('a second key that feeds a field keeps its own row', () => {
  const t = track(1, { albumartist: ['A'] }, [{ key: 'ALBUM ARTIST', fields: ['albumartist'], values: ['B'] }])
  expect(tagRows([t], {}, []).map((r) => r.target)).toEqual(['albumartist', 'key:ALBUM ARTIST'])
})

test('a new key goes under Standard when Navidrome reads it, else under Extended', () => {
  const t = track(1, {})
  const s = stageEdit(stageEdit({}, [t], keyTarget('BPM'), ['120']), [t], keyTarget('STUDIO'), ['Fjord'])
  expect(tagRows([t], s, ['BPM']).map((r) => [r.key, r.group, r.custom])).toEqual([['BPM', 'standard', true], ['STUDIO', 'extended', true]])
})

test('a tag of one track is marked at All fields, and a typed key is cleaned', () => {
  const t = track(1, { title: ['T'], lyrics: ['L'] }, [{ key: 'ISRC', values: ['X'] }])
  expect(tagRows([t], {}, []).filter((r) => r.perTrack).map((r) => r.key)).toEqual(['TITLE', 'LYRICS', 'ISRC'])
  expect(tagKey(' studio ')).toBe('STUDIO')
})

test('a release ID is found in an ID or a link, and other text is none', () => {
  const id = '8d2338a8-5e0e-4e3b-9f3e-6b1e4b8f6c11'
  expect(pastedRelease(id)).toBe(id)
  expect(pastedRelease(`https://musicbrainz.org/release/${id.toUpperCase()}`)).toBe(id)
  expect(pastedRelease('8d2338a8')).toBeNull()
})

test('a value over 100 characters is long', () => {
  expect(isLong(['x'.repeat(100)])).toBe(false)
  expect(isLong(['short', 'x'.repeat(101)])).toBe(true)
})
