import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import { createAppRouter } from '../router'
import { download, FakeEventSource, list, Reply, source, stubApi } from '../testing/api'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import { useStatus } from '../stores/status'
import { useSettings } from '../stores/settings'
import { useTheme } from '../stores/theme'

const scan = { enabled: true, ok: true, message: '', address: 'x', latency_ms: 3, scanning: false, count: 0, last_scan: '' }

function routes(over: Record<string, unknown> = {}) {
  return {
    '/api/downloads': list([]),
    '/api/sources': [source('qobuz', 'Qobuz')],
    '/api/library/scan': scan,
    '/api/telegram': { enabled: false, connected: false, username: '', users: 0 },
    ...over,
  }
}

beforeEach(() => {
  localStorage.clear()
  FakeEventSource.all = []
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => vi.unstubAllGlobals())

async function mountAt(path: string) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  return { wrapper, router }
}

const tab = (w: Awaited<ReturnType<typeof mountAt>>['wrapper'], name: string) => w.findAll('nav a').find((a) => a.text().startsWith(name))!

test('/ opens the Library and unknown paths too, and Library is the first tab', async () => {
  stubApi(routes({
    '/api/library/index': { indexing: false, ready: true, albums: 0, tracks: 0, read: 0, error: '' },
    '/api/library/filters': { albums: 0, issues: 0, no_mbid: 0, kinds: [], folders: [], sources: [], no_source: 0, types: [] },
    '/api/library/albums': { albums: [], total: 0, limit: 100, offset: 0 },
  }))
  const { wrapper, router } = await mountAt('/')
  expect(router.currentRoute.value.path).toBe('/library')
  expect(wrapper.findAll('nav a').map((a) => a.text())[0]).toMatch(/^Library/)
  expect(wrapper.find('a.brand').attributes('href')).toBe('/')
  expect((await mountAt('/nowhere')).router.currentRoute.value.path).toBe('/library')
})

test('the Downloads lamp lights only while a download runs or waits', async () => {
  stubApi(routes())
  const { wrapper } = await mountAt('/search')
  expect(tab(wrapper, 'Downloads').find('.led').exists()).toBe(false)
  useDownloads().items[1] = download(1, { status: 'running' })
  await flushPromises()
  expect(tab(wrapper, 'Downloads').find('.led').exists()).toBe(true)
  useDownloads().items[1] = download(1, { status: 'queued' })
  await flushPromises()
  expect(tab(wrapper, 'Downloads').find('.led').exists()).toBe(true)
  useDownloads().items[1] = download(1, { status: 'failed' })
  await flushPromises()
  expect(tab(wrapper, 'Downloads').find('.led').exists()).toBe(false)
})

test('a tab never shows a number', async () => {
  stubApi(routes({ '/api/downloads': list([download(1), download(2)]) }))
  const { wrapper } = await mountAt('/search')
  expect(tab(wrapper, 'Downloads').text()).not.toMatch(/\d/)
})

test('the Status lamp lights only while a part needs attention', async () => {
  stubApi(routes())
  const { wrapper } = await mountAt('/search')
  expect(tab(wrapper, 'Status').find('.led').exists()).toBe(false)

  const sources = useSources()
  sources.list[0]!.health = { ok: false, message: 'Qobuz token expired' }
  await flushPromises()
  expect(tab(wrapper, 'Status').find('.led').attributes('title')).toBe('Qobuz token expired')

  sources.list[0]!.health = { ok: true }
  useStatus().scan = { ...scan, ok: false }
  await flushPromises()
  expect(tab(wrapper, 'Status').find('.led').attributes('title')).toBe("Navidrome doesn't answer")

  useStatus().scan = scan
  await flushPromises()
  expect(tab(wrapper, 'Status').find('.led').exists()).toBe(false)
})

