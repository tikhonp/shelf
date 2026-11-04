import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { download, FakeEventSource, list, source, stubApi } from '../testing/api'
import { useDownloads } from './downloads'
import { useEvents } from './events'
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
