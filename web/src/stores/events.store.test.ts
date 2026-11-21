import { flushPromises } from '@vue/test-utils'
import { useAuth } from './auth'
import { useEditor } from './editor'
import { useLibrary } from './library'
import { useHistory } from './history'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { download, FakeEventSource, list, source, stubApi } from '../testing/api'
import { useDownloads } from './downloads'
import { useEvents } from './events'
import { useInbox } from './inbox'
import { useSources } from './sources'
import { useStatus } from './status'

const routes = {
  '/api/downloads': list([download(1)]),
  '/api/sources': [source('applemusic', 'Apple Music')],
  '/api/library/scan': { enabled: true, ok: true, message: '', address: 'x', latency_ms: 3, scanning: false, count: 0, last_scan: '' },
  '/api/telegram': { enabled: false, connected: false, username: '', users: 0 },
  '/api/applemusic/wrapper': { state: 'running', storefront: 'ru', since: '', error: '', code_deadline: null },
}

beforeEach(() => {
  setActivePinia(createPinia())
  FakeEventSource.all = []
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => {
  useEvents().disconnect()
  vi.unstubAllGlobals()
})

const stream = () => FakeEventSource.all[0]!

test('the first connection reads nothing, and a reconnect reloads the lists', async () => {
  const calls = stubApi(routes)
  useEvents().connect()
  stream().onopen?.()
  await Promise.resolve()
  expect(calls).toEqual([])

  stream().onerror?.()
  expect(useEvents().connected).toBe(false)
  stream().onopen?.()
  await vi.waitFor(() => expect(useDownloads().items[1]?.title).toBe('Album 1'))
  const urls = calls.map((c) => c.url)
  expect(urls.some((u) => u.startsWith('/api/downloads?status=queued'))).toBe(true)
  expect(urls).toContain('/api/sources')
  expect(urls).toContain('/api/library/scan')
  expect(useEvents().connected).toBe(true)
})

test('a check on a fresh page reads the wrapper once the sources say it is on', async () => {
  const calls = stubApi(routes)
  await useStatus().check()
  expect(calls.map((c) => c.url)).toContain('/api/applemusic/wrapper')
  expect(useStatus().wrapper?.state).toBe('running')
})

test('a check leaves the wrapper alone while Apple Music is not set up', async () => {
  const calls = stubApi({ ...routes, '/api/sources': [source('applemusic', 'Apple Music', { enabled: false })] })
  await useStatus().check()
  expect(calls.map((c) => c.url)).not.toContain('/api/applemusic/wrapper')
})

test('one stream serves the whole page', () => {
  useEvents().connect()
  useEvents().connect()
  expect(FakeEventSource.all).toHaveLength(1)
  expect(stream().url).toBe('/api/events')
})

test('a page that goes away closes its stream, and one that comes back opens it', () => {
  useEvents().connect()
  window.dispatchEvent(new Event('pagehide'))
  expect(stream().closed).toBe(true)
  window.dispatchEvent(Object.assign(new Event('pageshow'), { persisted: true }))
  expect(FakeEventSource.all).toHaveLength(2)
})

test('events reach their stores', () => {
  const sources = useSources()
  const status = useStatus()
  sources.list = [source('applemusic', 'Apple Music')]
  status.scan = { ...routes['/api/library/scan'] }
  useEvents().connect()
  stream().emit('scan', { scanning: true, count: 7, last_scan: '' })
  expect(status.scan).toMatchObject({ scanning: true, count: 7, ok: true })
  const wrapper = { state: 'signed_out', storefront: '', since: '', error: '', code_deadline: null }
  stream().emit('source', { id: 'applemusic', ok: false, message: 'Signed out', wrapper })
  expect(sources.byId('applemusic')?.health).toMatchObject({ ok: false, message: 'Signed out' })
  expect(status.wrapper).toEqual(wrapper)
})


test('unlocking the same account refreshes missed data and keeps staged edits', async () => {
  const signed = { provider: 'simple' as const, account: { id: 17, username: 'Alice' }, setup_needed: false }
  const album = { id: 7, title: 'Old title', tracks: [{ id: 1, standard: [{ key: 'ALBUM', fields: ['album'], values: ['Old title'] }], error: '', extended: [] }] }
  stubApi({ ...routes, '/api/auth': signed, '/api/library/albums/7': album, '/api/library/albums/7/suggestions': { album: [], tracks: [], covers: [], notes: [], unavailable: [] } })
  const auth = useAuth(); await auth.check()
  const editor = useEditor(); await editor.open(7); editor.edit('album', ['My staged title'])
  const staged = JSON.parse(JSON.stringify(editor.stage))
  useEvents().connect(); stream().onopen?.()
  auth.expired(); useEvents().disconnect()
  expect(useEvents().connected).toBe(false)
  const calls = stubApi({ ...routes, '/api/auth': signed,
    '/api/downloads': list([download(1, { status: 'done', title: 'Finished while locked' })]),
    '/api/library/index': { ready: true, indexing: false, albums: 1, tracks: 1 },
    '/api/library/albums': { albums: [{ id: 7, title: 'Updated title' }], total: 1 },
    '/api/library/albums/7': { ...album, title: 'Updated title' },
    '/api/library/filters': { albums: 1 }, '/api/library/history': { changes: [{ id: 45, author: 'Bob' }], next: null },
    '/api/library/runs/current': { run: null },
  })
  auth.signedIn(signed); useEvents().connect(); FakeEventSource.all.at(-1)!.onopen?.()
  await flushPromises()
  expect(useDownloads().items[1]?.title).toBe('Finished while locked')
  expect(useLibrary().albums[0]?.title).toBe('Updated title')
  expect(useHistory().changes[0]?.id).toBe(45)
  expect(editor.album?.title).toBe('Updated title')
  expect(editor.stage).toEqual(staged)
  expect(calls.every((c) => !c.init?.method || c.init.method === 'GET')).toBe(true)
})


test('a reconnect preserves the result of a Save that finished while disconnected', async () => {
  const album = { id: 7, title: 'Old title', tracks: [{ id: 1, revision: 'r1', standard: [{ key: 'ALBUM', fields: ['album'], values: ['Old title'] }], error: '', extended: [] }] }
  const queued = { id: 45, kind: 'save', album_id: 7, state: 'queued', tags: 1, cover: false, conflicts: [] }
  stubApi({ ...routes, '/api/library/albums/7': album, '/api/library/albums/7/save': queued,
    '/api/library/albums/7/suggestions': { album: [], tracks: [], covers: [], notes: [], unavailable: [] },
  })
  const editor = useEditor(); await editor.open(7); editor.edit('album', ['Saved title']); await editor.save()
  useEvents().connect(); stream().onopen?.(); useEvents().disconnect()
  const calls = stubApi({ ...routes, '/api/library/changes/45': { ...queued, state: 'done', album_id: 9 },
    '/api/library/albums/9': { ...album, id: 9, title: 'Saved title' },
    '/api/library/albums/9/suggestions': { album: [], tracks: [], covers: [], notes: [], unavailable: [] },
    '/api/library/albums': { albums: [], total: 0 }, '/api/library/history': { changes: [], next: null },
    '/api/library/runs/current': { run: null },
  })
  useEvents().connect(); FakeEventSource.all.at(-1)!.onopen?.(); await flushPromises()
  expect(editor.outcome).toMatchObject({ ok: true, changes: 1, tracks: 1 })
  expect(editor.album?.id).toBe(9)
  expect(editor.staged).toBe(0)
  expect(calls.some((c) => c.url === '/api/library/albums/7')).toBe(false)
  // A later reconnect also keeps the result that was already on screen.
  useEvents().disconnect(); useEvents().connect(); FakeEventSource.all.at(-1)!.onopen?.(); await flushPromises()
  expect(editor.outcome).toMatchObject({ ok: true, changes: 1 })
})

test('a completed Library change event for the open proposal album refreshes the open proposal', async () => {
  let detailCalls = 0
  const proposal = {
    id: 4, url: '/inbox/4', state: 'pending', changes_requested: false, connection: { id: 1, name: 'Agent' }, run_id: 1,
    album: { id: 7, current_id: 7, title: 'Blue', artist: 'Joni', folder: 'rock/Blue' }, group: '', follow_up_of: null, head: 1, revisions: [],
    current: { title: 'Genre', reason: 'Update', uncertainties: '', confidence: null, evidence: [], base: [{ track_id: 1, revision: 'r1' }], ops: [], preview: { album: { id: 7, folder: 'rock/Blue', title: 'Blue', artist: 'Joni' }, files: [], folder: [], warnings: [], digest: 'd' } },
    ops: [], summary: { pending: 0, accepted: 0, declined: 0, superseded: 0 }, acceptances: [], events: [],
  }
  stubApi({
    ...routes,
    '/api/inbox/proposals/4': () => { detailCalls++; return proposal },
  })
  const inbox = useInbox()
  await inbox.open(4)
  expect(detailCalls).toBe(1)

  useEvents().connect()
  // Unrelated album or unfinished change does not refresh the proposal.
  stream().emit('change', { id: 10, kind: 'save', album_id: 7, folder: 'rock/Blue', state: 'writing', conflicts: [] })
  stream().emit('change', { id: 11, kind: 'save', album_id: 99, folder: 'jazz/Other', state: 'done', conflicts: [] })
  await flushPromises()
  expect(detailCalls).toBe(1)

  // Completed change on the open proposal's album triggers a refresh.
  stream().emit('change', { id: 12, kind: 'save', album_id: 7, folder: 'rock/Blue', state: 'done', conflicts: [] })
  await flushPromises()
  expect(detailCalls).toBe(2)
})