test('a source that was never set up needs no attention while another is ready', async () => {
  stubApi(routes({ '/api/sources': [source('qobuz', 'Qobuz'), source('applemusic', 'Apple Music', { enabled: false, health: { ok: false } })] }))
  const { wrapper } = await mountAt('/search')
  expect(tab(wrapper, 'Status').find('.led').exists()).toBe(false)
})

test('with no source ready, Status asks for attention', async () => {
  stubApi(routes({ '/api/sources': [source('qobuz', 'Qobuz', { enabled: false, health: { ok: false } })] }))
  const { wrapper } = await mountAt('/search')
  expect(tab(wrapper, 'Status').find('.led').attributes('title')).toBe('No source is ready')
})

test('Night is the default and the switch is remembered', async () => {
  stubApi(routes())
  const { wrapper } = await mountAt('/search')
  expect(wrapper.find('.rs').classes()).not.toContain('day')
  await wrapper.find('button[aria-label="Switch to Day"]').trigger('click')
  expect(wrapper.find('.rs').classes()).toContain('day')
  await flushPromises()
  expect(localStorage.getItem('shelf.theme')).toBe('day')
  setActivePinia(createPinia())
  expect(useTheme().day).toBe(true)
})

const phoneWindow = () =>
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))

test('Compact sets the root class on the desktop, and Status offers the switch', async () => {
  stubApi(routes())
  localStorage.setItem('shelf.density', 'compact')
  const { wrapper } = await mountAt('/status')
  expect(wrapper.find('.rs').classes()).toContain('compact')
  expect(wrapper.find('[aria-label="Density"]').exists()).toBe(true)
  useSettings().density = 'comfortable'
  await flushPromises()
  expect(wrapper.find('.rs').classes()).not.toContain('compact')
})

test('the phone ignores the density and offers no switch', async () => {
  stubApi(routes())
  phoneWindow()
  localStorage.setItem('shelf.density', 'compact')
  const { wrapper } = await mountAt('/status')
  expect(wrapper.find('.rs').classes()).not.toContain('compact')
  expect(wrapper.find('[aria-label="Density"]').exists()).toBe(false)
})

test('the phone Status has the Account, Passkeys and Password units before Theme, and Shelf last', async () => {
  stubApi(routes({ '/api/auth': { provider: 'simple', account: { id: 1, username: 'alice' }, setup_needed: false }, '/api/auth/passkeys': { passkeys: [] } }))
  phoneWindow()
  const { wrapper } = await mountAt('/status')
  const labels = wrapper.findAll('section.smod').map((s) => s.attributes('aria-label'))
  expect(labels.slice(-5)).toEqual(['Account', 'Passkeys', 'Password', 'Theme', 'Shelf'])
  expect(wrapper.find('button[aria-label="Sign out of Shelf"]').exists()).toBe(true)
})

const libraryOnlyRoutes = {
  '/api/library/scan': scan,
  '/api/library/index': { indexing: false, ready: true, albums: 0, tracks: 0, read: 0, error: '' },
  '/api/library/filters': { albums: 0, issues: 0, no_mbid: 0, kinds: [], folders: [], sources: [], no_source: 0, types: [] },
  '/api/library/albums': { albums: [], total: 0, limit: 100, offset: 0 },
  '/api/library/musicbrainz': { ok: true, message: '', latency_ms: 5, checked_at: '' },
}

test('with downloads disabled, Search and Downloads hide and redirect to Library', async () => {
  stubApi(libraryOnlyRoutes)
  const { wrapper, router } = await mountAt('/library')
  expect(wrapper.findAll('nav a').map((a) => a.text())).toEqual(['Library', 'Status'])
  expect(tab(wrapper, 'Status').find('.led').exists()).toBe(false)

  await router.push('/downloads')
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/library')

  await router.push('/search?q=blue')
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/library')
})

