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
