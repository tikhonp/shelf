import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, afterEach, test, expect, vi } from 'vitest'
import type { ProposalDetail, Preview } from '../api/agents'
import { setAuthHandling } from '../api/client'
import { Reply, stubApi } from '../testing/api'
import { useInbox } from './inbox'
import { useMemory } from './memory'
import { useAgents } from './agents'
import InboxValues from '../components/InboxValues.vue'

const preview = (digest = 'a'): Preview => ({ album: { id: 7, folder: 'a', title: 'Album', artist: 'Artist' }, files: [{ track_id: 1, path: 'a/1.flac', keys: [{ key: 'GENRE', before: ['Rock'], after: ['Art Rock'] }], cover_before: '', cover_after: '' }], folder: [], warnings: [], digest })
function detail(): ProposalDetail {
  return { id: 1, url: '/inbox/1', state: 'pending', changes_requested: false, connection: { id: 1, name: 'Agent' }, run_id: 1, album: { id: 7, current_id: 7, title: 'Album', artist: 'Artist', folder: 'a' }, group: '', follow_up_of: null, head: 1, revisions: [], current: { title: 'Genre', reason: 'Specific', uncertainties: '', confidence: null, evidence: [], base: [{ track_id: 1, revision: 'r1' }], ops: [{ id: 'genre', type: 'tag', field: 'genre', tracks: [{ track_id: 1, values: ['Art Rock'] }] }], preview: preview() }, ops: [{ id: 'genre', type: 'tag', decision: 'pending' }], summary: { pending: 1, accepted: 0, declined: 0, superseded: 0 }, acceptances: [], events: [] }
}
const parsed = (calls: ReturnType<typeof stubApi>, suffix: string) => calls.filter(c => c.url.endsWith(suffix)).map(c => JSON.parse(c.init!.body as string))
async function opened(extra: Record<string, unknown> = {}) {
  const calls = stubApi({ '/api/inbox/proposals/1/preview': { preview: preview(), refusal: null, missing: [] }, '/api/inbox/proposals/1': detail(), ...extra })
  const store = useInbox()
  await store.open(1)
  await flushPromises()
  return { store, calls }
}
beforeEach(() => { setActivePinia(createPinia()); setAuthHandling(() => {}, () => false) })
afterEach(() => vi.unstubAllGlobals())

test('feature off is hidden, and without sign-in the UI cannot create a connection', async () => {
  stubApi({ '/api/inbox': new Reply(404, { error: 'Not found' }) })
  const s = useInbox()
  await s.detect()
  expect(s.enabled).toBe(false)
  stubApi({ '/api/inbox': { pending: 0, connections: 0, changes_requested: 0, accounts: 'none', can_create_connections: false } })
  await s.detect()
  const a = useAgents()
  expect(a.canCreate).toBe(false)
  expect(await a.create('Agent', [])).toBe(false)
})

test('values with punctuation remain separate literal values', async () => {
  const { store, calls } = await opened({ '/api/inbox/proposals/1/revisions': () => ({ ...detail(), head: 2 }) })
  store.setValues('genre', [1], ['Rock; Pop', 'R&B, Soul'])
  expect(store.canAccept).toBe(false)
  await store.saveValues()
  expect(parsed(calls, '/revisions')[0].ops[0].tracks[0].values).toEqual(['Rock; Pop', 'R&B, Soul'])
  expect(calls.some(c => c.url.endsWith('/accept'))).toBe(false)
})

test('changing a multi-value input preserves punctuation and emits the values independently', async () => {
  const w = mount(InboxValues, { props: { values: ['Rock; Pop', 'Soul'], label: 'Genre' } })
  await w.findAll('input')[1]!.setValue('R&B, Soul')
  expect(w.emitted('update')?.[0]?.[0]).toEqual(['Rock; Pop', 'R&B, Soul'])
})

test('a changed diff is displayed and only the next explicit acceptance uses its digest', async () => {
  const { store, calls } = await opened({ '/api/inbox/proposals/1/accept': new Reply(409, { code: 'preview_changed', error: 'Changed', preview: preview('b') }) })
  expect(store.canAccept).toBe(true)
  await store.accept()
  expect(store.selection?.preview?.digest).toBe('b')
  expect(parsed(calls, '/accept')).toHaveLength(1)
  await store.accept()
  expect(parsed(calls, '/accept').map(b => b.digest)).toEqual(['a', 'b'])
})

