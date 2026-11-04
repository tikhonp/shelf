import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { Download, DownloadEvent, DownloadStatus } from '../api/types'
import { download, list, Reply, source, stubApi } from '../testing/api'
import { useDownloads } from './downloads'
import { useSettings, type NoticeLevel } from './settings'
import { useSources } from './sources'

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

const event = (id: number, over: Partial<DownloadEvent> = {}): DownloadEvent => ({
  id,
  parent_id: null,
  status: 'running',
  tracks_done: 0,
  tracks_total: 0,
  tracks_failed: 0,
  current: '',
  bytes: 0,
  error: '',
  ...over,
})

function lists(active: ReturnType<typeof download>[], history: ReturnType<typeof download>[] = [], next: number | null = null) {
  return {
    '/api/downloads?status=queued,running': list(active),
    '/api/downloads?status=done,': list(history, next),
    '/api/downloads?status=failed': list(history.filter((d) => d.status === 'failed')),
  }
}

test('a download event updates the right row', async () => {
  stubApi(lists([download(1), download(2, { status: 'running' })]))
  const s = useDownloads()
  await s.load()
  s.applyEvent(event(2, { tracks_done: 3, tracks_total: 12, current: 'Under the Glasshouse' }))
  expect(s.items[2]).toMatchObject({ tracks_done: 3, tracks_total: 12, title: 'Album 2' })
  expect(s.items[1]).toMatchObject({ tracks_done: 0 })
  expect(s.currents[2]).toBe('Under the Glasshouse')
})

test('the lamp follows running and queued downloads', async () => {
  stubApi({ ...lists([download(1)]), '/api/downloads/1': { ...download(1, { status: 'done' }), tracks: [], children: [], provenance_path: '' } })
  const s = useDownloads()
  await s.load()
  expect(s.busy).toBe(true)
  s.applyEvent(event(1, { status: 'done' }))
  expect(s.busy).toBe(false)
  expect(s.history.map((d) => d.id)).toEqual([1])
})

test('a download from the bot appears when its first event comes', async () => {
  stubApi({ ...lists([]), '/api/downloads/9': { ...download(9, { title: 'From the bot' }), tracks: [], children: [], provenance_path: '' } })
  const s = useDownloads()
  await s.load()
  s.applyEvent(event(9, { status: 'queued' }))
  await vi.waitFor(() => expect(s.items[9]?.title).toBe('From the bot'))
})

test('an album of an artist stays out of the lists', async () => {
  const calls = stubApi(lists([]))
  const s = useDownloads()
  await s.load()
  const before = calls.length
  s.applyEvent(event(5, { parent_id: 4 }))
  expect(calls).toHaveLength(before)
  expect(s.items[5]).toBeUndefined()
})

test('the queue runs oldest first and the history newest first', async () => {
  stubApi(lists([download(3), download(2)], [download(1, { status: 'done' }), download(0, { status: 'failed' })]))
  const s = useDownloads()
  await s.load()
  expect(s.queued.map((d) => d.id)).toEqual([2, 3])
  expect(s.history.map((d) => d.id)).toEqual([1, 0])
})

test('failed downloads count for 24 hours', async () => {
  const now = Date.parse('2026-11-04T12:00:00Z')
  stubApi(
    lists(
      [],
      [
        download(2, { status: 'failed', finished_at: '2026-11-04T11:00:00Z' }),
        download(1, { status: 'failed', finished_at: '2026-11-02T11:00:00Z' }),
      ],
    ),
  )
  const s = useDownloads()
  await s.load()
  expect(s.failedSince(now)).toBe(1)
})

test('an old failure counted for the number stays out of the history', async () => {
  stubApi({
    '/api/downloads?status=queued,running': list([]),
    '/api/downloads?status=done,': list([download(50, { status: 'done' })], 50),
    '/api/downloads?status=failed': list([download(3, { status: 'failed' })]),
  })
  const s = useDownloads()
  await s.load()
  expect(s.history.map((d) => d.id)).toEqual([50])
})

