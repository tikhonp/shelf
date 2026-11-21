import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, afterEach, test, expect, vi } from 'vitest'
import type { ProposalDetail, Preview, PreviewAnswer } from '../api/agents'
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
  expect(store.canAccept).toBe(false)
  expect(await store.accept()).toBe(false)
  store.reviewPreview('a')
  expect(store.canAccept).toBe(false)
  store.reviewPreview('b')
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
  expect(store.canAccept).toBe(false)
  store.reviewPreview('b')
  expect(store.canAccept).toBe(true)
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

test('a new connection keeps its prompt with the secret until it is forgotten', async () => {
  const connection = { id: 3, name: 'Claude' }
  stubApi({
    '/api/inbox': { accounts: 'simple', can_create_connections: true },
    '/api/inbox/connections': (_url: string, init?: RequestInit) => init?.method === 'POST' ? { connection, token: 'one-time-secret', prompt: 'Bearer one-time-secret' } : { connections: [connection] },
    '/api/inbox/setup': {},
  })
  await useInbox().detect()
  const store = useAgents()
  expect(await store.create('Claude', [])).toBe(true)
  expect(store.created?.prompt).toBe('Bearer one-time-secret')
  store.forget()
  expect(store.created).toBeNull()
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

test('a preview check blocks acceptance and a late answer cannot replace a newer selection', async () => {
  const pending = deferred<unknown>()
  let starting = true
  const { store, calls } = await opened({
    '/api/inbox/proposals/1': dated(),
    '/api/inbox/proposals/1/preview': (_url: string, init?: RequestInit) => {
      const key = JSON.parse(init!.body as string).ops.join(',')
      if (key === 'originaldate,releasedate') return pending.promise
      return { preview: preview(starting ? 'original' : 'updated'), refusal: null, missing: [] }
    },
  })
  expect(store.canAccept).toBe(true)
  store.toggle('genre')
  expect(store.previewing).toBe(true)
  expect(store.canAccept).toBe(false)
  expect(await store.accept()).toBe(false)
  starting = false
  store.toggle('genre')
  await flushPromises()
  pending.resolve({ preview: preview('late'), refusal: null, missing: [] })
  await flushPromises()
  expect(store.preview?.digest).toBe('updated')
  expect(store.canAccept).toBe(false)
  expect(parsed(calls, '/accept')).toHaveLength(0)
  store.reviewPreview('updated')
  expect(store.canAccept).toBe(true)
})

test('an acceptance refusal invalidates its earlier preview', async () => {
  const { store, calls } = await opened({
    '/api/inbox/proposals/1/accept': new Reply(422, { code: 'refused', refusal: { error: 'The track is locked.', reason: 'locked' } }),
  })
  expect(store.canAccept).toBe(true)
  expect(await store.accept()).toBe(false)
  expect(store.currentSelection?.refusal?.reason).toBe('locked')
  expect(store.preview).toBeNull()
  expect(store.canAccept).toBe(false)
  expect(await store.accept()).toBe(false)
  expect(parsed(calls, '/accept')).toHaveLength(1)
})

test('checking again after a temporary refusal still requires review of a changed preview', async () => {
  let answer: PreviewAnswer = { preview: preview('original'), refusal: null, missing: [] }
  const { store } = await opened({ '/api/inbox/proposals/1/preview': () => answer })
  answer = { preview: null, refusal: { reason: 'locked', error: 'The track is locked.' }, missing: [] }
  await store.checkFiles()
  await flushPromises()
  expect(store.canAccept).toBe(false)
  answer = { preview: preview('updated'), refusal: null, missing: [] }
  await store.checkFiles()
  await flushPromises()
  expect(store.preview?.digest).toBe('updated')
  expect(store.canAccept).toBe(false)
  store.reviewPreview('updated')
  expect(store.canAccept).toBe(true)
})

test('All off keeps hidden fields selected, even when they depend on a visible field', async () => {
  const d = dated()
  d.live = { preview: preview(), refusal: null, op_stale: {}, op_unchanged: { releasedate: true } }
  const { store } = await opened({ '/api/inbox/proposals/1': d })
  store.selectAll(false)
  expect(store.selected).toEqual(['releasedate'])
  expect(store.shown).toEqual([])
  expect(store.declineCount).toBe(3)
})

test('a new revision that changes previewed writes forces reviewRequired and clears warning confirmations even with identical warning text', async () => {
  const d = detail()
  let digest = 'rev1'
  const warn = { kind: 'album_artist', message: 'Renames the album artist in Navidrome.' }
  const { store } = await opened({
    '/api/inbox/proposals/1': () => structuredClone(d),
    '/api/inbox/proposals/1/preview': () => ({ preview: { ...preview(digest), warnings: [warn] }, refusal: null, missing: ['album_artist'] }),
  })
  store.toggleConfirm('album_artist')
  expect(store.reviewRequired).toBe(false)
  expect(store.canAccept).toBe(true)

  d.head = 2
  digest = 'rev2'
  await store.loadDetail()
  await flushPromises()
  expect(store.reviewRequired).toBe(true)
  expect(store.confirm).toEqual([])
  expect(store.canAccept).toBe(false)

  store.reviewPreview('rev2')
  expect(store.reviewRequired).toBe(false)
  expect(store.canAccept).toBe(false)
  store.toggleConfirm('album_artist')
  expect(store.canAccept).toBe(true)
})

test('a new revision that changes an op target under the same op ID blocks silent rebase and surfaces a conflict', async () => {
  const d = dated()
  const { store, calls } = await opened({
    '/api/inbox/proposals/1': () => structuredClone(d),
    '/api/inbox/proposals/1/revisions': () => ({ ...structuredClone(d), head: 3 }),
  })
  store.setValues('genre', [1], ['Progressive Rock'])
  store.setValues('originaldate', [1], ['1974'])

  d.head = 2
  const genreOp = d.current.ops.find((o) => o.id === 'genre')!
  if (genreOp.type !== 'tag') throw new Error('expected tag op')
  genreOp.field = 'mood'
  genreOp.tracks = [{ track_id: 1, values: ['Calm'] }]

  await store.loadDetail()
  await flushPromises()

  expect(store.draftConflicts).toHaveLength(1)
  expect(store.draftConflicts[0]).toMatchObject({ opId: 'genre', originalLabel: 'Genre', currentLabel: 'Mood', text: 'Progressive Rock' })
  // Conflicted draft must not leak onto Mood or be sent in revisionOps.
  expect(store.values(store.detail!.current.ops.find((o) => o.id === 'genre') as import('../api/agents').TagOp, 1)).toEqual(['Calm'])
  expect(store.revisionOps().map((o) => o.id)).toEqual(['originaldate'])
  expect(store.rebase()).toBe(false)
  expect(await store.saveValues()).toBe(false)
  expect(parsed(calls, '/revisions')).toHaveLength(0)

  // Discarding the conflicted draft unblocks rebasing and saving the matching draft.
  store.discard('genre')
  expect(store.draftConflicts).toEqual([])
  expect(store.rebase()).toBe(true)
  expect(await store.saveValues()).toBe(true)
  expect(parsed(calls, '/revisions')[0]).toEqual({ parent_revision: 2, ops: [{ id: 'originaldate', tracks: [{ track_id: 1, values: ['1974'] }] }] })
})

test('deciding or superseding an edited op via live refresh retains the dropped draft until dismissed', async () => {
  const d = dated()
  const { store } = await opened({ '/api/inbox/proposals/1': () => structuredClone(d) })
  store.setValues('genre', [1], ['Progressive Rock'])
  store.setValues('originaldate', [1], ['1974'])

  d.head = 2
  d.ops.find((o) => o.id === 'genre')!.decision = 'superseded'
  d.current.ops = d.current.ops.filter((o) => o.id !== 'genre')
  d.ops.find((o) => o.id === 'originaldate')!.decision = 'accepted'

  await store.loadDetail()
  await flushPromises()

  expect(store.drafts).toEqual({})
  expect(store.droppedDrafts).toEqual([
    { opId: 'genre', label: 'Genre', reason: 'superseded', text: 'Progressive Rock' },
    { opId: 'originaldate', label: 'Original date', reason: 'accepted', text: '1974' },
  ])
  store.dismissDroppedDraft('genre')
  expect(store.droppedDrafts.map((x) => x.opId)).toEqual(['originaldate'])
  store.dismissDroppedDraft()
  expect(store.droppedDrafts).toEqual([])
})

test('hasUnsaved tracks value edits including pending empty rows, reply text, and decline reason', async () => {
  const { store } = await opened()
  expect(store.hasUnsaved).toBe(false)

  // Adding a pending empty row marks hasUnsaved even though revisionOps() drops empty strings.
  store.setValues('genre', [1], ['Art Rock', ''])
  expect(store.dirty).toBe(false)
  expect(store.hasValueDrafts).toBe(true)
  expect(store.hasUnsaved).toBe(true)

  // Removing the empty row returns to head and clears hasUnsaved.
  store.setValues('genre', [1], ['Art Rock'])
  expect(store.hasValueDrafts).toBe(false)
  expect(store.hasUnsaved).toBe(false)

  store.replyDraft = '   '
  expect(store.hasUnsaved).toBe(false)
  store.replyDraft = 'Check the pressing'
  expect(store.hasReplyDraft).toBe(true)
  expect(store.hasUnsaved).toBe(true)

  store.replyDraft = ''
  store.declineReason = 'Not this reissue'
  expect(store.hasDeclineDraft).toBe(true)
  expect(store.hasUnsaved).toBe(true)

  store.close()
  expect(store.replyDraft).toBe('')
  expect(store.declineReason).toBe('')
  expect(store.hasUnsaved).toBe(false)
})

const summary = (id: number, title = `Album ${id}`) => ({
  id,
  state: 'pending' as const,
  changes_requested: false,
  title: 'Tags',
  album: { id, current_id: id, title, artist: 'Artist', folder: `a/${id}` },
  connection: { id: 1, name: 'Agent' },
  kinds: ['tag'],
  fields: [{ id: 'genre', type: 'tag' as const, field: 'genre' }],
  confidence: 80,
  summary: { pending: 1, accepted: 0, declined: 0, superseded: 0 },
  updated_at: '2026-11-09T10:00:00Z',
  stale: false,
})

test('detect disables Inbox only on 404 and keeps a retryable error on transient failures', async () => {
  let fail = true
  stubApi({
    '/api/inbox': () => (fail ? new Reply(503, { error: 'Service unavailable' }) : { pending: 2, connections: 1, changes_requested: 0, accounts: 'none', can_create_connections: false }),
  })
  const store = useInbox()
  await store.detect()
  expect(store.enabled).toBeNull()
  expect(store.detectError).toContain('Service unavailable')

  fail = false
  await store.detect()
  expect(store.enabled).toBe(true)
  expect(store.detectError).toBe('')
  expect(store.info?.pending).toBe(2)

  // A later transient error keeps enabled = true instead of hiding the Inbox.
  fail = true
  await store.detect()
  expect(store.enabled).toBe(true)
  expect(store.detectError).toContain('Service unavailable')

  // Only a confirmed 404 disables Inbox.
  stubApi({ '/api/inbox': new Reply(404, { error: 'Not found' }) })
  await store.detect()
  expect(store.enabled).toBe(false)
  expect(store.detectError).toBe('')
})

test('changing filters clears stale rows and cursor immediately and stays empty on failure', async () => {
  let resolveNext: ((v: unknown) => void) | null = null
  stubApi({
    '/api/inbox/proposals': (url: string) => {
      if (url.includes('kind=cover')) {
        return new Promise((resolve) => { resolveNext = resolve })
      }
      return { proposals: [summary(10), summary(9)], next: 9 }
    },
  })
  const store = useInbox()
  store.setFilters({ state: 'pending', album: null, kind: '', connection: null })
  await flushPromises()
  expect(store.proposals.map((p) => p.id)).toEqual([10, 9])
  expect(store.next).toBe(9)

  store.setFilters({ state: 'pending', album: null, kind: 'cover', connection: null })
  expect(store.proposals).toEqual([])
  expect(store.next).toBeNull()
  expect(store.listLoading).toBe(true)

  resolveNext!(new Reply(500, { error: 'Filter query failed' }))
  await flushPromises()
  expect(store.proposals).toEqual([])
  expect(store.next).toBeNull()
  expect(store.listError).toContain('Filter query failed')
})

test('SSE refresh preserves already paginated pages without dropping earlier pages', async () => {
  let version = 1
  stubApi({
    '/api/inbox': { pending: 3, connections: 1, changes_requested: 0, accounts: 'none', can_create_connections: false },
    '/api/inbox/proposals': (url: string) => {
      if (url.includes('before=20')) {
        return { proposals: [summary(10, `Page 2 v${version}`)], next: null }
      }
      return { proposals: [summary(30, `Page 1a v${version}`), summary(20, `Page 1b v${version}`)], next: 20 }
    },
  })
  const store = useInbox()
  await store.loadList()
  await store.moreList()
  expect(store.proposals.map((p) => p.id)).toEqual([30, 20, 10])
  expect(store.next).toBeNull()

  version = 2
  store.applyHub({ proposal: 30 })
  await flushPromises()
  expect(store.proposals.map((p) => [p.id, p.album.title])).toEqual([
    [30, 'Page 1a v2'],
    [20, 'Page 1b v2'],
    [10, 'Page 2 v2'],
  ])
  expect(store.next).toBeNull()
})

test('saveValues does not rebind in-flight edits when an agent retargets the op after save commits, and keeps newer SSE detail', async () => {
  const response = deferred<ProposalDetail>()
  const d = detail()
  const { store } = await opened({
    '/api/inbox/proposals/1': d,
    '/api/inbox/proposals/1/revisions': () => response.promise,
  })

  // Edit Genre, start saving, then keep typing Genre while the request is in flight.
  store.setValues('genre', [1], ['Saved Genre'])
  const saving = store.saveValues()
  store.setValues('genre', [1], ['Still Genre'])

  // Server committed save as rev 2, then an agent retargeted op "genre" to Mood in rev 3 before the response read detail.
  const rev3 = detail()
  rev3.head = 3
  rev3.current.ops = [{ id: 'genre', type: 'tag', field: 'mood', tracks: [{ track_id: 1, values: ['Calm'] }] }]
  response.resolve(rev3)
  expect(await saving).toBe(true)

  // Remaining draft must stay targeted to Genre, conflict against rev 3 Mood, and headSeen stays at saved rev 2.
  expect(store.headSeen).toBe(2)
  expect(store.drafts.genre?.target?.field).toBe('genre')
  expect(store.draftConflicts).toHaveLength(1)
  expect(store.draftConflicts[0]?.reason).toContain('Genre')
  expect(store.draftConflicts[0]?.reason).toContain('Mood')
  expect(store.rebase()).toBe(false)

  // Now test that a newer SSE detail already received is not overwritten by an older save response.
  const response2 = deferred<ProposalDetail>()
  const d2 = detail()
  const rev4 = detail()
  rev4.head = 4
  rev4.current.title = 'Newer SSE title'
  const { store: store2 } = await opened({
    '/api/inbox/proposals/1': () => rev4,
    '/api/inbox/proposals/1/revisions': () => response2.promise,
  })
  // Reset store2 detail to rev 1 first.
  store2.detail = d2
  store2.headSeen = 1
  store2.setValues('genre', [1], ['Edit 1'])
  const saving2 = store2.saveValues()
  await store2.loadDetail()
  expect(store2.detail?.head).toBe(4)

  const rev2 = detail()
  rev2.head = 2
  rev2.current.title = 'Older save response'
  response2.resolve(rev2)
  await saving2
  expect(store2.detail?.head).toBe(4)
  expect(store2.detail?.current.title).toBe('Newer SSE title')
})

test.each([true, false])('failed SSE refresh retries refreshList across multiple pages (hasMore: %s)', async (hasMore) => {
  let version = 1
  let failRefresh = false
  stubApi({
    '/api/inbox': { pending: 3, connections: 1, changes_requested: 0, accounts: 'none', can_create_connections: false },
    '/api/inbox/proposals': (url: string) => {
      if (failRefresh) return new Reply(503, { error: 'Refresh failed' })
      if (url.includes('before=20')) {
        return { proposals: [summary(10, `Page 2 v${version}`)], next: hasMore ? 10 : null }
      }
      return { proposals: [summary(30, `Page 1a v${version}`), summary(20, `Page 1b v${version}`)], next: 20 }
    },
  })
  const store = useInbox()
  await store.loadList()
  await store.moreList()
  expect(store.proposals.map((p) => p.id)).toEqual([30, 20, 10])
  expect(store.next).toBe(hasMore ? 10 : null)

  version = 2
  failRefresh = true
  store.applyHub({ proposal: 30 })
  await flushPromises()
  expect(store.listError).toContain('Refresh failed')
  expect(store.proposals.map((p) => p.album.title)).toEqual(['Page 1a v1', 'Page 1b v1', 'Page 2 v1'])

  failRefresh = false
  // Automatic pagination while a refresh error is active must not wipe the refresh failure.
  await store.moreList()
  expect(store.listError).toContain('Refresh failed')

  await store.retryList()
  expect(store.listError).toBe('')
  expect(store.proposals.map((p) => [p.id, p.album.title])).toEqual([
    [30, 'Page 1a v2'],
    [20, 'Page 1b v2'],
    [10, 'Page 2 v2'],
  ])
  expect(store.next).toBe(hasMore ? 10 : null)
})

test('same-head withdrawal/decline is not rolled back by a delayed save response, and replacement drafts keep their new target', async () => {
  // 1. Delayed save response at head 2 must not roll back a same-head withdrawal with more events.
  const saveRes = deferred<ProposalDetail>()
  const d1 = detail()
  const withdrawnAt2 = detail()
  withdrawnAt2.head = 2
  withdrawnAt2.state = 'withdrawn'
  withdrawnAt2.events = [
    ...withdrawnAt2.events,
    { id: 2, at: '2026-11-09T11:00:00Z', kind: 'proposal_edited', actor_kind: 'user', actor: 'alice' },
    { id: 3, at: '2026-11-09T11:01:00Z', kind: 'proposal_withdrawn', actor_kind: 'agent', actor: 'Claude' },
  ]
  const { store } = await opened({
    '/api/inbox/proposals/1': () => structuredClone(withdrawnAt2),
    '/api/inbox/proposals/1/revisions': () => saveRes.promise,
  })
  store.detail = d1
  store.headSeen = 1
  store.setValues('genre', [1], ['Edit'])
  const saving = store.saveValues()
  await store.loadDetail()
  expect(store.detail?.state).toBe('withdrawn')

  const staleSaveAnswer = detail()
  staleSaveAnswer.head = 2
  staleSaveAnswer.events = [
    ...staleSaveAnswer.events,
    { id: 2, at: '2026-11-09T11:00:00Z', kind: 'proposal_edited', actor_kind: 'user', actor: 'alice' },
  ]
  saveRes.resolve(staleSaveAnswer)
  await saving
  expect(store.detail?.state).toBe('withdrawn')

  // 2. Discarding a conflicting draft and editing the retargeted op while POST is pending preserves the new draft target.
  const saveRes2 = deferred<ProposalDetail>()
  const rev3 = detail()
  rev3.head = 3
  rev3.current.ops = [{ id: 'genre', type: 'tag', field: 'mood', tracks: [{ track_id: 1, values: ['Calm'] }, { track_id: 2, values: ['Calm'] }] }]
  const { store: store2 } = await opened({
    '/api/inbox/proposals/1': () => structuredClone(rev3),
    '/api/inbox/proposals/1/revisions': () => saveRes2.promise,
  })
  store2.detail = detail()
  store2.headSeen = 1
  store2.setValues('genre', [1], ['Old Genre'])
  const saving2 = store2.saveValues()
  await store2.loadDetail()
  expect(store2.draftConflicts).toHaveLength(1)
  store2.discard('genre')
  store2.setValues('genre', [1, 2], ['Upbeat'])
  expect(store2.draftConflicts).toHaveLength(0)

  saveRes2.resolve(staleSaveAnswer)
  await saving2
  expect(store2.drafts.genre?.target?.field).toBe('mood')
  expect(store2.drafts.genre?.target?.track_ids).toEqual([1, 2])
  expect(store2.draftConflicts).toHaveLength(0)
})