test('with downloads disabled, Status hides sources, Telegram and Notices and skips their calls', async () => {
  const calls = stubApi(libraryOnlyRoutes)
  const { wrapper } = await mountAt('/status')
  const labels = wrapper.findAll('section.unit, section.smod').map((s) => s.attributes('aria-label'))
  expect(labels).toContain('Navidrome')
  expect(labels).toContain('Library')
  expect(labels).not.toContain('Qobuz')
  expect(labels).not.toContain('Apple Music')
  expect(labels).not.toContain('Telegram')
  expect(labels).not.toContain('Notices')
  expect(calls.some((c) => c.url.startsWith('/api/sources'))).toBe(false)
  expect(calls.some((c) => c.url.startsWith('/api/telegram'))).toBe(false)
  expect(calls.some((c) => c.url.startsWith('/api/applemusic/wrapper'))).toBe(false)
})

test('transient failure on /api/downloads keeps Downloads page on a retry view instead of redirecting', async () => {
  stubApi({ ...libraryOnlyRoutes, '/api/downloads': new Reply(500, { error: 'Database is busy' }), '/api/sources': new Reply(500, { error: 'Database is busy' }) })
  const { wrapper, router } = await mountAt('/downloads')
  expect(router.currentRoute.value.path).toBe('/downloads')
  expect(wrapper.text()).toContain('Database is busy')
  expect(wrapper.findAll('button').some((b) => b.text() === 'Retry')).toBe(true)
})

test('Status Check again retries download detection after a transient startup failure', async () => {
  stubApi({ ...libraryOnlyRoutes, '/api/downloads': new Reply(503, { error: 'Starting up' }), '/api/sources': new Reply(503, { error: 'Starting up' }) })
  const { wrapper, router } = await mountAt('/library')
  expect(wrapper.findAll('nav a').map((a) => a.text())).toEqual(['Library', 'Status'])
  expect(useDownloads().enabled).toBe(null)

  await router.push('/status')
  await flushPromises()
  expect(wrapper.findAll('nav a').map((a) => a.text())).toEqual(['Library', 'Status'])

  stubApi(routes(libraryOnlyRoutes))
  await wrapper.findAll('button').find((b) => b.text() === 'Check again')!.trigger('click')
  await flushPromises()
  expect(useDownloads().enabled).toBe(true)
  expect(wrapper.findAll('nav a').map((a) => a.text())).toEqual(['Library', 'Search', 'Downloads', 'Status'])
  expect(wrapper.findAll('section.unit, section.smod').map((s) => s.attributes('aria-label'))).toContain('Qobuz')
})

test('direct load of Downloads deduplicates detection while the probe is deferred', async () => {
  let resolveProbe!: (v: unknown) => void
  const calls = stubApi({
    ...routes(libraryOnlyRoutes),
    '/api/downloads?status=queued': () =>
      new Promise((resolve) => {
        resolveProbe = resolve
      }),
  })
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createAppRouter(createMemoryHistory())
  await router.push('/downloads')
  const wrapper = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  expect(useDownloads().detecting).toBe(true)
  expect(calls.filter((c) => c.url.startsWith('/api/downloads?status=queued'))).toHaveLength(1)

  resolveProbe(list([download(1, { status: 'running', title: 'Live' })]))
  await flushPromises()
  expect(useDownloads().enabled).toBe(true)
  expect(useDownloads().detecting).toBe(false)
  expect(wrapper.text()).toContain('Live')
})

