import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { item, page, Reply, source, stubApi } from '../testing/api'
import { mergeByRank, useSearch } from './search'
import { useSources } from './sources'

const q = source('qobuz', 'Qobuz')
const a = source('applemusic', 'Apple Music')

beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
})
afterEach(() => vi.unstubAllGlobals())

function ready(...list: (typeof q)[]) {
  const s = useSources()
  s.list = list
  s.loaded = true
}
const searches = (calls: { url: string }[]) => calls.filter((c) => c.url.startsWith('/api/search')).map((c) => new URL(c.url, 'http://x').searchParams)

test('merging takes each source in turn by rank', () => {
  const [q1, q2, q3, a1] = [item('q', '1'), item('q', '2'), item('q', '3'), item('a', '1')]
  expect(mergeByRank([[q1, q2, q3], [a1]])).toEqual([q1, a1, q2, q3])
  expect(mergeByRank([])).toEqual([])
})

test('search asks every source that is on and merges by rank', async () => {
  ready(q, a)
  const calls = stubApi({
    '/api/search?source=qobuz': page([item('qobuz', 'q1'), item('qobuz', 'q2')], 5),
    '/api/search?source=applemusic': page([item('applemusic', 'a1')], 1),
  })
  const s = useSearch()
  s.query = 'northern'
  await s.submit()
  expect(searches(calls).map((p) => p.get('source'))).toEqual(['qobuz', 'applemusic'])
  expect(s.items.map((i) => i.ref.id)).toEqual(['q1', 'a1', 'q2'])
  expect(s.total).toBe(6)
  expect(s.hasMore).toBe(true)
})

test('a source switched off, or not set up, is not asked', async () => {
  ready(q, a, source('dark', 'Dark', { enabled: false }))
  const calls = stubApi({ '/api/search': page([]) })
  const s = useSearch()
  s.toggle('applemusic')
  s.query = 'x'
  await s.submit()
  expect(searches(calls).map((p) => p.get('source'))).toEqual(['qobuz'])
})

test('a source that can not search the type is not asked', async () => {
  ready(q, source('small', 'Small', { capabilities: { links: true, search: ['album'], resolve: true, download: true } }))
  const calls = stubApi({ '/api/search': page([]) })
  const s = useSearch()
  s.type = 'playlist'
  s.query = 'x'
  await s.submit()
  expect(searches(calls).map((p) => p.get('source'))).toEqual(['qobuz'])
})

test('Load more asks each source for its next page', async () => {
  ready(q, a)
  const calls = stubApi({
    '/api/search?source=qobuz': (url: string) =>
      url.includes('offset=0') ? page([item('qobuz', 'q1')], 2) : page([item('qobuz', 'q2')], 2),
    '/api/search?source=applemusic': (url: string) =>
      url.includes('offset=0') ? page([item('applemusic', 'a1')], 1) : new Reply(500, { error: 'not asked' }),
  })
  const s = useSearch()
  s.query = 'x'
  await s.submit()
  await s.loadMore()
  const more = searches(calls).slice(2)
  expect(more.map((p) => [p.get('source'), p.get('offset')])).toEqual([['qobuz', '1']])
  expect(s.items.map((i) => i.ref.id)).toEqual(['q1', 'a1', 'q2'])
  expect(s.hasMore).toBe(false)
})

test('an empty page ends a source, whatever its total said', async () => {
  ready(q)
  stubApi({ '/api/search': (url: string) => (url.includes('offset=0') ? page([item('qobuz', 'q1')], 9) : page([], 9)) })
  const s = useSearch()
  s.query = 'x'
  await s.submit()
  expect(s.hasMore).toBe(true)
  await s.loadMore()
  expect(s.hasMore).toBe(false)
})