test('Load more reads the next page of the history', async () => {
  stubApi({
    '/api/downloads?status=queued,running': list([]),
    '/api/downloads?status=failed': list([]),
    '/api/downloads?status=done,': (url: string) =>
      url.includes('before=50') ? list([download(49, { status: 'done' })]) : list([download(50, { status: 'done' })], 50),
  })
  const s = useDownloads()
  await s.load()
  await s.loadMore()
  expect(s.history.map((d) => d.id)).toEqual([50, 49])
  expect(s.historyNext).toBeNull()
})

test('queuing shows the slot with the format and what is ahead', async () => {
  useSources().list = [source('qobuz', 'Qobuz')]
  stubApi({ ...lists([download(1, { status: 'running' })]), '/api/downloads': (_: string, i?: RequestInit) => (i?.method === 'POST' ? new Reply(201, download(2)) : list([])) })
  const s = useDownloads()
  await s.load()
  await s.queue({ source: 'qobuz', kind: 'album', id: 'album-2' })
  expect(s.notice).toMatchObject({ title: 'Queued', text: 'Album 2', sub: 'Album · Max · 1 ahead of it' })
})

test('a skipped download says it is in the library', async () => {
  stubApi({ '/api/downloads': new Reply(201, download(2, { status: 'skipped' })) })
  const s = useDownloads()
  await s.queue({ source: 'qobuz', kind: 'album', id: 'album-2' })
  expect(s.notice).toMatchObject({ title: 'Already in the library', lamp: '' })
  expect(s.deskNotice).toBeNull()
})

test('a refused download says why', async () => {
  stubApi({ '/api/downloads': new Reply(422, { error: 'unknown or disabled source' }) })
  const s = useDownloads()
  expect(await s.queue({ source: 'x', kind: 'album', id: '1' }, { title: 'Album' })).toBeNull()
  expect(s.notice).toMatchObject({ lamp: 'bad', to: null, sub: 'unknown or disabled source' })
  expect(s.deskNotice).toEqual(s.notice)
})

test('what this page saw of a download beats what the search answered', async () => {
  stubApi(lists([download(1, { item_id: 'a', status: 'running', tracks_done: 1, tracks_total: 4 })], [download(0, { item_id: 'b', status: 'canceled' })]))
  const s = useDownloads()
  await s.load()
  const ref = (id: string) => ({ source: 'qobuz', kind: 'album' as const, id })
  expect(s.liveStatus(ref('a'), { state: 'queued' })).toMatchObject({ state: 'running', tracks_done: 1, tracks_total: 4 })
  expect(s.liveStatus(ref('b'), { state: 'queued' })).toBeUndefined()
  expect(s.liveStatus(ref('b'), { state: 'in_library' })).toEqual({ state: 'in_library' })
  expect(s.liveStatus(ref('c'), { state: 'failed' })).toEqual({ state: 'failed' })
})

test('a finished download reads as in the library', async () => {
  stubApi(lists([], [download(1, { item_id: 'a', status: 'done' }), download(2, { item_id: 'b', status: 'skipped' })]))
  const s = useDownloads()
  await s.load()
  expect(s.liveStatus({ source: 'qobuz', kind: 'album', id: 'a' })?.state).toBe('in_library')
  expect(s.liveStatus({ source: 'qobuz', kind: 'album', id: 'b' })?.state).toBe('in_library')
})

test('an open download reads its tracks again, at most once a second', async () => {
  vi.useFakeTimers()
  const detail = { ...download(1, { status: 'running' }), tracks: [], children: [], provenance_path: '' }
  const calls = stubApi({ ...lists([download(1, { status: 'running' })]), '/api/downloads/1': detail })
  const s = useDownloads()
  await s.load()
  await s.loadDetail(1)
  const reads = () => calls.filter((c) => c.url === '/api/downloads/1').length
  expect(reads()).toBe(1)
  s.applyEvent(event(1))
  s.applyEvent(event(1))
  s.applyEvent(event(1))
  await vi.advanceTimersByTimeAsync(1100)
  expect(reads()).toBe(2)
})

