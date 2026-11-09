import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { createMemoryHistory } from 'vue-router'
import type { Preview, ProposalDetail, ProposalSummary } from '../api/agents'
import App from '../App.vue'
import { setAuthHandling } from '../api/client'
import { createAppRouter } from '../router'
import { useInbox } from '../stores/inbox'
import { FakeEventSource, list, source, stubApi } from '../testing/api'
import InboxDetail from './InboxDetail.vue'
import InboxDiff from './InboxDiff.vue'
import InboxList from './InboxList.vue'

const file = (n: number, keys: { key: string; before: string[]; after: string[] }[]) => ({ track_id: n, path: `a/0${n}.flac`, keys, cover_before: '', cover_after: '' })
const sameOnSix = (): Preview => ({
  album: { id: 7, folder: 'a', title: 'Album', artist: 'Artist' },
  files: [1, 2, 3, 4, 5, 6].map((n) => file(n, [{ key: 'GENRE', before: ['Rock'], after: ['Art Rock'] }, { key: 'ORIGINALDATE', before: [], after: ['1974'] }, { key: 'TITLE', before: [`T${n}`], after: [`Track ${n}`] }])),
  folder: [],
  warnings: [{ kind: 'reissue', message: 'The release date is later than the original date.' }],
  digest: 'd',
})

