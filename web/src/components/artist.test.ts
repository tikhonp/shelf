import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import { createAppRouter } from '../router'
import { useSources } from '../stores/sources'
import { download, item, Reply, source, stubApi } from '../testing/api'
import ItemPanel from './ItemPanel.vue'

const artist = item('qobuz', 'artist-1', {
  ref: { source: 'qobuz', kind: 'artist', id: 'artist-1' },
  title: 'Northern Static',
  sections: [
    { id: 'albums', title: 'Albums', items: [item('qobuz', 'a'), item('qobuz', 'b', { status: { state: 'in_library' } })] },
    { id: 'eps', title: 'EPs and singles', items: [item('qobuz', 'c')] },
    { id: 'compilations', title: 'Compilations', items: [item('qobuz', 'd')] },
    { id: 'appears_on', title: 'Appears on', items: [] },
  ],
})

let pinia: ReturnType<typeof createPinia>
beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
  useSources().list = [source('qobuz', 'Qobuz')]
})
afterEach(() => vi.unstubAllGlobals())

async function open() {
  const wrapper = mount(ItemPanel, {
    props: { itemRef: artist.ref },
    global: { plugins: [pinia, createAppRouter(createMemoryHistory())] },
  })
  await flushPromises()
  return wrapper
}

const keys = (w: Awaited<ReturnType<typeof open>>, text: string) => w.findAll('button').find((b) => b.text().startsWith(text))

test('Download all asks first and counts what is not in the library', async () => {
  const calls = stubApi({ '/api/items/qobuz/artist/artist-1': artist })
  const w = await open()
  await keys(w, 'Download all')!.trigger('click')
  expect(w.text()).toContain('Download 3 releases by Northern Static?')
  expect(w.text()).toContain('IN LIBRARY, SKIPPED1')
  expect(w.text()).toContain('ALBUMS1')
  expect(w.text()).toContain('EPS AND SINGLES1')
  expect(calls.filter((c) => c.init?.method === 'POST')).toEqual([])
})

test('confirming queues the artist as one download in the chosen format', async () => {
  const calls = stubApi({
    '/api/items/qobuz/artist/artist-1': artist,
    '/api/downloads': new Reply(201, download(7, { kind: 'artist', item_id: 'artist-1', title: 'Northern Static' })),
  })
  const w = await open()
  await keys(w, 'Download all')!.trigger('click')
  await w.findAll('[role="radio"]').find((b) => b.text() === 'Hi')!.trigger('click')
  await keys(w, 'Download 3 · Hi')!.trigger('click')
  await flushPromises()
  const posts = calls.filter((c) => c.init?.method === 'POST')
  expect(posts).toHaveLength(1)
  expect(JSON.parse(posts[0]!.init!.body as string)).toMatchObject({ ref: artist.ref, format: 'qobuz:hi' })
  expect(w.text()).toContain('3 downloads queued')
})

test('Cancel closes the question and queues nothing', async () => {
  const calls = stubApi({ '/api/items/qobuz/artist/artist-1': artist })
  const w = await open()
  await keys(w, 'Download all')!.trigger('click')
  await keys(w, 'Cancel')!.trigger('click')
  expect(w.text()).not.toContain('Download 3 releases')
  expect(calls.filter((c) => c.init?.method === 'POST')).toEqual([])
})

test('every group of the discography shows, and an empty one does not', async () => {
  stubApi({ '/api/items/qobuz/artist/artist-1': artist })
  const w = await open()
  expect(w.findAll('h4').map((h) => h.text())).toEqual(['Albums', 'EPs and singles', 'Compilations'])
})

test('with the whole discography in the library there is nothing to confirm', async () => {
  const all = { ...artist, sections: [{ id: 'albums', title: 'Albums', items: [item('qobuz', 'b', { status: { state: 'in_library' as const } })] }] }
  stubApi({ '/api/items/qobuz/artist/artist-1': all })
  const w = await open()
  await keys(w, 'Download all')!.trigger('click')
  expect(keys(w, 'Download 0')!.attributes('disabled')).toBeDefined()
})