test('a lost response retries the same acceptance ID instead of creating another write', async () => {
  const { store, calls } = await opened({ '/api/inbox/proposals/1/accept': () => { throw new Error('Disconnected') } })
  await store.accept()
  await flushPromises()
  await store.accept()
  const bodies = parsed(calls, '/accept')
  expect(bodies).toHaveLength(2)
  expect(bodies[0].request_id).toBeTruthy()
  expect(bodies[1].request_id).toBe(bodies[0].request_id)
})

test('reload refreshes the selected preview even when the proposal revision stayed the same', async () => {
  let digest = 'a'
  const { store } = await opened({ '/api/inbox/proposals/1/preview': () => ({ preview: preview(digest), refusal: null, missing: [] }) })
  digest = 'b'
  await store.loadDetail()
  await flushPromises()
  expect(store.selection?.preview?.digest).toBe('b')
})

test('a concurrent revision keeps user drafts until the user chooses to rebase them', async () => {
  const { store } = await opened({ '/api/inbox/proposals/1/revisions': new Reply(409, { code: 'stale_revision', error: 'New revision' }) })
  store.setValues('genre', [1], ['Progressive Rock'])
  expect(await store.saveValues()).toBe(false)
  expect(store.dirty).toBe(true)
  expect(store.values(store.detail!.current.ops[0] as import('../api/agents').TagOp, 1)).toEqual(['Progressive Rock'])
})

test('declining selected fields does not accept any library operations', async () => {
  const { store, calls } = await opened({ '/api/inbox/proposals/1/decline': {} })
  await store.decline(false, 'Prefer the current genre')
  expect(parsed(calls, '/decline')[0]).toEqual({ revision: 1, ops: ['genre'], reason: 'Prefer the current genre' })
  expect(calls.some(c => c.url.endsWith('/accept'))).toBe(false)
})

test('memory paging retrieves documents beyond the first page and correction sends its expected version', async () => {
  const doc = { id: 3, key: 'genre', version: 2 }
  const calls = stubApi({ '/api/inbox/memory/3': doc, '/api/inbox/memory': (url: string) => url.includes('after=3') ? { memory: [{ ...doc, id: 4 }], next: null } : { memory: [doc], next: 3 } })
  const s = useMemory()
  await s.load()
  await s.more()
  expect(s.docs.map(d => d.id)).toEqual([3, 4])
  await s.correct(s.docs[0]!, { body: 'Specific genres' })
  expect(parsed(calls, '/memory/3')[0]).toEqual({ expected_version: 2, body: 'Specific genres' })
})

test('a warning changed under the same kind needs fresh confirmation', async () => {
  let message = 'Renames the artist to A'
  const { store } = await opened({ '/api/inbox/proposals/1/preview': () => ({ preview: { ...preview(message), warnings: [{ kind: 'album_artist', message }] }, refusal: null, missing: ['album_artist'] }) })
  store.toggleConfirm('album_artist')
  expect(store.canAccept).toBe(true)
  message = 'Renames the artist to B'
  await store.loadDetail()
  await flushPromises()
  expect(store.confirm).toEqual([])
  expect(store.canAccept).toBe(false)
})

// Three dated fields: releasedate requires originaldate, and genre stands alone.
function dated(): ProposalDetail {
  const d = detail()
  const tag = (id: string, requires?: string[]) => ({ id, type: 'tag' as const, field: id, tracks: [{ track_id: 1, values: ['x'] }], ...(requires ? { requires } : {}) })
  d.current.ops = [tag('releasedate', ['originaldate']), tag('genre'), tag('originaldate')]
  d.ops = ['releasedate', 'genre', 'originaldate'].map((id) => ({ id, type: 'tag' as const, decision: 'pending' as const }))
  d.summary = { pending: 3, accepted: 0, declined: 0, superseded: 0 }
  return d
}