test('a failing source shows its error and the other results stay', async () => {
  ready(q, a)
  stubApi({
    '/api/search?source=qobuz': new Reply(503, { error: 'Qobuz: token expired' }),
    '/api/search?source=applemusic': page([item('applemusic', 'a1')]),
  })
  const s = useSearch()
  s.query = 'x'
  await s.submit()
  expect(s.failed.map((r) => r.error)).toEqual(['Qobuz: token expired'])
  expect(s.items.map((i) => i.ref.id)).toEqual(['a1'])
})

test('a rate limit is told apart from other errors', async () => {
  ready(q)
  stubApi({ '/api/search': new Reply(429, { error: 'Qobuz is slowing us down, try again in a minute' }) })
  const s = useSearch()
  s.query = 'x'
  await s.submit()
  expect(s.failed[0]?.rateLimited).toBe(true)
})

test('with every source off, search sends nothing and says so', async () => {
  ready(q)
  const calls = stubApi({})
  const s = useSearch()
  s.toggle('qobuz')
  s.query = 'x'
  await s.submit()
  expect(calls).toEqual([])
  expect(s.noSources).toBe(true)
})

test('a link in the field resolves instead of searching', async () => {
  ready(q)
  const album = item('qobuz', 'q1')
  const calls = stubApi({ '/api/resolve': album })
  const s = useSearch()
  s.query = 'https://play.qobuz.com/album/q1'
  await s.submit()
  expect(calls.map((c) => c.url)).toEqual(['/api/resolve'])
  expect(calls[0]?.init?.body).toBe(JSON.stringify({ url: 'https://play.qobuz.com/album/q1' }))
  expect(s.link?.item).toEqual(album)
})

test('/search?q= with a link resolves it, as the bot opens it, once the sources are known', async () => {
  const album = item('qobuz', 'q1')
  const calls = stubApi({ '/api/sources': [q], '/api/resolve': album })
  const s = useSearch()
  await s.fromRoute('https://play.qobuz.com/album/q1')
  expect(calls.map((c) => c.url)).toEqual(['/api/sources', '/api/resolve'])
  expect(s.query).toBe('https://play.qobuz.com/album/q1')
  expect(s.link?.item).toEqual(album)
})

test('a link shelf refuses shows its answer', async () => {
  ready(q)
  stubApi({ '/api/resolve': new Reply(422, { error: 'Music videos and stations are not supported yet.' }) })
  const s = useSearch()
  s.query = 'https://music.apple.com/ru/music-video/x/1'
  await s.submit()
  expect(s.link?.error).toBe('Music videos and stations are not supported yet.')
})

test('the address runs a search once, and a new type runs it again', async () => {
  ready(q)
  const calls = stubApi({ '/api/search': page([]) })
  const s = useSearch()
  await s.fromRoute('northern')
  await s.fromRoute('northern')
  expect(searches(calls)).toHaveLength(1)
  await s.fromRoute('northern', 'artist')
  expect(searches(calls).at(-1)?.get('type')).toBe('artist')
})

test('switching the type keeps the query', async () => {
  ready(q)
  const calls = stubApi({ '/api/search': page([]) })
  const s = useSearch()
  s.query = 'northern'
  await s.submit()
  s.setType('track')
  await vi.waitFor(() => expect(searches(calls)).toHaveLength(2))
  expect(searches(calls)[1]?.get('q')).toBe('northern')
})

test('a slow answer to an old search never reaches the list', async () => {
  ready(q)
  let release: (v: unknown) => void = () => {}
  stubApi({
    '/api/search': (url: string) => (url.includes('q=old') ? new Promise((r) => (release = r)) : page([item('qobuz', 'new')])),
  })
  const s = useSearch()
  s.query = 'old'
  const first = s.submit()
  s.query = 'new'
  await s.submit()
  release(page([item('qobuz', 'old')]))
  await first
  expect(s.items.map((i) => i.ref.id)).toEqual(['new'])
})

test('the sources switched off are remembered', () => {
  ready(q)
  useSearch().toggle('qobuz')
  setActivePinia(createPinia())
  expect(useSearch().off).toEqual(['qobuz'])
})
