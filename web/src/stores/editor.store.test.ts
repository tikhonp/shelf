import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import type { DeletedFolder, LibraryAlbumDetail, LibraryChange, LibraryTrack } from '../api/types'
import { Reply, stubApi } from '../testing/api'
import { useEditor } from './editor'

function track(id: number, over: Partial<LibraryTrack> = {}): LibraryTrack {
  return {
    id, path: `a/${id}.flac`, disc: 1, track: id, title: `T${id}`, artist: 'A', duration: 60, quality: 'FLAC 16/44.1', format: 'flac', size: 1, revision: `r${id}`,
    error: '', issues: [], extended: [], pictures: [], standard: [{ key: 'ALBUM', fields: ['album'], values: ['X'] }], ...over,
  }
}
const album = (id = 7, tracks = [track(1), track(2)]): LibraryAlbumDetail => ({
  id, folder: 'a', title: 'X', artist: 'A', year: 2000, quality: 'FLAC 16/44.1', duration: 120, size: 2, added_at: '', navidrome_id: 'n', has_mbid: false, lamp: false, issues: [], source: { id: '', name: '' }, tracks, stale: false,
})
const noSuggestions = { anchor: null, album: [], tracks: [], covers: [], notes: [], unavailable: [] }
const change = (over: Partial<LibraryChange> = {}): LibraryChange => ({
  id: 1, kind: 'save', target: null, album_id: 7, folder: 'a', title: 'X', artist: 'A', state: 'queued', reason: '', error: '', undone: false, tags: 2, cover: false, conflicts: [], job_id: 1,
  created_at: '', finished_at: null, ...over,
})
const routes = (over: Record<string, unknown> = {}) => ({
  '/api/library/albums/7/suggestions': noSuggestions,
  '/api/library/albums/7/save': change(),
  '/api/library/albums/7': album(),
  '/api/library/albums': { albums: [], total: 0, limit: 100, offset: 0 },
  ...over,
})
const body = (calls: ReturnType<typeof stubApi>, path: string) => calls.filter((c) => c.url === path).map((c) => JSON.parse(c.init!.body as string))

async function opened(over: Record<string, unknown> = {}) {
  const calls = stubApi(routes(over))
  const e = useEditor()
  await e.open(7)
  await flushPromises()
  return { e, calls }
}

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

test('at All tracks an edit reaches every track, and a selected track only itself', async () => {
  const { e } = await opened()
  e.edit('album', ['Y'])
  expect(Object.keys(e.stage)).toEqual(['1', '2'])
  e.discard()
  e.trackId = 2
  e.edit('album', ['Y'])
  expect(Object.keys(e.stage)).toEqual(['2'])
  expect(e.staged).toBe(1)
})

test('a file shelf could not read is left out of what an edit reaches', async () => {
  const { e } = await opened({ '/api/library/albums/7': album(7, [track(1), track(2, { error: 'broken', revision: '' })]) })
  e.edit('album', ['Y'])
  expect(Object.keys(e.stage)).toEqual(['1'])
})

test('a Save that needs a confirm asks, and sends the kinds back', async () => {
  let n = 0
  const { e, calls } = await opened({
    '/api/library/albums/7/save': () => (n++ === 0 ? new Reply(409, { error: 'confirm', reason: 'confirm', confirm: [{ kind: 'split', message: 'Moves 1 track.' }] }) : change()),
  })
  e.edit('album', ['Y'])
  await e.save()
  expect(e.asks).toEqual([{ kind: 'split', message: 'Moves 1 track.' }])
  expect(e.saving).toBeNull()
  await e.confirmAsks()
  expect(e.asks).toBeNull()
  expect(body(calls, '/api/library/albums/7/save')[1].confirm).toEqual(['split'])
  expect(e.saving?.id).toBe(1)
})

test('Cancel on a confirm keeps the staged edits', async () => {
  const { e } = await opened({ '/api/library/albums/7/save': new Reply(409, { error: 'c', reason: 'confirm', confirm: [{ kind: 'merge', message: 'm' }] }) })
  e.edit('album', ['Y'])
  await e.save()
  e.cancelAsks()
  expect(e.asks).toBeNull()
  expect(e.staged).toBe(1)
})

test.each(['conflict', 'locked', 'busy'])('a %s refusal keeps the staged edits and says why', async (reason) => {
  const { e } = await opened({ '/api/library/albums/7/save': new Reply(409, { error: `no: ${reason}`, reason }) })
  e.edit('album', ['Y'])
  await e.save()
  expect(e.outcome).toMatchObject({ ok: false, reason, message: `no: ${reason}` })
  expect(e.staged).toBe(1)
})

test('a conflict reload takes the new revisions and keeps the edits', async () => {
  let rev = 'r1'
  const { e, calls } = await opened({ '/api/library/albums/7': () => album(7, [track(1, { revision: rev }), track(2)]) })
  e.edit('album', ['Y'])
  rev = 'new'
  await e.reload()
  await e.save()
  expect(body(calls, '/api/library/albums/7/save')[0].tracks[0].revision).toBe('new')
})

test('a finished Save clears the stage and reports what it held', async () => {
  const { e } = await opened()
  e.edit('album', ['Y'])
  await e.save()
  e.applyChange(change({ state: 'writing' }))
  expect(e.saving?.state).toBe('writing')
  e.applyChange(change({ state: 'done' }))
  await flushPromises()
  expect(e.saving).toBeNull()
  expect(e.staged).toBe(0)
  expect(e.outcome).toMatchObject({ ok: true, changes: 1, tracks: 2 })
})

