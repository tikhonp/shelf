import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import { createAppRouter } from '../router'
import { download, FakeEventSource, list, source, stubApi } from '../testing/api'
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
    '/api/library/filters': { albums: 0, issues: 0, no_mbid: 0, kinds: [], folders: [], sources: [], no_source: 0 },
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
