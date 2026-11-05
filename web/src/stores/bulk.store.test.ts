import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import type { RunPreview, RunProgress } from '../api/types'
import { Reply, stubApi } from '../testing/api'
import { useBulk } from './bulk'
import { useLibrary } from './library'

const preview = (over: Partial<RunPreview> = {}): RunPreview => ({ selected: 2, fixes: [{ fix: 'no_releasedate', albums: 2 }], albums: 2, files: 20, bytes: 2e9, samples: [], ...over })
const run = (over: Partial<RunProgress> = {}): RunProgress => ({
  id: 9, kind: 'save', fixes: ['no_releasedate'], state: 'writing', reason: '', error: '', cancelling: false, total: 2, done: 0, skipped: 0, failed: 0, files: 0,
  now: '', now_state: '', log: [], created_at: '2026-11-04T10:00:00Z', finished_at: null, ...over,
})
const routes = (over: Record<string, unknown> = {}) => ({
  '/api/library/runs/preview': preview(),
  '/api/library/runs/current': { run: null },
  '/api/library/runs': run(),
  '/api/library/filters': { albums: 0, issues: 0, no_mbid: 0, kinds: [], folders: [], sources: [], no_source: 0 },
  '/api/library/albums': { albums: [], total: 0, limit: 100, offset: 0 },
  ...over,
})
const bodies = (calls: ReturnType<typeof stubApi>, path: string) => calls.filter((c) => c.url === path).map((c) => JSON.parse(c.init!.body as string))

beforeEach(() => setActivePinia(createPinia()))
afterEach(() => vi.unstubAllGlobals())

test('picks and unpicks albums, and the panel follows', async () => {
  const calls = stubApi(routes())
  const b = useBulk()
  b.toggle(1)
  b.toggle(2)
  await flushPromises()
  expect(b.count).toBe(2)
  expect(b.panel).toBe(true)
  expect(b.has(1)).toBe(true)
  expect(bodies(calls, '/api/library/runs/preview').at(-1)).toEqual({ selection: { albums: [1, 2] }, fixes: [] })
  b.toggle(1)
  b.toggle(2)
  await flushPromises()
  expect(b.panel).toBe(false)
  expect(b.preview).toBe(null)
})

test('Select all sends the query and the source filter, with the albums taken out', async () => {
  const calls = stubApi(routes())
  const library = useLibrary()
  library.total = 1099
  library.query = 'blue'
  library.filter = 'no_releasedate'
  library.source = 'qobuz'
  const b = useBulk()
  b.toggleAll()
  b.toggle(5)
  await flushPromises()
  expect(b.count).toBe(1098)
  expect(b.has(5)).toBe(false)
  expect(b.has(6)).toBe(true)
  expect(bodies(calls, '/api/library/runs/preview').at(-1)).toEqual({ selection: { all: true, q: 'blue', filter: 'no_releasedate', folder: '', source: 'qobuz', except: [5] }, fixes: [] })
})

test('a slow preview never replaces a newer one', async () => {
  let first: (v: unknown) => void = () => {}
  const slow = new Promise((res) => (first = res))
  let n = 0
  stubApi(routes({ '/api/library/runs/preview': () => (n++ === 0 ? slow : preview({ files: 2 })) }))
  const b = useBulk()
  b.toggle(1)
  b.toggleFix('replaygain')
  await flushPromises()
  first(preview({ files: 99 }))
  await flushPromises()
  expect(b.preview?.files).toBe(2)
})

test('starts a run, follows it, and reads the list again when it ends', async () => {
  const calls = stubApi(routes())
  const b = useBulk()
  b.toggle(1)
  b.toggleFix('no_releasedate')
  await flushPromises()
  await b.start()
  expect(b.working).toBe(true)
  expect(bodies(calls, '/api/library/runs')[0]).toEqual({ selection: { albums: [1] }, fixes: ['no_releasedate'] })
  b.applyRun(run({ done: 1 }))
  expect(b.run?.done).toBe(1)
  const before = calls.filter((c) => c.url.startsWith('/api/library/albums')).length
  b.applyRun(run({ done: 2, state: 'done' }))
  await flushPromises()
  expect(b.working).toBe(false)
  expect(calls.filter((c) => c.url.startsWith('/api/library/albums')).length).toBe(before + 1)
  b.close()
  expect(b.run).toBe(null)
  expect(b.count).toBe(0)
  expect(b.selecting).toBe(false)
})

test('a refused Start says why', async () => {
  stubApi(routes({ '/api/library/runs': new Reply(422, { error: 'none of these albums has anything to fix', reason: 'refused' }) }))
  const b = useBulk()
  b.toggle(1)
  await flushPromises()
  await b.start()
  expect(b.error).toBe('none of these albums has anything to fix')
  expect(b.run).toBe(null)
})

test('an undo of a run lights the lamp and leaves nothing behind', () => {
  stubApi(routes())
  const b = useBulk()
  b.applyRun(run({ kind: 'undo' }))
  expect(b.working).toBe(true)
  b.applyRun(run({ kind: 'undo', state: 'done' }))
  expect(b.working).toBe(false)
  expect(b.run).toBe(null)
})

test('a reconnect finds a run that ended in between', async () => {
  stubApi(routes({ '/api/library/runs/9': run({ state: 'done', done: 2 }) }))
  const b = useBulk()
  b.applyRun(run())
  await b.check()
  expect(b.run?.state).toBe('done')
})

test('a page that opens finds the run that works', async () => {
  stubApi(routes({ '/api/library/runs/current': { run: run() } }))
  const b = useBulk()
  await b.check()
  expect(b.working).toBe(true)
})