test('switching a required field off also switches off the fields that need it, and back on', async () => {
  const { store } = await opened({ '/api/inbox/proposals/1': dated() })
  expect([...store.selected].sort()).toEqual(['genre', 'originaldate', 'releasedate'])
  store.toggle('originaldate')
  expect(store.selected).toEqual(['genre'])
  expect(store.cascade).toEqual({ op: 'originaldate', on: false, moved: ['releasedate'] })
  store.toggle('releasedate')
  expect([...store.selected].sort()).toEqual(['genre', 'originaldate', 'releasedate'])
  expect(store.cascade).toEqual({ op: 'releasedate', on: true, moved: ['originaldate'] })
  store.toggle('genre')
  expect(store.cascade).toBeNull()
})

test('a required field is listed above the fields that need it', async () => {
  const { needs, ordered } = await import('./inbox')
  const ops = dated().current.ops
  expect(ordered(ops).map((o) => o.id)).toEqual(['originaldate', 'releasedate', 'genre'])
  expect(needs(ops, 'releasedate')).toEqual(['originaldate'])
})

test('Decline the rest declines only the fields left off, or every pending field when none is on', async () => {
  const { store, calls } = await opened({ '/api/inbox/proposals/1/decline': {}, '/api/inbox/proposals/1': dated() })
  store.toggle('genre')
  expect(store.rest).toEqual(['genre'])
  await store.declineRest('Not now')
  expect(parsed(calls, '/decline')[0]).toEqual({ revision: 1, ops: ['genre'], reason: 'Not now' })
  store.selectAll(false)
  await store.declineRest('')
  expect(parsed(calls, '/decline')[1]).toEqual({ revision: 1, remaining: true, reason: '' })
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

test('leaving Agents forgets a late creation secret but still reloads connections', async () => {
  const response = deferred<unknown>()
  const connection = { id: 2, name: 'New agent' }
  const calls = stubApi({
    '/api/inbox': { accounts: 'simple', can_create_connections: true },
    '/api/inbox/connections': (_url: string, init?: RequestInit) => init?.method === 'POST' ? response.promise : { connections: [connection] },
    '/api/inbox/setup': {},
  })
  await useInbox().detect()
  const store = useAgents()
  const saving = store.create('New agent', [])
  store.forget()
  response.resolve({ connection, token: 'one-time-secret' })
  expect(await saving).toBe(true)
  expect(store.created).toBeNull()
  expect(store.connections.map(c => c.id)).toEqual([2])
  expect(calls.filter(c => c.url.endsWith('/connections'))).toHaveLength(2)
})

test.each([false, true])('a late save leaves another review alone (close and reopen: %s)', async (reopen) => {
  const response = deferred<ProposalDetail>()
  const other = { ...detail(), id: 2 }
  const { store } = await opened({
    '/api/inbox/proposals/1/revisions': () => response.promise,
    '/api/inbox/proposals/2': other,
  })
  store.setValues('genre', [1], ['Saved'])
  const saving = store.saveValues()
  if (reopen) store.close()
  await store.open(reopen ? 1 : 2)
  store.setValues('genre', [1], ['New review'])
  response.resolve({ ...detail(), head: 2 })
  expect(await saving).toBe(true)
  expect(store.detail?.id).toBe(reopen ? 1 : 2)
  expect(store.detail?.head).toBe(1)
  expect(store.drafts.genre?.tracks[1]).toEqual(['New review'])
  expect(store.notice).toBe('')
})

test('saving clears unchanged submitted drafts and preserves newer values in another draft', async () => {
  const response = deferred<ProposalDetail>()
  const d = dated()
  const { store } = await opened({ '/api/inbox/proposals/1': d, '/api/inbox/proposals/1/revisions': () => response.promise })
  store.setValues('genre', [1], ['Saved'])
  store.setValues('releasedate', [1], ['1974'])
  const saving = store.saveValues()
  store.setValues('genre', [1], ['Newer'])
  store.setValues('originaldate', [1], ['1970'])
  response.resolve({ ...d, head: 2 })
  await saving
  expect(store.drafts.releasedate).toBeUndefined()
  expect(store.drafts.genre?.tracks[1]).toEqual(['Newer'])
  expect(store.drafts.originaldate?.tracks[1]).toEqual(['1970'])
  expect(store.detail?.head).toBe(2)
  expect(store.dirty).toBe(true)
})

test('preview_changed belongs to the submitted selection, while fields can still be toggled', async () => {
  const response = deferred<Reply>()
  const { store, calls } = await opened({
    '/api/inbox/proposals/1': dated(),
    '/api/inbox/proposals/1/accept': () => response.promise,
    '/api/inbox/proposals/1/preview': (_url: string, init?: RequestInit) => ({ preview: preview(JSON.parse(init!.body as string).ops.join(',')), refusal: null, missing: [] }),
  })
  const accepting = store.accept()
  store.toggle('genre')
  await flushPromises()
  const digest = store.selection?.preview?.digest
  response.resolve(new Reply(409, { code: 'preview_changed', preview: preview('submitted-selection') }))
  expect(await accepting).toBe(false)
  await flushPromises()
  expect(store.selection?.preview?.digest).toBe(digest)
  expect(store.canAccept).toBe(true)
  await store.accept()
  expect(parsed(calls, '/accept')[1].digest).toBe(digest)
})

test.each(['accept', 'comment'] as const)('a late %s response does not reload or annotate another proposal', async (action) => {
  const response = deferred<unknown>()
  const { store, calls } = await opened({
    [`/api/inbox/proposals/1/${action === 'accept' ? 'accept' : 'comments'}`]: () => response.promise,
    '/api/inbox/proposals/2': { ...detail(), id: 2 },
  })
  const pending = action === 'accept' ? store.accept() : store.comment('A reply')
  await store.open(2)
  store.setValues('genre', [1], ['B draft'])
  response.resolve({})
  await pending
  expect(store.detail?.id).toBe(2)
  expect(store.drafts.genre?.tracks[1]).toEqual(['B draft'])
  expect(store.notice).toBe('')
  expect(calls.filter(c => c.url === '/api/inbox/proposals/2')).toHaveLength(1)
})

test('refresh drops selected fields with new unselected requirements, including transitive dependencies', async () => {
  const d = dated()
  const { store } = await opened({ '/api/inbox/proposals/1': () => d })
  store.toggle('genre')
  d.head = 2
  d.current.ops.find(o => o.id === 'originaldate')!.requires = ['genre']
  await store.loadDetail()
  expect(store.selected).toEqual([])
  expect(store.cascade).toEqual({ op: 'genre', on: false, moved: ['releasedate', 'originaldate'] })
})

test.each(['accepted', 'declined'] as const)('refresh treats a %s requirement like toggle does', async (decision) => {
  const d = dated()
  const { store } = await opened({ '/api/inbox/proposals/1': () => d })
  store.toggle('genre')
  d.head = 2
  d.ops.find(o => o.id === 'genre')!.decision = decision
  d.current.ops.find(o => o.id === 'originaldate')!.requires = ['genre']
  await store.loadDetail()
  expect(store.selected).toContain('originaldate')
  store.toggle('originaldate')
  store.toggle('originaldate')
  expect(store.selected).toContain('originaldate')
  expect(store.selected).not.toContain('genre')
})

test('returning to the old head during a save remains an edit against the saved revision', async () => {
  const response = deferred<ProposalDetail>()
  const { store } = await opened({ '/api/inbox/proposals/1/revisions': () => response.promise })
  store.setValues('genre', [1], ['Saved'])
  const saving = store.saveValues()
  store.setValues('genre', [1], ['Art Rock'])
  const answer = detail()
  answer.head = 2
  ;(answer.current.ops[0] as import('../api/agents').TagOp).tracks[0]!.values = ['Saved']
  response.resolve(answer)
  await saving
  expect(store.drafts.genre?.tracks[1]).toEqual(['Art Rock'])
  expect(store.dirty).toBe(true)
})

test('late album proposal reads cannot restore old suggestion tapes', async () => {
  const response = deferred<unknown>()
  let count = 0
  stubApi({ '/api/inbox/albums/7/proposals': () => ++count === 1 ? response.promise : { proposals: [] } })
  const store = useInbox(); store.enabled = true
  const first = store.loadAlbum(7)
  await store.loadAlbum(7)
  response.resolve({ proposals: [detail()] })
  await first
  expect(store.byAlbum[7]).toEqual([])
})