test('reconnect 503 after downloads were off recovers on Status Check again', async () => {
  stubApi(libraryOnlyRoutes)
  const { wrapper } = await mountAt('/status')
  const stream = FakeEventSource.all.at(-1)!
  stream.onopen?.()
  expect(useDownloads().enabled).toBe(false)
  expect(wrapper.findAll('nav a').map((a) => a.text())).toEqual(['Library', 'Status'])

  // Server restarts with downloads enabled, but first reconnect probe hits 503.
  stubApi({ ...libraryOnlyRoutes, '/api/downloads': new Reply(503, { error: 'Starting up' }) })
  stream.onerror?.()
  stream.onopen?.()
  await flushPromises()
  expect(useDownloads().enabled).toBe(false)
  expect(useDownloads().detectError).toBe('Starting up')

  // Check again retries detection and restores Search, Downloads, and Status cards.
  stubApi(routes(libraryOnlyRoutes))
  await wrapper.findAll('button').find((b) => b.text() === 'Check again')!.trigger('click')
  await flushPromises()
  expect(useDownloads().enabled).toBe(true)
  expect(wrapper.findAll('nav a').map((a) => a.text())).toEqual(['Library', 'Search', 'Downloads', 'Status'])
  expect(wrapper.findAll('section.unit, section.smod').map((s) => s.attributes('aria-label'))).toContain('Qobuz')
})

test('Status Check again disables downloads UI when /api/sources answers 404', async () => {
  stubApi(routes(libraryOnlyRoutes))
  const { wrapper } = await mountAt('/status')
  expect(useDownloads().enabled).toBe(true)
  expect(wrapper.findAll('nav a').map((a) => a.text())).toEqual(['Library', 'Search', 'Downloads', 'Status'])

  stubApi(libraryOnlyRoutes)
  await wrapper.findAll('button').find((b) => b.text() === 'Check again')!.trigger('click')
  await flushPromises()
  expect(useDownloads().enabled).toBe(false)
  expect(wrapper.findAll('nav a').map((a) => a.text())).toEqual(['Library', 'Status'])
  const labels = wrapper.findAll('section.unit, section.smod').map((s) => s.attributes('aria-label'))
  expect(labels).not.toContain('Qobuz')
  expect(labels).not.toContain('Telegram')
  expect(labels).not.toContain('Notices')
})

test('Search still runs when active downloads answer 200 and history answers 500', async () => {
  const calls = stubApi({
    ...routes(libraryOnlyRoutes),
    '/api/downloads?status=queued': list([]),
    '/api/downloads?status=done': new Reply(500, { error: 'history broken' }),
    '/api/downloads?status=failed': list([]),
    '/api/search': { items: [{ ref: { source: 'qobuz', kind: 'album', id: 'a1' }, title: 'Blue', artist: 'Joni', quality: {} }], total: 1, limit: 20, offset: 0 },
  })
  const { wrapper, router } = await mountAt('/search?q=blue')
  expect(router.currentRoute.value.path).toBe('/search')
  expect(useDownloads().enabled).toBe(true)
  expect(calls.some((c) => c.url.startsWith('/api/search'))).toBe(true)
  expect(wrapper.text()).toContain('Blue')
})

test('Downloads page shows active album and history error with Retry when history answers 500', async () => {
  stubApi({
    ...routes(libraryOnlyRoutes),
    '/api/downloads?status=queued': list([download(1, { status: 'running', title: 'Live Album' })]),
    '/api/downloads?status=done': new Reply(500, { error: 'history broken' }),
    '/api/downloads?status=failed': list([]),
  })
  const { wrapper } = await mountAt('/downloads')
  expect(wrapper.text()).toContain('Live Album')
  expect(wrapper.text()).not.toContain('Nothing downloading')
  expect(wrapper.text()).toContain('history broken')

  stubApi(routes({ ...libraryOnlyRoutes, '/api/downloads': list([download(2, { status: 'done', title: 'Finished Album' })]) }))
  await wrapper.find('.dl-load-err button').trigger('click')
  await flushPromises()
  expect(wrapper.find('.dl-load-err').exists()).toBe(false)
  expect(wrapper.text()).toContain('Finished Album')
})