function detail(): ProposalDetail {
  const tag = (id: string, requires?: string[]) => ({ id, type: 'tag' as const, field: id, tracks: [1, 2, 3, 4, 5, 6].map((t) => ({ track_id: t, values: ['x'] })), ...(requires ? { requires } : {}) })
  return {
    id: 4, url: '/inbox/4', state: 'pending', changes_requested: false, connection: { id: 1, name: 'Claude at home' }, run_id: 1,
    album: { id: 7, current_id: 7, title: 'Autobahn', artist: 'Kraftwerk', folder: 'qobuz/Autobahn' }, group: '', follow_up_of: null, head: 1,
    revisions: [{ revision: 1, author_kind: 'agent', author: 'Claude at home', created_at: '2026-11-09T10:00:00Z' }],
    current: {
      title: 'Dates from MusicBrainz', reason: 'The release has a reissue date.', uncertainties: 'The label', confidence: 86,
      evidence: [{ kind: 'link', text: 'https://musicbrainz.org/release/x' }, { kind: 'note', text: 'Booklet says 1974' }, { kind: 'link', text: 'javascript:alert(1)' }],
      base: [1, 2, 3, 4, 5, 6].map((t) => ({ track_id: t, revision: 'r' })),
      ops: [tag('releasedate', ['originaldate']), tag('albumartists'), tag('originaldate')], preview: sameOnSix(),
    },
    ops: ['releasedate', 'albumartists', 'originaldate'].map((id) => ({ id, type: 'tag' as const, decision: 'pending' as const })),
    summary: { pending: 3, accepted: 0, declined: 0, superseded: 0 }, acceptances: [],
    events: [{ id: 1, kind: 'proposal_created', actor_kind: 'agent', actor: 'Claude at home', at: '2026-11-09T10:00:00Z', data: { ops: ['originaldate', 'releasedate', 'albumartists'] } }],
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  setAuthHandling(() => {}, () => false)
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => vi.unstubAllGlobals())

async function openDetail(over: Record<string, unknown> = {}) {
  stubApi({ '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] }, '/api/inbox/proposals/4': detail(), ...over })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox/4')
  await useInbox().open(4)
  const w = mount(InboxDetail, { global: { plugins: [router] } })
  await flushPromises()
  return w
}

test('the title bar names the album, and labels and talkback use field names, never op IDs', async () => {
  const w = await openDetail()
  expect(w.find('.tb h2').text()).toBe('Autobahn')
  expect(w.text()).not.toContain('Proposal 4')
  expect(w.text()).toContain('Album artists')
  expect(w.text()).not.toContain('albumartists')
  expect(w.text()).toContain('Goes with Original date')
  expect(w.text()).toContain('proposed Original date, Release date, Album artists')
  expect(w.text()).not.toContain('That explains the proposal')
})

test('fields are rockers, and a required field sits above the fields that need it', async () => {
  const w = await openDetail()
  const names = w.findAll('.ib-op .rocker .rk-name').map((n) => n.text())
  expect(names).toEqual(['Original date', 'Release date', 'Album artists'])
  expect(w.findAll('.ib-op input[type="checkbox"]')).toHaveLength(0)
  expect(w.find('.ib-op .rk-state').text()).toBe('All 6 tracks')
})

test('switching off a required field switches off what needs it and says so in field names', async () => {
  const w = await openDetail()
  await w.findAll('.ib-op .rocker')[0]!.trigger('click')
  await flushPromises()
  expect(w.find('.ib-moved').text()).toBe('Release date goes with Original date, so it switched off too.')
  expect(w.findAll('.ib-op .rocker').map((r) => r.attributes('aria-checked'))).toEqual(['false', 'false', 'true'])
})

test("a server refusal for a missing dependency is worded with the fields' names", async () => {
  const w = await openDetail({ '/api/inbox/proposals/4/preview': { preview: null, refusal: { error: 'operation releasedate requires originaldate', reason: 'refused' }, missing: [] } })
  expect(w.find('.bad-text').text()).toBe("Release date goes with Original date, which is no longer pending, so it can't be accepted.")
})

test('evidence is tape, and only a web address opens', async () => {
  const w = await openDetail()
  const links = w.findAll('.ib-tapes a')
  expect(links).toHaveLength(1)
  expect(links[0]!.attributes('href')).toBe('https://musicbrainz.org/release/x')
  expect(links[0]!.attributes('rel')).toContain('noopener')
  expect(w.findAll('.ib-tapes .sug')).toHaveLength(3)
  expect(w.find('.ib-tapes').text()).not.toMatch(/\blink\b|\bnote\b/)
})

test('one reply box: Send comments, or requests changes with the rocker on', async () => {
  const calls = stubApi({ '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] }, '/api/inbox/proposals/4/comments': {}, '/api/inbox/proposals/4/request-changes': {}, '/api/inbox/proposals/4': detail() })
  const router = createAppRouter(createMemoryHistory())
  await useInbox().open(4)
  const w = mount(InboxDetail, { global: { plugins: [router] } })
  await flushPromises()
  expect(w.findAll('textarea.ib-text')).toHaveLength(1)
  await w.find('textarea.ib-text').setValue('Use the 1974 date')
  await w.find('form[aria-label="Reply"]').trigger('submit')
  await flushPromises()
  expect(calls.some((c) => c.url.endsWith('/comments'))).toBe(true)
  await w.find('textarea.ib-text').setValue('Drop the label')
  await w.find('form[aria-label="Reply"] .rocker').trigger('click')
  await w.find('form[aria-label="Reply"]').trigger('submit')
  await flushPromises()
  expect(calls.some((c) => c.url.endsWith('/request-changes'))).toBe(true)
})

test('the review bar counts what Accept writes, and the displays count fields, files and tag writes', async () => {
  const w = await openDetail()
  const bar = w.find('[aria-label="Review"]')
  expect(bar.text()).toContain('Accept writes 3 fields to 6 files. History can undo it.')
  expect(bar.text()).toContain('Accept 3 fields')
  expect(w.findAll('.ib-lcds .lcd').map((l) => l.findAll('span').map((s) => s.text()).join(' '))).toEqual(['3 FIELDS', '6 FILES', '18 TAG WRITES'])
})

test('the diff has one row per tag while every file changes alike, and Show each file expands it', async () => {
  const w = mount(InboxDiff, { props: { preview: sameOnSix(), title: 'What Accept writes' } })
  let rows = w.findAll('.ib-tr:not(.head)')
  expect(rows.map((r) => r.findAll('[role="cell"]').map((c) => c.text()).slice(0, 2))).toEqual([['All 6', 'GENRE'], ['All 6', 'ORIGINALDATE'], ['01.flac', 'TITLE'], ['02.flac', 'TITLE'], ['03.flac', 'TITLE'], ['04.flac', 'TITLE'], ['05.flac', 'TITLE'], ['06.flac', 'TITLE']])
  await w.find('button[aria-pressed]').trigger('click')
  rows = w.findAll('.ib-tr:not(.head)')
  expect(rows).toHaveLength(18)
})

test('warnings are confirmed with rockers', async () => {
  const w = mount(InboxDiff, { props: { preview: sameOnSix(), title: 'x', confirm: [] } })
  const r = w.find('.ib-warn .rocker')
  expect(r.attributes('role')).toBe('switch')
  await r.trigger('click')
  expect(w.emitted('toggle')?.[0]).toEqual(['reissue'])
})

const summary = (over: Partial<ProposalSummary> = {}): ProposalSummary => ({
  id: 4, state: 'pending', changes_requested: false, title: 'Dates', album: { id: 7, current_id: 7, title: 'Autobahn', artist: 'Kraftwerk', folder: 'a' },
  connection: { id: 1, name: 'Claude at home' }, kinds: ['tag'], fields: [{ id: 'albumartists', type: 'tag', field: 'albumartists' }, { id: 'cover', type: 'cover', action: 'replace' }],
  confidence: 86, summary: { pending: 2, accepted: 0, declined: 0, superseded: 0 }, updated_at: '2026-11-09T10:00:00Z', stale: false, ...over,
})

async function listWith(proposals: ProposalSummary[], path = '/inbox') {
  stubApi({ '/api/inbox/proposals': { proposals, next: null }, '/api/inbox': { pending: 1, changes_requested: 0, connections: 1, accounts: 'none', can_create_connections: false } })
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  const inbox = useInbox()
  inbox.setFilters({ state: (router.currentRoute.value.query.state as 'all') ?? 'pending', album: null, kind: '', connection: null })
  const w = mount(InboxList, { global: { plugins: [router] }, attachTo: document.body })
  await flushPromises()
  return w
}

test('a list row shows the cover, the album, the field names and the confidence, and no pill the filter already says', async () => {
  const w = await listWith([summary(), summary({ id: 5, changes_requested: true })])
  const rows = w.findAll('.ib-row')
  expect(rows[0]!.find('img').attributes('src')).toContain('/api/library/albums/7/cover')
  expect(rows[0]!.text()).toContain('Autobahn')
  expect(rows[0]!.text()).toContain('Kraftwerk · Album artists, Replace cover')
  expect(rows[0]!.text()).toContain('86%')
  expect(rows[0]!.find('.pill').exists()).toBe(false)
  expect(rows[1]!.find('.pill').text()).toBe('Changes requested')
  w.unmount()
})

test('the state is a knob, and Agent and Kind are menus, not selects', async () => {
  const w = await listWith([summary()])
  expect(w.find('select').exists()).toBe(false)
  expect(w.find('.knob').exists()).toBe(true)
  const kind = w.findAll('.pick-cap').find((b) => b.text() === 'Tags and covers')!
  await kind.trigger('click')
  expect(w.findAll('[role="menuitemradio"]').map((b) => b.text())).toEqual(['Tags and covers', 'Tags', 'Covers'])
  w.unmount()
})

test('on the phone the dock keeps four tabs and the Library title bar opens the Inbox', async () => {
  vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))
  stubApi({
    '/api/downloads': list([]), '/api/sources': [source('qobuz', 'Qobuz')], '/api/library/scan': { enabled: false }, '/api/telegram': { enabled: false },
    '/api/library/albums': { albums: [], next: null }, '/api/library/filters': { albums: 0, issues: 0, no_mbid: 0, no_source: 0, kinds: [], folders: [], sources: [] }, '/api/library/index': { albums: 0, tracks: 0 },
    '/api/inbox': { pending: 2, changes_requested: 0, connections: 1, accounts: 'none', can_create_connections: false },
  })
  const pinia = createPinia(); setActivePinia(pinia)
  const router = createAppRouter(createMemoryHistory()); await router.push('/library')
  const w = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  expect(w.findAll('.dock a').map((a) => a.text().split(',')[0])).toEqual(['Library', 'Search', 'Downloads', 'Status'])
  const cap = w.find('a.inbox-cap')
  expect(cap.attributes('href')).toBe('/inbox')
  expect(cap.text()).toContain('2 to review')
  expect(w.find('.dock a').text()).toContain('2 proposals to review')
  w.unmount()
})