// ended loads one running download, then has it end as `status`.
async function ended(level: NoticeLevel, status: DownloadStatus, over: Partial<Download> = {}) {
  useSettings().notices = level
  const final = download(1, { status, tracks_done: 11, tracks_total: 12, error: status === 'failed' ? 'Qobuz returned 401' : '', ...over })
  stubApi({ ...lists([download(1, { status: 'running' })]), '/api/downloads/1': { ...final, tracks: [], children: [], provenance_path: '' } })
  const s = useDownloads()
  await s.load()
  s.applyEvent(event(1, { status, parent_id: final.parent_id }))
  await flushPromises()
  return s
}

test.each<[NoticeLevel, DownloadStatus, string | null]>([
  ['off', 'done', null],
  ['off', 'failed', null],
  ['off', 'partial', null],
  ['failures', 'done', null],
  ['failures', 'failed', 'Failed'],
  ['failures', 'partial', 'Done with errors'],
  ['all', 'done', 'Downloaded'],
  ['all', 'failed', 'Failed'],
  ['all', 'partial', 'Done with errors'],
  ['all', 'canceled', null],
  ['all', 'skipped', null],
])('with notices at %s, a download ending %s shows %s', async (level, status, title) => {
  const s = await ended(level, status)
  expect(s.notice?.title ?? null).toBe(title)
})

test('a notice says why and links to its download', async () => {
  expect((await ended('failures', 'failed')).notice).toMatchObject({ text: 'Album 1', sub: 'Qobuz returned 401', lamp: 'bad', to: '/downloads/1' })
})

test('a partial download counts its tracks', async () => {
  expect((await ended('failures', 'partial')).notice).toMatchObject({ sub: '11 of 12 tracks', lamp: 'warn' })
})

test('a result reaches the desktop', async () => {
  expect((await ended('all', 'done')).deskNotice).toMatchObject({ title: 'Downloaded', lamp: 'ok' })
})

test('reading lists that hold ended downloads shows no notice', async () => {
  useSettings().notices = 'all'
  stubApi(lists([], [download(1, { status: 'failed' }), download(2, { status: 'done' })]))
  const s = useDownloads()
  await s.load()
  expect(s.notice).toBeNull()
})

test('a repeated final event shows no second notice', async () => {
  const s = await ended('all', 'failed')
  s.notice = null
  s.applyEvent(event(1, { status: 'failed' }))
  await flushPromises()
  expect(s.notice).toBeNull()
})

test('a retry that ends shows a notice again', async () => {
  const s = await ended('failures', 'failed')
  s.notice = null
  s.applyEvent(event(1, { status: 'running' }))
  s.applyEvent(event(1, { status: 'failed' }))
  await flushPromises()
  expect(useDownloads().notice?.title).toBe('Failed')
})

test('a download from the bot that this page never listed still shows', async () => {
  useSettings().notices = 'failures'
  stubApi({ ...lists([]), '/api/downloads/9': { ...download(9, { status: 'failed', error: 'no' }), tracks: [], children: [], provenance_path: '' } })
  const s = useDownloads()
  await s.load()
  s.applyEvent(event(9, { status: 'failed' }))
  s.applyEvent(event(9, { status: 'failed' }))
  await flushPromises()
  expect(s.notice).toMatchObject({ title: 'Failed', text: 'Album 9' })
})

test('an album of an artist shows no notice of its own', async () => {
  useSettings().notices = 'all'
  stubApi(lists([]))
  const s = useDownloads()
  await s.load()
  s.applyEvent(event(5, { parent_id: 4, status: 'failed' }))
  await flushPromises()
  expect(s.notice).toBeNull()
})