test('stale /api/sources 404 from an earlier Status check cannot disable a recovered session', async () => {
  let resolveOldSources!: (v: unknown) => void
  let hangNext = false
  stubApi({
    ...routes(libraryOnlyRoutes),
    '/api/sources': () => {
      if (hangNext) {
        hangNext = false
        return new Promise((r) => {
          resolveOldSources = r
        })
      }
      return [source('qobuz', 'Qobuz')]
    },
  })
  const { wrapper } = await mountAt('/status')
  const stream = FakeEventSource.all.at(-1)!
  stream.onopen?.()
  expect(useDownloads().enabled).toBe(true)

  // Start an older Status check whose /api/sources call hangs.
  hangNext = true
  const oldCheck = useStatus().check()
  await flushPromises()
  expect(hangNext).toBe(false)

  // Reconnect completes first with healthy downloads and sources.
  stream.onerror?.()
  stream.onopen?.()
  await flushPromises()
  expect(useDownloads().enabled).toBe(true)
  expect(useSources().list).toHaveLength(1)

  // Now the stale /api/sources call finishes with 404; it must not disable the recovered session.
  resolveOldSources(new Reply(404, { error: 'Not Found' }))
  await oldCheck
  await flushPromises()
  expect(useDownloads().enabled).toBe(true)
  expect(useSources().list).toHaveLength(1)
  expect(wrapper.findAll('nav a').map((a) => a.text())).toEqual(['Library', 'Search', 'Downloads', 'Status'])
})

test('stale /api/sources 200 from an earlier Status check cannot overwrite recovered sources or trigger stale wrapper reads', async () => {
  let resolveOldSources!: (v: unknown) => void
  let hangNext = false
  const calls = stubApi({
    ...routes(libraryOnlyRoutes),
    '/api/sources': () => {
      if (hangNext) {
        hangNext = false
        return new Promise((r) => {
          resolveOldSources = r
        })
      }
      return [source('qobuz', 'Qobuz', { health: { ok: true, message: 'Fresh' } })]
    },
    '/api/applemusic/wrapper': { state: 'running', storefront: 'us', since: '', error: '', code_deadline: null },
  })
  await mountAt('/status')
  const stream = FakeEventSource.all.at(-1)!
  stream.onopen?.()

  hangNext = true
  const oldCheck = useStatus().check()
  await flushPromises()

  // Reconnect completes with fresh Qobuz-only sources.
  stream.onerror?.()
  stream.onopen?.()
  await flushPromises()
  expect(useSources().list.map((s) => s.id)).toEqual(['qobuz'])

  // Stale /api/sources 200 arrives with Apple Music enabled; it must not overwrite sources or fetch the wrapper.
  resolveOldSources([source('applemusic', 'Apple Music', { enabled: true })])
  await oldCheck
  await flushPromises()
  expect(useSources().list.map((s) => s.id)).toEqual(['qobuz'])
  expect(calls.some((c) => c.url.startsWith('/api/applemusic/wrapper'))).toBe(false)
})

test('an older overlapping Status check getting 404 on /api/sources cannot disable downloads after a newer check succeeded', async () => {
  let resolveOldSources!: (v: unknown) => void
  let hangNext = false
  stubApi({
    ...routes(libraryOnlyRoutes),
    '/api/sources': () => {
      if (hangNext) {
        hangNext = false
        return new Promise((r) => {
          resolveOldSources = r
        })
      }
      return [source('qobuz', 'Qobuz')]
    },
  })
  const { wrapper } = await mountAt('/status')
  expect(useDownloads().enabled).toBe(true)

  // Two overlapping Check again runs share the same downloads generation.
  hangNext = true
  const firstCheck = useStatus().check()
  await flushPromises()
  await useStatus().check()
  expect(useDownloads().enabled).toBe(true)
  expect(useSources().list).toHaveLength(1)

  resolveOldSources(new Reply(404, { error: 'Not Found' }))
  await firstCheck
  await flushPromises()
  expect(useDownloads().enabled).toBe(true)
  expect(useSources().list).toHaveLength(1)
  expect(wrapper.findAll('nav a').map((a) => a.text())).toEqual(['Library', 'Search', 'Downloads', 'Status'])
})

