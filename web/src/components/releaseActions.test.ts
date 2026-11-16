import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import type { Format, Item } from '../api/types'
import { createAppRouter } from '../router'
import { useSources } from '../stores/sources'
import { download, item, Reply, source, stubApi } from '../testing/api'
import ItemAction from './ItemAction.vue'
import ReleaseActions from './ReleaseActions.vue'

const qobuz: Format[] = [
  { id: 'qobuz:27', label: 'Max', description: 'FLAC up to 24-bit 192 kHz', default: true, bit_depth: 24, sample_rate: 192000 },
  { id: 'qobuz:7', label: '24/96', description: 'FLAC up to 24-bit 96 kHz', bit_depth: 24, sample_rate: 96000 },
  { id: 'qobuz:6', label: 'CD', description: 'FLAC 16-bit 44.1 kHz', bit_depth: 16, sample_rate: 44100 },
  { id: 'qobuz:5', label: 'MP3 320', description: 'MP3 320 kbps' },
]

let pinia: ReturnType<typeof createPinia>
beforeEach(() => {
  pinia = createPinia()
  setActivePinia(pinia)
  useSources().list = [source('qobuz', 'Qobuz', { formats: qobuz })]
})
afterEach(() => vi.unstubAllGlobals())

function mountFor(it: Item) {
  return mount(ReleaseActions, { props: { item: it }, global: { plugins: [pinia, createAppRouter(createMemoryHistory())] } })
}

const labels = (w: ReturnType<typeof mountFor>) => w.findAll('.fmt-list [role="radio"]').map((b) => b.text())

test('a CD album offers CD in place of Max and queues it', async () => {
  const calls = stubApi({ '/api/downloads': new Reply(201, download(3)) })
  const w = mountFor(item('qobuz', 'cd', { quality: { codec: 'flac', bit_depth: 16, sample_rate: 44100 } }))
  expect(labels(w)).toEqual(['CDdefault', 'MP3 320'])
  await w.find('button.sun').trigger('click')
  await flushPromises()
  const post = calls.find((c) => c.init?.method === 'POST')!
  expect(JSON.parse(post.init!.body as string)).toMatchObject({ format: 'qobuz:6' })
})

test('a 24/96 album drops Max and keeps CD', () => {
  const w = mountFor(item('qobuz', 'hi', { quality: { codec: 'flac', bit_depth: 24, sample_rate: 96000 } }))
  expect(labels(w)).toEqual(['24/96default', 'CD', 'MP3 320'])
})

test('a 24/192 album, a playlist and an album of unknown quality offer every format', () => {
  const all = ['Maxdefault', '24/96', 'CD', 'MP3 320']
  expect(labels(mountFor(item('qobuz', 'max', { quality: { codec: 'flac', bit_depth: 24, sample_rate: 192000 } })))).toEqual(all)
  const playlist = item('qobuz', 'p', { ref: { source: 'qobuz', kind: 'playlist', id: 'p' }, quality: { bit_depth: 16, sample_rate: 44100 } })
  expect(labels(mountFor(playlist))).toEqual(all)
  expect(labels(mountFor(item('qobuz', 'unknown')))).toEqual(all)
})

test('a pick the next album does not offer falls back to its default', async () => {
  const w = mountFor(item('qobuz', 'hi', { quality: { bit_depth: 24, sample_rate: 192000 } }))
  await w.findAll('.fmt-list [role="radio"]').find((b) => b.text() === '24/96')!.trigger('click')
  await w.setProps({ item: item('qobuz', 'cd', { quality: { bit_depth: 16, sample_rate: 44100 } }) })
  expect(w.find('button.sun').text()).toContain('CD')
})

test('a row of a CD album downloads in CD and its menu leaves out Max', async () => {
  const calls = stubApi({ '/api/downloads': new Reply(201, download(4)) })
  const cd = item('qobuz', 'cd')
  const w = mount(ItemAction, {
    props: { itemRef: cd.ref, title: 'CD album', quality: { bit_depth: 16, sample_rate: 44100 }, layout: 'wide' },
    global: { plugins: [pinia, createAppRouter(createMemoryHistory())] },
    attachTo: document.body,
  })
  await w.find('[aria-label="Choose format"]').trigger('click')
  expect(w.findAll('[role="menuitemradio"] .bold').map((b) => b.text())).toEqual(['CD', 'MP3 320'])
  await w.find('[aria-label="Download CD album"]').trigger('click')
  await flushPromises()
  const post = calls.find((c) => c.init?.method === 'POST')!
  expect(JSON.parse(post.init!.body as string)).toMatchObject({ format: 'qobuz:6' })
  w.unmount()
})