test('a failed write keeps the stage, and another change is ignored', async () => {
  const { e } = await opened()
  e.edit('album', ['Y'])
  await e.save()
  e.applyChange(change({ id: 99, state: 'done' }))
  expect(e.saving?.id).toBe(1)
  e.applyChange(change({ state: 'failed', reason: 'verify', error: 'read-back differs' }))
  await flushPromises()
  expect(e.outcome).toMatchObject({ ok: false, reason: 'verify', message: 'read-back differs' })
  expect(e.staged).toBe(1)
})

test('Save cover alone keeps staged edits and takes the revisions from the change', async () => {
  const files = [{ track_id: 1, path: 'a/1.flac', kind: 'audio', written: true, revision: 'after', keys: [], cover_before: 'a', cover_after: 'b' }]
  const { e, calls } = await opened({ '/api/library/changes/1': change({ state: 'done', cover: true, files: files as LibraryChange['files'] }) })
  e.edit('album', ['Y'])
  await e.saveCoverAlone('hash')
  const sent = body(calls, '/api/library/albums/7/save')[0]
  expect(sent.cover).toEqual({ upload: 'hash' })
  expect(sent.tracks.map((t: { edits: unknown[] }) => t.edits)).toEqual([[], []])
  e.applyChange(change({ state: 'done', cover: true }))
  await flushPromises()
  expect(e.staged).toBe(1)
  expect(e.outcome).toMatchObject({ ok: true, changes: 0 })
  expect(e.tracks.map((t) => t.revision)).toEqual(['after', 'r2'])
  // Reading the album again would accept a change another tool made in between.
  expect(calls.filter((c) => c.url === '/api/library/albums/7').length).toBe(1)
})

test('after the walk the editor follows a Save that gave the album a new ID, with its staged edits', async () => {
  const { e } = await opened({ '/api/library/changes/1': change({ state: 'done', album_id: 9 }), '/api/library/albums/9': album(9) })
  e.edit('album', ['Y'])
  await e.save()
  e.applyChange(change({ state: 'done' }))
  await flushPromises()
  e.edit('album', ['Z'])
  await e.follow()
  expect(e.album?.id).toBe(9)
  expect(e.staged).toBe(1)
  await e.follow()
  expect(e.album?.id).toBe(9)
})

test('edits stay staged when another album opens and comes back', async () => {
  const { e } = await opened({ '/api/library/albums/8/suggestions': noSuggestions, '/api/library/albums/8': album(8) })
  e.edit('album', ['Y'])
  await e.open(8)
  expect(e.staged).toBe(0)
  await e.open(7)
  expect(e.staged).toBe(1)
})

test('an unanswered suggestion call leaves the editor working', async () => {
  const { e } = await opened({ '/api/library/albums/7/suggestions': new Reply(502, { error: 'bad gateway' }) })
  expect(e.suggestError).toBe('bad gateway')
  expect(e.album?.id).toBe(7)
})

test('a reconnect asks about a running Save', async () => {
  const { e } = await opened({ '/api/library/changes/1': change({ state: 'done' }) })
  e.edit('album', ['Y'])
  await e.save()
  await e.recheck()
  await flushPromises()
  expect(e.saving).toBeNull()
  expect(e.outcome?.ok).toBe(true)
})

const deleted: DeletedFolder = { files: 2, bytes: 2, albums: [], forgot: 0, navidrome: 'waiting', navidrome_tracks: 0 }

test('Delete always asks, sends the kinds back, and leaves the album once its folder is gone', async () => {
  let n = 0
  const warnings = [
    { kind: 'delete', message: 'This deletes 2 files.' },
    { kind: 'shared_folder', message: 'Another album goes too.' },
  ]
  const { e, calls } = await opened({
    '/api/library/albums/7/delete': () => (n++ === 0 ? new Reply(409, { error: 'confirm', reason: 'confirm', confirm: warnings }) : change({ id: 5, kind: 'delete', deleted })),
  })
  e.edit('album', ['Y'])
  await e.remove()
  expect(e.asks).toEqual(warnings)
  expect(e.askKey).toBe('Delete')
  expect(e.deleting).toBeNull()
  await e.confirmAsks()
  expect(body(calls, '/api/library/albums/7/delete')[1].confirm).toEqual(['delete', 'shared_folder'])
  expect(e.deleting?.id).toBe(5)
  // Another change's event leaves the delete waiting.
  e.applyChange(change({ id: 9, state: 'done' }))
  expect(e.deleting?.id).toBe(5)
  e.applyChange(change({ id: 5, kind: 'delete', state: 'done', deleted }))
  await flushPromises()
  expect(e.deleting).toBeNull()
  expect(e.gone).toBe(7)
  expect(e.outcome).toBeNull()
})

test('a Delete shelf refuses or that fails keeps the album and says why', async () => {
  const { e } = await opened({ '/api/library/albums/7/delete': new Reply(409, { error: 'folk/Blue: locked', reason: 'locked' }) })
  await e.remove(['delete'])
  expect(e.outcome).toMatchObject({ ok: false, deleting: true, reason: 'locked' })
  expect(e.gone).toBeNull()

  const again = await opened({ '/api/library/albums/7/delete': change({ id: 6, kind: 'delete', deleted }) })
  await again.e.remove(['delete'])
  again.e.applyChange(change({ id: 6, kind: 'delete', state: 'partial', error: 'folk/Blue/1.flac: permission denied', deleted }))
  await flushPromises()
  expect(again.e.outcome).toMatchObject({ ok: false, deleting: true, reason: 'partial' })
  expect(again.e.gone).toBeNull()
})