test('stale Telegram 404 and wrapper 503 cannot overwrite a newer Status check', async () => {
  let resolveOldTg!: (v: unknown) => void
  let resolveOldWrap!: (v: unknown) => void
  let hangNext = false
  const okTg = { enabled: true, connected: true, username: 'shelfbot', users: 1 }
  const okWrap = { state: 'running', storefront: 'us', since: '', error: '', code_deadline: null }
  stubApi({
    ...routes(libraryOnlyRoutes),
    '/api/sources': [source('applemusic', 'Apple Music', { enabled: true })],
    '/api/telegram': () =>
      hangNext
        ? new Promise((r) => {
            resolveOldTg = r
          })
        : okTg,
    '/api/applemusic/wrapper': () =>
      hangNext
        ? new Promise((r) => {
            resolveOldWrap = r
          })
        : okWrap,
  })
  await mountAt('/status')

  hangNext = true
  const oldCheck = useStatus().check()
  await flushPromises()
  hangNext = false
  await useStatus().check()
  expect(useStatus().telegram).toEqual(okTg)
  expect(useStatus().wrapper).toEqual(okWrap)
  expect(useStatus().wrapperAnswers).toBe(true)

  resolveOldTg(new Reply(404, { error: 'Not Found' }))
  resolveOldWrap(new Reply(503, { error: 'down' }))
  await oldCheck
  await flushPromises()
  expect(useStatus().telegram).toEqual(okTg)
  expect(useStatus().wrapper).toEqual(okWrap)
  expect(useStatus().wrapperAnswers).toBe(true)
})

test('DownloadPanel keeps showing a refreshed download when a stale initial loadDetail rejects', async () => {
  let resolveInitial!: (v: unknown) => void
  let detailCalls = 0
  stubApi({
    ...routes(libraryOnlyRoutes),
    '/api/downloads': list([download(9, { status: 'done', title: 'Refreshed Album' })]),
    '/api/downloads/9': () => {
      detailCalls++
      if (detailCalls === 1) {
        return new Promise((r) => {
          resolveInitial = r
        })
      }
      return { ...download(9, { status: 'done', title: 'Refreshed Album' }), tracks: [], children: [], provenance_path: '' }
    },
  })
  const { wrapper } = await mountAt('/downloads/9')
  const stream = FakeEventSource.all.at(-1)!
  stream.onopen?.()

  // Reconnect refreshes open detail 9 cleanly before the initial request rejects.
  stream.onerror?.()
  stream.onopen?.()
  await flushPromises()
  expect(wrapper.text()).toContain('Refreshed Album')

  resolveInitial(new Reply(404, { error: 'stale not found' }))
  await flushPromises()
  expect(wrapper.text()).toContain('Refreshed Album')
  expect(wrapper.text()).not.toContain('stale not found')
})

test('DownloadPanel shows the reconnect refresh 503 error instead of staying on Loading when both requests fail', async () => {
  let resolveInitial!: (v: unknown) => void
  let detailCalls = 0
  stubApi({
    ...routes(libraryOnlyRoutes),
    '/api/downloads': list([download(9, { status: 'running', title: 'Pending Album' })]),
    '/api/downloads/9': () => {
      detailCalls++
      if (detailCalls === 1) {
        return new Promise((r) => {
          resolveInitial = r
        })
      }
      return new Reply(503, { error: 'temporarily unavailable' })
    },
  })
  const { wrapper, router } = await mountAt('/downloads')
  const stream = FakeEventSource.all.at(-1)!
  stream.onopen?.()

  await router.push('/downloads/9')
  await flushPromises()
  expect(wrapper.text()).toContain('Loading')

  // Reconnect refresh returns 503 with no cached detail, then initial request also fails.
  stream.onerror?.()
  stream.onopen?.()
  await flushPromises()
  resolveInitial(new Reply(500, { error: 'stale error' }))
  await flushPromises()
  expect(wrapper.text()).toContain('temporarily unavailable')
  expect(wrapper.text()).not.toContain('Loading')
})
