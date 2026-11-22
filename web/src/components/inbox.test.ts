import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, onTestFinished, test, vi } from 'vitest'
import { defineComponent, h, type VNode } from 'vue'
import { createMemoryHistory, type RouteLocationNormalizedLoaded, type Router, RouterView } from 'vue-router'
import type { Preview, ProposalDetail, ProposalSummary } from '../api/agents'
import App from '../App.vue'
import { setAuthHandling } from '../api/client'
import { createAppRouter } from '../router'
import { useInbox } from '../stores/inbox'
import { FakeEventSource, list, Reply, source, stubApi } from '../testing/api'
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
      evidence: [{ kind: 'link', text: 'Release credits', url: 'https://musicbrainz.org/release/x' }, { kind: 'note', text: 'Booklet says 1974' }, { kind: 'link', text: 'Unsafe link', url: 'javascript:alert(1)' }],
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

// InboxPage mounts through a RouterView, as in the app, so its route guards
// find their route record. A redirect away from the inbox draws nothing.
const InboxHost = defineComponent(() => () => h(RouterView, null, {
  default: ({ Component, route }: { Component: VNode | undefined; route: RouteLocationNormalizedLoaded }) =>
    route.name === 'inbox' || route.name === 'proposal' ? Component : null,
}))
function mountInboxPage(router: Router, options: { attachTo?: Element } = {}) {
  return mount(InboxHost, { ...options, global: { plugins: [router] } })
}

async function openDetail(over: Record<string, unknown> = {}, phone = false) {
  stubApi({ '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] }, '/api/inbox/proposals/4': detail(), ...over })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox/4')
  await useInbox().open(4)
  const w = mount(InboxDetail, { props: { phone }, global: { plugins: [router] } })
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
  await w.get('#tab-talkback').trigger('click')
  expect(w.get('#panel-talkback').text()).toContain('proposed Original date, Release date, Album artists')
  expect(w.text()).not.toContain('That explains the proposal')
})

test('fields are rockers, and a required field sits above the fields that need it', async () => {
  const w = await openDetail()
  const names = w.findAll('.ib-op [role="switch"]').map((n) => n.attributes('aria-label'))
  expect(names).toEqual(['Album artists', 'Original date', 'Release date'])
  expect(w.findAll('.ib-op input[type="checkbox"]')).toHaveLength(0)
  expect(w.find('.ib-op .rk-state').text()).toBe('All 6 tracks')
})

test('switching off a required field switches off what needs it and says so in field names', async () => {
  const w = await openDetail()
  await w.get('.ib-op [aria-label="Original date"]').trigger('click')
  await flushPromises()
  expect(w.find('.ib-moved').text()).toBe('Release date goes with Original date, so it switched off too.')
  expect(w.findAll('.ib-op .rocker').map((r) => r.attributes('aria-checked'))).toEqual(['true', 'false', 'false'])
})

test("a server refusal for a missing dependency is worded with the fields' names", async () => {
  const w = await openDetail({ '/api/inbox/proposals/4/preview': { preview: null, refusal: { error: 'operation releasedate requires originaldate', reason: 'refused' }, missing: [] } })
  expect(w.find('.bad-text').text()).toBe("Release date goes with Original date, which can no longer be accepted with it.")
})

test('evidence is tape, and only a web address opens', async () => {
  const w = await openDetail()
  await w.get('#tab-brief').trigger('click')
  const links = w.findAll('.ib-tapes a')
  expect(links).toHaveLength(1)
  expect(links[0]!.attributes('href')).toBe('https://musicbrainz.org/release/x')
  expect(links[0]!.text()).toContain('Release credits')
  expect(links[0]!.attributes('rel')).toBe('noopener noreferrer')
  expect(links[0]!.find('.sug-s [aria-hidden="true"]').text()).toBe('↗')
  expect(w.findAll('.ib-tapes span.sug').every((t) => !t.text().includes('↗'))).toBe(true)
  expect(w.findAll('.ib-tapes .sug')).toHaveLength(3)
  expect(w.find('.ib-tapes').text()).not.toMatch(/\blink\b|\bnote\b/)
})

test('evidence that names a field uses the editor word, not the field ID', async () => {
  const d = detail()
  d.current.evidence = [{ kind: 'suggestion', source: 'musicbrainz', field: 'originaldate' }, { kind: 'suggestion', source: 'file', key: 'ORIGINALYEAR' }]
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  await w.get('#tab-brief').trigger('click')
  const tapes = w.findAll('.ib-tapes .sug-v').map((t) => t.text())
  expect(tapes).toEqual(['Original date', 'ORIGINALYEAR'])
})

test('one reply box: Send comments, or requests changes with the rocker on', async () => {
  const calls = stubApi({ '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] }, '/api/inbox/proposals/4/comments': {}, '/api/inbox/proposals/4/request-changes': {}, '/api/inbox/proposals/4': detail() })
  const router = createAppRouter(createMemoryHistory())
  await useInbox().open(4)
  const w = mount(InboxDetail, { global: { plugins: [router] } })
  await flushPromises()
  expect(w.findAll('textarea.ib-text')).toHaveLength(1)
  expect(w.find('button[type="submit"]').text()).toBe('Send comment')
  // The key keeps the longer label's width, so the rocker beside it doesn't move.
  expect(w.find('button[type="submit"]').attributes('data-wide')).toBe('Ask for changes')
  await w.find('textarea.ib-text').setValue('Use the 1974 date')
  await w.find('form[aria-label="Reply"]').trigger('submit')
  await flushPromises()
  expect(calls.some((c) => c.url.endsWith('/comments'))).toBe(true)
  await w.find('textarea.ib-text').setValue('Drop the label')
  await w.find('form[aria-label="Reply"] .rocker').trigger('click')
  expect(w.find('button[type="submit"]').text()).toBe('Ask for changes')
  await w.find('form[aria-label="Reply"]').trigger('submit')
  await flushPromises()
  expect(calls.some((c) => c.url.endsWith('/request-changes'))).toBe(true)
})

test('the review bar counts what Accept writes, and the displays count fields, files and tag writes', async () => {
  const w = await openDetail()
  const bar = w.find('[aria-label="Review"]')
  await w.get('#tab-files').trigger('click')
  expect(bar.text()).toContain('3 fields · 6 files')
  expect(bar.text()).not.toContain('Accept writes')
  expect(bar.text()).toContain('Accept 3 fields')
  expect(w.findAll('.ib-lcds .lcd').map((l) => l.findAll(':scope > span').map((s) => s.text()).join(' '))).toEqual(['3 FIELDS', '6 FILES', '18 TAG WRITES'])
})

test('the diff has one row per tag while every file changes alike, and Show each file expands it', async () => {
  const w = mount(InboxDiff, { props: { preview: sameOnSix(), title: 'What Accept writes' } })
  let rows = w.findAll('.ib-tr:not(.head)')
  expect(rows.map((r) => r.findAll('.ib-cell-v').map((c) => c.text()))).toEqual([['All 6', 'GENRE'], ['All 6', 'ORIGINALDATE'], ['01.flac', 'TITLE'], ['02.flac', 'TITLE'], ['03.flac', 'TITLE'], ['04.flac', 'TITLE'], ['05.flac', 'TITLE'], ['06.flac', 'TITLE']])
  await w.find('button[aria-pressed]').trigger('click')
  rows = w.findAll('.ib-tr:not(.head)')
  expect(rows).toHaveLength(18)
})

test('the rename warning is confirmed from the review bar on either tab or layout', async () => {
  for (const phone of [false, true]) {
    const preview = { ...sameOnSix(), warnings: [{ kind: 'album_artist', message: 'This renames the album artist in Navidrome.' }] }
    const over = { '/api/inbox/proposals/4/preview': { preview, refusal: null, missing: ['album_artist'] } }
    const w = phone ? await openOnPhone(over) : await openDetail(over)
    const bar = w.get('[aria-label="Review"]')
    const rocker = bar.get('.ib-warn [role="switch"]')
    expect(bar.get('.sun').attributes('disabled')).toBeDefined()
    expect(rocker.attributes('aria-checked')).toBe('false')
    await w.get('#tab-brief').trigger('click')
    expect(rocker.isVisible()).toBe(true)
    await rocker.trigger('click')
    expect(rocker.attributes('aria-checked')).toBe('true')
    expect(bar.get('.sun').attributes('disabled')).toBeUndefined()
    w.unmount()
    useInbox().close()
  }
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
    '/api/library/albums': { albums: [], next: null }, '/api/library/filters': { albums: 0, issues: 0, no_mbid: 0, no_source: 0, kinds: [], folders: [], sources: [], types: [] }, '/api/library/index': { albums: 0, tracks: 0 },
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

test('on the desktop the Inbox fits the window like the Library, and Memory scrolls as one page', async () => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }))
  stubApi({
    '/api/downloads': list([]), '/api/sources': [source('qobuz', 'Qobuz')], '/api/library/scan': { enabled: false }, '/api/telegram': { enabled: false },
    '/api/inbox/proposals': { proposals: [], next: null }, '/api/inbox/memory': { body: '', updated_at: null },
    '/api/inbox': { pending: 0, changes_requested: 0, connections: 1, accounts: 'none', can_create_connections: false }, '/api/agents': { connections: [] },
  })
  const pinia = createPinia(); setActivePinia(pinia)
  const router = createAppRouter(createMemoryHistory()); await router.push('/inbox')
  const w = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  expect(w.find('.rs').classes()).toContain('shell-fit')
  await router.push('/inbox/memory')
  await flushPromises()
  expect(w.find('.rs').classes()).not.toContain('shell-fit')
  w.unmount()
})


test('evidence without a label shows its URL and never links non-web schemes', async () => {
  const d = detail()
  d.current.evidence = [
    { kind: 'link', url: 'http://example.com/credits' },
    { kind: 'link', url: 'data:text/html,unsafe' },
    { kind: 'link', url: '//example.com/credits' },
    { kind: 'link', text: 'https://example.com/label-only' },
  ]
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  await w.get('#tab-brief').trigger('click')
  const links = w.findAll('.ib-tapes a')
  expect(links).toHaveLength(1)
  expect(links[0]!.attributes('href')).toBe('http://example.com/credits')
  expect(links[0]!.text()).toContain('http://example.com/credits')
  expect(w.find('.ib-tapes').text()).toContain('data:text/html,unsafe')
})

// withBefore gives the detail the values the files hold now, as the server's live part does.
function withBefore(before: Record<string, Record<string, string[]>>): ProposalDetail {
  const d = detail()
  d.live = { preview: sameOnSix(), op_stale: {}, refusal: null, before }
  return d
}
const six = (v: string[]) => Object.fromEntries([1, 2, 3, 4, 5, 6].map((t) => [String(t), v]))

test('each proposed value says what the files hold now', async () => {
  const before = { originaldate: six([]), releasedate: six(['2012']), albumartists: { ...six(['Kraftwerk']), 3: ['Ralf & Florian'] } }
  const w = await openDetail({ '/api/inbox/proposals/4': withBefore(before) })
  expect(w.findAll('.ib-op .lf-was').map((e) => e.text())).toEqual(['Was different on each track', 'Was empty', 'Was 2012'])
  const row = w.get('[data-op="albumartists"]')
  await row.get('.ib-label').trigger('click')
  await row.findAll('button').find((b) => b.text() === 'Per track')!.trigger('click')
  await row.findAll('button').find((b) => b.text() === 'Show 1 more track')!.trigger('click')
  const perTrack = row.findAll('.ib-op-val .lf-was').map((e) => e.text())
  expect(perTrack).toHaveLength(6)
  expect(perTrack[2]).toBe('Was Ralf & Florian')
})

test('without the values now, no Was line shows', async () => {
  const w = await openDetail()
  expect(w.findAll('.lf-was')).toHaveLength(0)
})

test('a patch cable joins the fields requires links, named for screen readers', async () => {
  const w = await openDetail()
  const cable = w.find('.ib-bank svg.ib-cable')
  expect(cable.attributes('role')).toBe('img')
  expect(cable.attributes('aria-label')).toBe('Original date and Release date go together')
  expect(w.get('#ib-bank-release').classes()).toContain('cabled')
  expect(w.find('.ib-plug').exists()).toBe(false)
  expect(w.findAll('.ib-op .sr').map((s) => s.text())).toEqual(['Goes with Release date. They switch on and off together.', 'Goes with Original date. They switch on and off together.'])
})

test('a proposal with no requires draws no cable', async () => {
  const d = detail()
  d.current.ops = d.current.ops.map((op) => ({ ...op, requires: undefined }))
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  expect(w.find('svg.ib-cable').exists()).toBe(false)
  expect(w.find('.ib-bank').classes()).not.toContain('cabled')
})

async function openOnPhone(over: Record<string, unknown> = {}) {
  vi.stubGlobal('matchMedia', (q: string) => ({ matches: q.includes('max-width'), addEventListener() {}, removeEventListener() {} }))
  stubApi({
    '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] }, '/api/inbox/proposals/4': withBefore({ originaldate: six([]) }),
    '/api/inbox/proposals': { proposals: [], next: null }, '/api/inbox': { pending: 1, changes_requested: 0, connections: 1, accounts: 'none', can_create_connections: false },
    '/api/agents': { connections: [] }, ...over,
  })
  const router = createAppRouter(createMemoryHistory()); await router.push('/inbox/4')
  const w = mountInboxPage(router)
  await flushPromises()
  return w
}

test('on the phone the review bar is a slot above the dock, outside the proposal window', async () => {
  const w = await openOnPhone()
  const slot = w.find('.stack > section.win.slot[aria-label="Review"]')
  expect(slot.text()).toContain('3 fields · 6 files')
  expect(slot.findAll('button.key').map((b) => b.text())).toEqual(['Accept 3'])
  expect(w.find('.ib-detail [aria-label="Review"]').exists()).toBe(false)
  // Decline shows once a field is off, and declines the fields left off.
  await w.get('.ib-op [aria-label="Album artists"]').trigger('click')
  await flushPromises()
  expect(w.find('.stack > section.win.slot').findAll('button.key').map((b) => b.text())).toEqual(['Decline 1', 'Accept 2'])
})

test('on the phone a field row shows the new value, and pressing its label opens the field', async () => {
  const w = await openOnPhone()
  const row = w.get('[data-op="originaldate"]')
  expect(row.find('.rocker').attributes('aria-label')).toBe('Original date')
  expect(row.find('.ib-label .rk-state').text()).toBe('x, was empty')
  expect(row.find('.ib-op-val').exists()).toBe(false)
  await row.find('.ib-label').trigger('click')
  expect(row.find('.ib-label').attributes('aria-expanded')).toBe('true')
  expect(row.find('.ib-op-val input').exists()).toBe(true)
})

test('after Accept the bar shows the outcome, the files and the change in History', async () => {
  const d = detail()
  d.acceptances = [{ id: 9, revision: 1, ops: ['originaldate', 'releasedate'], change_id: 31, author: 'alice', created_at: '2026-11-09T10:00:00Z', outcome: { state: 'done', files: 6, written: 6 } } as never]
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  useInbox().lastAccepted = { acceptance: { id: 9 }, change: {} } as never
  await flushPromises()
  const out = w.find('[aria-label="Review"] .ib-out')
  expect(out.find('.pill').text()).toBe('Written')
  expect(out.text()).toContain('6 of 6 files written')
  expect(out.text()).toContain('Change 31 in History')
})

test('on the phone the change links to its album\'s History', async () => {
  const d = detail()
  d.acceptances = [{ id: 9, revision: 1, ops: ['originaldate', 'releasedate'], change_id: 31, author: 'alice', created_at: '2026-11-09T10:00:00Z', outcome: { state: 'done', files: 6, written: 6 } } as never]
  const w = await openDetail({ '/api/inbox/proposals/4': d }, true)
  const link = w.findAll('a').find((a) => a.text() === 'Change 31 in History')!
  expect(link.attributes('href')).toBe('/library/7?view=history&change=31')
  expect(w.text()).not.toContain('on a desktop')
})

test('on the phone a change whose album left the index is named without a link', async () => {
  const d = detail()
  d.album = { ...d.album, current_id: 0 }
  d.acceptances = [{ id: 9, revision: 1, ops: ['originaldate', 'releasedate'], change_id: 31, author: 'alice', created_at: '2026-11-09T10:00:00Z', outcome: { state: 'done', files: 6, written: 6 } } as never]
  const w = await openDetail({ '/api/inbox/proposals/4': d }, true)
  expect(w.text()).toContain('Change 31 is in History on a desktop.')
  expect(w.findAll('a').some((a) => (a.attributes('href') ?? '').includes('history'))).toBe(false)
})


test('the desktop proposal divider resizes, clamps and remembers the list width', async () => {
  localStorage.removeItem('inbox.list')
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }))
  stubApi({
    '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] }, '/api/inbox/proposals/4': detail(),
    '/api/inbox/proposals': { proposals: [], next: null }, '/api/inbox': { pending: 1, changes_requested: 0, connections: 1, accounts: 'none', can_create_connections: false },
    '/api/agents': { connections: [] },
  })
  const router = createAppRouter(createMemoryHistory()); await router.push('/inbox')
  const w = mountInboxPage(router)
  await flushPromises()
  expect(w.find('.lib-split').exists()).toBe(false)
  await router.push('/inbox/4')
  await flushPromises()
  const split = w.find('.lib-split')
  expect(split.attributes('aria-label')).toContain('proposal list')
  // As in the Library, both windows are direct children of the row and scroll by themselves, so the divider is as tall as the page.
  expect(w.findAll('main.lib-main > .win').map((e) => e.classes()).map((c) => c.includes('ib-list') || c.includes('ib-main'))).toEqual([true, true])
  const width = () => (w.find('.ib-page').element as HTMLElement).style.getPropertyValue('--list-w')
  expect(width()).toBe('420px')
  await split.trigger('keydown', { key: 'ArrowRight' })
  expect(width()).toBe('440px')
  expect(localStorage.getItem('inbox.list')).toBe('440')
  for (let n = 0; n < 12; n++) await split.trigger('keydown', { key: 'ArrowRight' })
  expect(width()).toBe('640px')
  expect(localStorage.getItem('inbox.list')).toBe('640')
  await split.trigger('keydown', { key: 'ArrowLeft' })
  expect(width()).toBe('620px')
  // A drag keeps the pointer from the browser's own drag, which would cancel it.
  const down = new MouseEvent('pointerdown', { cancelable: true, clientX: 600 })
  split.element.dispatchEvent(down)
  expect(down.defaultPrevented).toBe(true)
  split.element.dispatchEvent(new MouseEvent('pointermove', { clientX: 560 }))
  await flushPromises()
  expect(width()).toBe('580px')
  await split.trigger('pointerup')
  w.unmount()
  const restored = mountInboxPage(router)
  await flushPromises()
  expect((restored.find('.ib-page').element as HTMLElement).style.getPropertyValue('--list-w')).toBe('580px')
  restored.unmount()
  localStorage.removeItem('inbox.list')
})

test('help notes sit on names and keys, never at rest, and open 350ms after the pointer arrives', async () => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  const root = document.createElement('div')
  root.className = 'rs'
  document.body.appendChild(root)
  try {
    const w = await openDetail()
    const tips = () => w.findAll('.has-tip').map((t) => t.text())
    expect(tips()).toEqual(expect.arrayContaining(['Open album', 'revision 1', '86%', 'Not sure', 'Release credits', 'Release date', 'Original date', 'TAG WRITES', 'Talkback', 'Ask for changes', 'Accept 3 fields']))
    // Only the first tape carries the evidence note.
    expect(tips().filter((t) => t === 'Booklet says 1974')).toEqual([])
    expect(w.find('.ib-bank > .has-tip svg.ib-cable').exists()).toBe(true)
    expect(document.querySelector('.tipbox')).toBeNull()
    const accept = w.findAll('.has-tip').find((t) => t.text() === 'Accept 3 fields')!
    // A tap on the phone opens no note.
    await accept.trigger('pointerenter', { pointerType: 'touch' })
    await vi.advanceTimersByTimeAsync(400)
    expect(document.querySelector('.tipbox')).toBeNull()
    await accept.trigger('pointerenter', { pointerType: 'mouse' })
    await vi.advanceTimersByTimeAsync(300)
    expect(document.querySelector('.tipbox')).toBeNull()
    await vi.advanceTimersByTimeAsync(60)
    expect(document.querySelector('.tipbox')?.textContent).toContain('Writes the fields that are on as one change, which History can undo.')
    await accept.trigger('pointerleave', { pointerType: 'mouse' })
    expect(document.querySelector('.tipbox')).toBeNull()
    w.unmount()
  } finally {
    vi.useRealTimers()
    root.remove()
  }
})

test('on the phone no name carries a note, and the ? cap opens the page note above the review slot', async () => {
  const w = await openOnPhone()
  expect(w.find('.ib-detail .has-tip').exists()).toBe(false)
  expect(w.find('[role="note"].slot').exists()).toBe(false)
  const cap = w.find('button.wbtn.q')
  expect(cap.attributes('aria-pressed')).toBe('false')
  await cap.trigger('click')
  const slots = w.findAll('.stack > .slot')
  expect(slots.map((s) => s.attributes('role'))).toEqual(['note', 'region'])
  expect(slots[0]!.text()).toContain('Review')
  expect(slots[0]!.text()).toContain('The red cable joins fields that switch on and off together.')
  expect(slots[0]!.text()).toContain("86% is the agent's own confidence.")
  expect(cap.attributes('aria-pressed')).toBe('true')
  await slots[0]!.find('button[aria-label="Close the note"]').trigger('click')
  expect(w.find('[role="note"].slot').exists()).toBe(false)
  w.unmount()
})

test('fields the files already hold are hidden, stay on, and count nowhere', async () => {
  const d = detail()
  d.live = { preview: sameOnSix(), op_stale: { releasedate: true }, op_unchanged: { albumartists: true }, refusal: null }
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  expect(w.findAll('.ib-op').map((e) => e.attributes('data-op'))).toEqual(['originaldate', 'releasedate'])
  expect(w.find('.ib-op[data-op="releasedate"]').findAll('.pill').map((p) => p.text())).toEqual(['Changed since proposed'])
  expect(w.find('.ib-held').text()).toBe('1 field the files already hold is hidden. Accept marks it accepted too.')
  const inbox = useInbox()
  expect([...inbox.selected].sort()).toEqual(['albumartists', 'originaldate', 'releasedate'])
  expect(inbox.shown).toHaveLength(2)
  expect(inbox.writes?.fields).toBe(2)
  inbox.selectAll(false)
  expect(inbox.selected).toEqual(['albumartists'])
  expect([...inbox.rest].sort()).toEqual(['originaldate', 'releasedate'])
})

test('Fields opens first and a collapsed group keeps its switch and inline editor state', async () => {
  const w = await openDetail()
  expect(w.get('#tab-fields').attributes('aria-selected')).toBe('true')
  expect(w.get('#panel-fields').isVisible()).toBe(true)
  expect(w.get('#panel-files').attributes('style')).toContain('display: none')
  expect(w.get('#panel-brief').attributes('style')).toContain('display: none')
  expect(w.get('#panel-talkback').attributes('style')).toContain('display: none')
  const fold = w.get('#ib-group-release')
  const group = w.get('[aria-labelledby="ib-group-release"]')
  const row = group.get('[data-op="originaldate"]')
  expect(row.find('input').exists()).toBe(false)
  await row.get('.ib-label').trigger('click')
  await fold.trigger('click')
  expect(fold.attributes('aria-expanded')).toBe('false')
  expect(group.get('#ib-bank-release').attributes('style')).toContain('display: none')
  const toggle = group.get('.lib-h [role="switch"]')
  await toggle.trigger('click')
  expect(toggle.isVisible()).toBe(true)
  expect(toggle.attributes('aria-checked')).toBe('false')
  expect(w.get('[data-op="albumartists"] [role="switch"]').attributes('aria-checked')).toBe('true')
  await fold.trigger('click')
  expect(row.get('input').isVisible()).toBe(true)
  await w.get('#tab-fields').trigger('keydown', { key: 'ArrowRight' })
  expect(w.get('#tab-brief').attributes('aria-selected')).toBe('true')
  expect(w.get('#panel-brief').isVisible()).toBe(true)
  expect(w.get('[aria-label="Review"]').isVisible()).toBe(true)
})

test('Files uses the selected physical preview and counts embedded and folder artwork separately', async () => {
  const d = detail()
  d.current.ops.push({ id: 'cover', type: 'cover', action: 'replace', image: 'new' })
  d.ops.push({ id: 'cover', type: 'cover', decision: 'pending' })
  d.summary.pending++
  const preview = {
    ...sameOnSix(),
    files: [{ ...file(1, []), cover_before: 'old', cover_after: 'new' }],
    folder: [{ path: 'a/cover.jpg', before: 'small', after: 'large' }],
    warnings: [],
  }
  const w = await openDetail({ '/api/inbox/proposals/4': d, '/api/inbox/proposals/4/preview': { preview, refusal: null, missing: [] } })
  await w.get('#tab-files').trigger('click')
  const files = w.get('#panel-files')
  expect(files.get('.ib-lcds').text()).toMatch(/4\s*FIELDS1\s*FILE0\s*TAG WRITES/)
  expect(files.text()).not.toContain('GENRE')
  expect(files.get('[aria-label="Embedded artwork"]').text()).toContain('01.flac')
  expect(files.get('[aria-label="Folder artwork"]').text()).toContain('cover.jpg')
  expect(w.get('[aria-label="Review"]').text()).toContain('1 folder artwork change')
})

test('a changed preview blocks phone acceptance until the reviewer acknowledges the updated Files tab', async () => {
  const updated = { ...sameOnSix(), files: [file(1, [{ key: 'DATE', before: ['2012'], after: ['2023'] }])], warnings: [], digest: 'changed' }
  const seen = new Set<string>()
  const w = await openOnPhone({
    '/api/inbox/proposals/4/preview': { preview: { ...sameOnSix(), warnings: [] }, refusal: null, missing: [] },
    '/api/inbox/proposals/4/accept': (_url: string, init?: RequestInit) => {
      const { digest } = JSON.parse(init!.body as string)
      seen.add(digest)
      return new Reply(409, { code: 'preview_changed', preview: updated })
    },
  })
  const review = w.get('[aria-label="Review"]')
  await review.get('.sun').trigger('click')
  await flushPromises()
  expect(review.get('.sun').attributes('disabled')).toBeDefined()
  expect(seen).toEqual(new Set(['d']))
  await review.get('button.link').trigger('click')
  expect(w.get('#tab-files').attributes('aria-selected')).toBe('true')
  const files = w.get('#panel-files')
  expect(files.text()).toContain('2023')
  await files.findAll('button').find((b) => b.text() === 'I reviewed the updated changes')!.trigger('click')
  expect(review.get('.sun').attributes('disabled')).toBeUndefined()
  await review.get('.sun').trigger('click')
  await flushPromises()
  expect(seen).toEqual(new Set(['d', 'changed']))
})

test('a phone editor preserves ordered per-track values, artwork scope, discard and saving a revision', async () => {
  const d = detail()
  d.current.ops.push({ id: 'cover', type: 'cover', action: 'replace', image: 'new' })
  d.ops.push({ id: 'cover', type: 'cover', decision: 'pending' })
  d.summary.pending++
  const submitted: unknown[] = []
  const w = await openOnPhone({
    '/api/inbox/proposals/4': d,
    '/api/inbox/proposals/4/revisions': (_url: string, init?: RequestInit) => {
      submitted.push(JSON.parse(init!.body as string))
      return { ...d, head: 2 }
    },
  })
  const row = w.get('[data-op="albumartists"]')
  await row.get('.ib-label').trigger('click')
  await row.findAll('button').find((b) => b.text() === 'Per track')!.trigger('click')
  const track = row.get('.ib-track')
  await track.get('input').setValue('A, B')
  await track.get('.lf-add').trigger('click')
  await track.findAll('input')[1]!.setValue('C; D')
  await row.findAll('.ib-track')[1]!.get('button.link').trigger('click')
  const cover = w.get('[data-op="cover"]')
  await cover.get('.ib-label').trigger('click')
  const scope = cover.get('.ib-op-val [role="switch"]')
  await scope.trigger('click')
  await w.get('#tab-brief').trigger('click')
  expect(w.get('[aria-label="Unsaved values"]').isVisible()).toBe(true)
  await w.get('[aria-label="Unsaved values"] button.key').trigger('click')
  await w.get('#tab-fields').trigger('click')
  expect((track.get('input').element as HTMLInputElement).value).toBe('x')
  expect(scope.attributes('aria-checked')).toBe('false')
  await track.get('input').setValue('A, B')
  await track.get('.lf-add').trigger('click')
  await track.findAll('input')[1]!.setValue('C; D')
  await row.findAll('.ib-track')[1]!.get('button.link').trigger('click')
  await scope.trigger('click')
  await w.get('[aria-label="Unsaved values"] .sun').trigger('click')
  await flushPromises()
  expect(submitted).toEqual([{ parent_revision: 1, ops: [
    { id: 'albumartists', tracks: d.current.ops[1]!.type === 'tag' ? d.current.ops[1]!.tracks.filter((t) => t.track_id !== 2).map((t) => ({ ...t, values: t.track_id === 1 ? ['A, B', 'C; D'] : t.values })) : [] },
    { id: 'cover', every_track: true },
  ] }])
  expect(w.get('[aria-label="Review"]').text()).toContain('Saved as revision 2')
})

test('a subset retry keeps untouched accepted fields retryable, records only the completed subset, and retains its original failure in Talkback', async () => {
  const d = detail()
  d.state = 'reviewed'
  d.summary = { pending: 0, accepted: 2, declined: 1, superseded: 0 }
  d.ops = [
    { id: 'originaldate', decision: 'accepted', type: 'tag', acceptance: 8 },
    { id: 'releasedate', decision: 'accepted', type: 'tag', acceptance: 8 },
    { id: 'albumartists', decision: 'declined', type: 'tag' },
  ]
  const done = { ...sameOnSix(), files: [file(1, [{ key: 'ORIGINALDATE', before: [], after: ['1974'] }])], warnings: [], digest: 'written' }
  d.acceptances = [{ id: 8, revision: 1, ops: ['originaldate', 'releasedate'], change_id: 8, author: 'alice', created_at: '2026-11-09T10:00:00Z', preview: sameOnSix(), outcome: { state: 'failed', files: 6, written: 0, error: 'The source file changed.' } }]
  const accepted = { id: 9, revision: 1, ops: ['originaldate'], change_id: 9, author: 'alice', created_at: '2026-11-09T11:00:00Z', preview: done, outcome: { state: 'done' as const, files: 1, written: 1 } }
  const tried: string[][] = []
  let detailAfter = d
  const w = await openDetail({
    '/api/inbox/proposals/4': () => detailAfter,
    '/api/inbox/proposals/4/preview': { preview: { ...done, digest: 'planned' }, refusal: null, missing: [] },
    '/api/inbox/proposals/4/accept': (_url: string, init?: RequestInit) => {
      tried.push(JSON.parse(init!.body as string).ops)
      detailAfter = { ...d, ops: d.ops.map((o) => o.id === 'originaldate' ? { ...o, acceptance: 9 } : o), acceptances: [...d.acceptances, accepted] }
      return { acceptance: accepted, change: {} }
    },
  })
  await w.get('[data-op="releasedate"] [role="switch"]').trigger('click')
  await flushPromises()
  expect(w.find('.ib-review-actions').text()).toBe('Accept again 1 field')
  await w.get('.ib-review-actions .sun').trigger('click')
  await flushPromises()
  expect(tried).toEqual([['originaldate']])
  await w.get('#panel-files select').setValue('9')
  const files = w.get('#panel-files')
  expect(files.text()).toContain('What Accept wrote')
  expect(files.text()).toContain('ORIGINALDATE')
  expect(files.text()).not.toContain('GENRE')
  await w.get('#tab-fields').trigger('click')
  expect(useInbox().detail?.ops.find((o) => o.id === 'releasedate')).toMatchObject({ decision: 'accepted', acceptance: 8 })
  const remaining = w.get('[data-op="releasedate"] [role="switch"]')
  expect(remaining.isVisible()).toBe(true)
  await remaining.trigger('click')
  await flushPromises()
  expect(w.get('.ib-review-actions').text()).toBe('Accept again 1 field')
  expect(useInbox().declineCount).toBe(0)
  await w.get('#tab-talkback').trigger('click')
  expect(w.get('#panel-talkback').text()).toContain('The source file changed.')
  expect(w.get('#panel-talkback').text()).toContain('0 of 6 files written')
})

test('groups switch dependencies across headers and a linked field opens its collapsed group', async () => {
  const d = detail()
  d.current.ops[2]!.requires = ['albumartists']
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  const artists = w.get('#ib-group-artists')
  await artists.trigger('click')
  await w.get('[aria-label="All on in Artists"]').trigger('click')
  expect(useInbox().shown).toEqual([])
  expect(w.get('.ib-moved').text()).toContain('Release date, Original date go with Artists')
  await w.get('[aria-label="All on in Release"]').trigger('click')
  expect(useInbox().shown).toHaveLength(3)
  const original = w.get('[data-op="originaldate"]')
  await original.get('.ib-label').trigger('click')
  await original.get('.ib-op-val button.link').trigger('click')
  await flushPromises()
  expect(artists.attributes('aria-expanded')).toBe('true')
  expect(w.get('[data-op="albumartists"] .ib-label').attributes('aria-expanded')).toBe('true')
})

test('the phone decline form submits only pending fields left off and keeps the reason', async () => {
  const requests: unknown[] = []
  const w = await openOnPhone({
    '/api/inbox/proposals/4/decline': (_url: string, init?: RequestInit) => {
      requests.push(JSON.parse(init!.body as string))
      return {}
    },
  })
  await w.get('[data-op="albumartists"] [role="switch"]').trigger('click')
  await flushPromises()
  const bar = w.get('.stack > section.win.slot[aria-label="Review"]')
  await bar.findAll('button.key').find((b) => b.text() === 'Decline 1')!.trigger('click')
  await bar.get('input').setValue('Use the original credit.')
  await bar.get('form').trigger('submit')
  await flushPromises()
  expect(requests).toEqual([{ revision: 1, ops: ['albumartists'], reason: 'Use the original credit.' }])
  expect(bar.find('input').exists()).toBe(false)
})

test('Files can show the proposal fields written by a partial library Save', async () => {
  const d = detail()
  d.state = 'reviewed'
  d.ops = d.ops.map((o) => ({ ...o, decision: o.id === 'originaldate' ? 'accepted' : 'declined' }))
  d.summary = { pending: 0, accepted: 1, declined: 2, superseded: 0 }
  const preview = { ...sameOnSix(), files: [file(1, [{ key: 'ORIGINALDATE', before: [], after: ['1974'] }])], warnings: [] }
  d.acceptances = [{ id: 8, revision: 1, ops: ['originaldate'], change_id: 8, author: 'alice', created_at: '2026-11-09T10:00:00Z', preview, outcome: { state: 'partial', files: 2, written: 1 }, via_save: true }]
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  await w.get('#tab-files').trigger('click')
  const files = w.get('#panel-files')
  expect(files.text()).toContain('Proposal fields written by Save')
  expect(files.text()).toContain('ORIGINALDATE')
  expect(files.text()).not.toContain('GENRE')
})

test('typing through another group’s value keeps the edit on its original tracks', async () => {
  const d = detail()
  const op = d.current.ops[1]!
  if (op.type !== 'tag') throw new Error('Expected a tag operation')
  op.tracks.forEach((t) => { t.values = [t.track_id <= 2 ? 'Pop' : 'Rock'] })
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  const row = w.get('[data-op="albumartists"]')
  await row.get('.ib-label').trigger('click')
  const first = row.get('input')
  await first.setValue('Rock')
  expect(row.findAll('input')).toHaveLength(2)
  await first.setValue('Rockabilly')
  expect(useInbox().revisionOps()[0]?.tracks).toEqual(op.tracks.map((t) => ({
    ...t, values: [t.track_id <= 2 ? 'Rockabilly' : 'Rock'],
  })))
})

test('Files shows the completed snapshot after another reviewer accepts the remaining fields', async () => {
  const d = detail()
  const preview = { ...sameOnSix(), warnings: [] }
  const w = await openDetail({
    '/api/inbox/proposals/4': () => structuredClone(d),
    '/api/inbox/proposals/4/preview': { preview, refusal: null, missing: [] },
    '/api/inbox/proposals/4/accept': () => {
      d.state = 'reviewed'
      d.summary = { ...d.summary, pending: 0, accepted: d.ops.length }
      d.ops.forEach((op) => { op.decision = 'accepted'; op.acceptance = 9 })
      d.acceptances = [{ id: 9, revision: 1, ops: d.ops.map((op) => op.id), change_id: 9, author: 'alice', created_at: '2026-11-09T11:00:00Z', preview, outcome: { state: 'done' } }]
      return new Reply(409, { code: 'decided_op' })
    },
  })
  await w.get('.ib-review-actions .sun').trigger('click')
  await flushPromises()
  await w.get('#tab-files').trigger('click')
  expect(w.get<HTMLSelectElement>('#panel-files select').element.value).toBe('9')
  expect(w.get('#panel-files').text()).toContain('What Accept wrote')
})

test('Decline snapshots revision and ops, requires reconfirmation on scope or revision change, submits on Enter, and keeps reason on conflict', async () => {
  const d = detail()
  let failOnce = true
  const requests: unknown[] = []
  const w = await openDetail({
    '/api/inbox/proposals/4': () => structuredClone(d),
    '/api/inbox/proposals/4/decline': (_url: string, init?: RequestInit) => {
      requests.push(JSON.parse(init!.body as string))
      if (failOnce) {
        failOnce = false
        d.head = 2
        return new Reply(409, { code: 'stale_revision', error: 'New revision' })
      }
      return {}
    },
  })

  await w.get('[data-op="albumartists"] [role="switch"]').trigger('click')
  await flushPromises()
  const bar = w.get('[aria-label="Review"]')
  await bar.findAll('button.key').find((b) => b.text().includes('Decline the rest'))!.trigger('click')

  expect(bar.find('form.ib-decline-form').exists()).toBe(true)
  expect(bar.text()).toContain('Declining: Album artists (revision 1)')
  await bar.get('input').setValue('Keep original credit')

  // Toggling another field while Decline is open blocks submit until reconfirmed.
  await w.get('[data-op="releasedate"] [role="switch"]').trigger('click')
  await flushPromises()
  expect(bar.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  expect(bar.text()).toContain('The fields to decline changed while this form was open.')
  await bar.get('form.ib-decline-form').trigger('submit')
  await flushPromises()
  expect(requests).toHaveLength(0)

  // Updating the snapshot keeps the typed reason and allows submitting via Enter / form submit.
  await bar.findAll('button.link').find((b) => b.text().includes('Update to current 2 fields'))!.trigger('click')
  expect((bar.get('input').element as HTMLInputElement).value).toBe('Keep original credit')
  expect(bar.text()).toContain('Declining: Album artists, Release date (revision 1)')
  await bar.get('form.ib-decline-form').trigger('submit')
  await flushPromises()

  // Server returned 409 stale_revision: reason is preserved and reconfirmation is required for revision 2.
  expect(requests).toEqual([{ revision: 1, ops: ['albumartists', 'releasedate'], reason: 'Keep original credit' }])
  expect(bar.find('form.ib-decline-form').exists()).toBe(true)
  expect((bar.get('input').element as HTMLInputElement).value).toBe('Keep original credit')
  expect(bar.text()).toContain('Revision 2 arrived while Decline was open.')
  expect(bar.get('button[type="submit"]').attributes('disabled')).toBeDefined()

  await bar.findAll('button.link').find((b) => b.text().includes('Update to current 2 fields'))!.trigger('click')
  await bar.get('form.ib-decline-form').trigger('submit')
  await flushPromises()
  expect(requests[1]).toEqual({ revision: 2, ops: ['albumartists', 'releasedate'], reason: 'Keep original credit' })
  expect(bar.find('form.ib-decline-form').exists()).toBe(false)
})

test('live refresh dropping an edited field shows a dismissible notice with the draft text', async () => {
  const d = detail()
  const w = await openDetail({ '/api/inbox/proposals/4': () => structuredClone(d) })
  const row = w.get('[data-op="albumartists"]')
  await row.get('.ib-label').trigger('click')
  await row.get('input').setValue('Kraftwerk & Guests')

  d.head = 2
  d.ops.find((o) => o.id === 'albumartists')!.decision = 'declined'
  await useInbox().loadDetail()
  await flushPromises()

  const dropped = w.get('.ib-dropped')
  expect(dropped.text()).toContain('Album artists')
  expect(dropped.text()).toContain('declined')
  expect(dropped.text()).toContain('Kraftwerk & Guests')
  await dropped.get('button.link').trigger('click')
  expect(w.find('.ib-dropped').exists()).toBe(false)
})

async function openInboxApp(extra: Record<string, unknown> = {}) {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }))
  const other = { ...detail(), id: 5, url: '/inbox/5', album: { ...detail().album, title: 'Trans-Europe Express' } }
  stubApi({
    '/api/downloads': list([]), '/api/sources': [source('qobuz', 'Qobuz')], '/api/library/scan': { enabled: false }, '/api/telegram': { enabled: false },
    '/api/library/albums': { albums: [], next: null }, '/api/library/filters': { albums: 0, issues: 0, no_mbid: 0, no_source: 0, kinds: [], folders: [], sources: [], types: [] }, '/api/library/index': { albums: 0, tracks: 0 },
    '/api/inbox': { pending: 2, changes_requested: 0, connections: 1, accounts: 'none', can_create_connections: false },
    '/api/agents': { connections: [] },
    '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] },
    '/api/inbox/proposals/5/preview': { preview: sameOnSix(), refusal: null, missing: [] },
    '/api/inbox/proposals/4': () => detail(),
    '/api/inbox/proposals/5': () => other,
    '/api/inbox/proposals': { proposals: [summary({ id: 4 }), summary({ id: 5, album: { id: 8, current_id: 8, title: 'Trans-Europe Express', artist: 'Kraftwerk', folder: 'b' } })], next: null },
    ...extra,
  })
  const pinia = createPinia(); setActivePinia(pinia)
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox/4')
  const w = mount(App, { global: { plugins: [pinia, router] } })
  await flushPromises()
  return { w, router }
}

test('switching proposals, closing, or navigating to /library with unsaved value edits, Reply text, or Decline reason prompts before discarding', async () => {
  const { w, router } = await openInboxApp()

  // 1. Adding an empty value row protects switching proposals.
  const row = w.get('[data-op="albumartists"]')
  await row.get('.ib-label').trigger('click')
  await row.get('button.lf-add').trigger('click')
  expect(useInbox().hasUnsaved).toBe(true)

  // Changing a list filter on the same proposal does not prompt.
  await router.push({ path: '/inbox/4', query: { state: 'all' } })
  await flushPromises()
  expect(w.find('[role="alertdialog"]').exists()).toBe(false)

  // Switching to proposal 5 prompts; canceling stays on proposal 4 with the draft intact.
  const toFive = router.push('/inbox/5')
  await flushPromises()
  const dlg = w.get('[role="alertdialog"]')
  expect(dlg.text()).toContain('unsaved value edits')
  await dlg.findAll('button.key').find((b) => b.text() === 'Keep editing')!.trigger('click')
  await toFive
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/inbox/4')
  expect(useInbox().hasValueDrafts).toBe(true)

  // Confirming Discard proceeds to proposal 5 and clears the old draft.
  const toFiveAgain = router.push('/inbox/5')
  await flushPromises()
  await w.get('[role="alertdialog"]').findAll('button.key').find((b) => b.text() === 'Discard')!.trigger('click')
  await toFiveAgain
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/inbox/5')
  expect(useInbox().hasUnsaved).toBe(false)

  // 2. Non-empty Talkback reply protects Close.
  await w.get('#tab-talkback').trigger('click')
  await w.get('textarea.ib-text').setValue('Please check the mastering credits')
  await w.get('[aria-label="Close the proposal"]').trigger('click')
  await flushPromises()
  expect(w.get('[role="alertdialog"]').text()).toContain('unsent Talkback reply')
  await w.get('[role="alertdialog"]').findAll('button.key').find((b) => b.text() === 'Keep editing')!.trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/inbox/5')
  expect((w.get('textarea.ib-text').element as HTMLTextAreaElement).value).toBe('Please check the mastering credits')
  await w.get('[aria-label="Close the proposal"]').trigger('click')
  await flushPromises()
  await w.get('[role="alertdialog"]').findAll('button.key').find((b) => b.text() === 'Discard')!.trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/inbox')
  expect(useInbox().replyDraft).toBe('')

  // 3. Open Decline reason protects navigating to /library.
  await router.push('/inbox/4')
  await flushPromises()
  await w.get('[data-op="albumartists"] [role="switch"]').trigger('click')
  await flushPromises()
  const bar = w.get('[aria-label="Review"]')
  await bar.findAll('button.key').find((b) => b.text().includes('Decline the rest'))!.trigger('click')
  await bar.get('input').setValue('Keep current album artist')

  const toLibrary = router.push('/library')
  await flushPromises()
  expect(w.get('[role="alertdialog"]').text()).toContain('unsent Decline reason')
  await w.get('[role="alertdialog"]').findAll('button.key').find((b) => b.text() === 'Keep editing')!.trigger('click')
  await toLibrary
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/inbox/4')
  expect((bar.get('input').element as HTMLInputElement).value).toBe('Keep current album artist')

  const toLibraryAgain = router.push('/library')
  await flushPromises()
  await w.get('[role="alertdialog"]').findAll('button.key').find((b) => b.text() === 'Discard')!.trigger('click')
  await toLibraryAgain
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/library')
  expect(useInbox().hasUnsaved).toBe(false)
  w.unmount()
})

test('typing additional text while a Talkback request is in flight does not lose the newly typed text', async () => {
  let resolveComment!: (v: unknown) => void
  const commentReq = new Promise((r) => { resolveComment = r })
  const bodies: unknown[] = []
  const w = await openDetail({
    '/api/inbox/proposals/4/comments': (_url: string, init?: RequestInit) => {
      bodies.push(JSON.parse(init!.body as string))
      return commentReq
    },
  })
  await w.get('#tab-talkback').trigger('click')
  const textarea = w.get('textarea.ib-text')
  await textarea.setValue('First note.')
  await w.get('form[aria-label="Reply"]').trigger('submit')
  // User continues typing while the first comment is still in flight.
  await textarea.setValue('First note. Follow-up thought.')
  resolveComment({})
  await flushPromises()
  expect(bodies).toEqual([{ body: 'First note.' }])
  expect((textarea.element as HTMLTextAreaElement).value).toBe('Follow-up thought.')
})

test('when the last pending op is decided while Ask for changes is on, submitting does not silently post a comment without confirmation', async () => {
  const d = detail()
  const calls = stubApi({
    '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] },
    '/api/inbox/proposals/4/comments': {},
    '/api/inbox/proposals/4/request-changes': {},
    '/api/inbox/proposals/4': () => structuredClone(d),
  })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox/4')
  await useInbox().open(4)
  const w = mount(InboxDetail, { global: { plugins: [router] } })
  await flushPromises()

  await w.get('#tab-talkback').trigger('click')
  await w.get('textarea.ib-text').setValue('Please use the 1974 original date')
  await w.get('form[aria-label="Reply"] .rocker').trigger('click')
  expect(w.get('form[aria-label="Reply"] button[type="submit"]').text()).toBe('Ask for changes')

  // All pending ops are decided while the reviewer is drafting.
  d.state = 'reviewed'
  d.ops = d.ops.map((o) => ({ ...o, decision: 'accepted' as const }))
  d.summary = { pending: 0, accepted: 3, declined: 0, superseded: 0 }
  await useInbox().loadDetail()
  await flushPromises()

  // Draft is preserved, notice is shown, and submitting does not silently post a comment.
  expect((w.get('textarea.ib-text').element as HTMLTextAreaElement).value).toBe('Please use the 1974 original date')
  const alert = w.get('.ib-ask-blocked')
  expect(alert.text()).toContain('No pending fields remain')
  expect(w.get('form[aria-label="Reply"] button[type="submit"]').attributes('disabled')).toBeDefined()
  await w.get('form[aria-label="Reply"]').trigger('submit')
  await flushPromises()
  expect(calls.some((c) => c.url.endsWith('/comments'))).toBe(false)
  expect(calls.some((c) => c.url.endsWith('/request-changes'))).toBe(false)

  // Explicitly confirming "Send as comment" posts the comment and clears the draft.
  await alert.findAll('button.link').find((b) => b.text() === 'Send as comment')!.trigger('click')
  await flushPromises()
  expect(calls.filter((c) => c.url.endsWith('/comments'))).toHaveLength(1)
  expect((w.get('textarea.ib-text').element as HTMLTextAreaElement).value).toBe('')
  expect(w.find('.ib-ask-blocked').exists()).toBe(false)
})

test('switching between Fields and Brief keeps the tab bar directly under the header so it never shifts', async () => {
  const w = await openDetail()
  const children = () => Array.from(w.get('.ib-body').element.children)
  const tabIndex = () => children().findIndex((el) => el.classList.contains('ib-tabs'))
  const whyIndex = () => children().findIndex((el) => el.classList.contains('ib-why-summary'))
  expect(tabIndex()).toBe(1)
  expect(whyIndex()).toBe(2)
  await w.get('#tab-brief').trigger('click')
  expect(tabIndex()).toBe(1)
  expect(whyIndex()).toBe(-1)
  await w.get('#tab-fields').trigger('click')
  expect(tabIndex()).toBe(1)
  expect(whyIndex()).toBe(2)
})

test('all-held proposals show accurate copy instead of claiming Accept will mark them accepted', async () => {
  const d = detail()
  d.live = {
    preview: sameOnSix(),
    op_stale: {},
    op_unchanged: { releasedate: true, albumartists: true, originaldate: true },
    refusal: null,
  }
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  expect(w.find('.ib-held').text()).toBe('3 fields the files already hold are hidden. Nothing left to write; decline them to close the proposal without writing.')
  expect(w.find('.ib-held').text()).not.toContain('Accept marks')
  expect(w.get('[aria-label="Review"]').text()).toContain('The files already hold every pending field.')
  expect(w.get('[aria-label="Review"] .sun').attributes('disabled')).toBeDefined()
})

test('superseded fields render human-readable field names instead of raw op IDs', async () => {
  const d = detail()
  d.ops.push(
    { id: 'op-99', type: 'tag', field: 'originaldate', decision: 'superseded' },
    { id: 'op-100', type: 'cover', action: 'replace', decision: 'superseded' },
  )
  d.summary.superseded = 2
  d.events.push({ id: 2, kind: 'declined', actor_kind: 'user', actor: 'alice', at: '2026-11-09T10:05:00Z', data: { ops: ['op-99', 'op-100'] } })
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  const sup = w.get('.ib-superseded').text()
  expect(sup).toContain('Original date')
  expect(sup).toContain('Replace cover')
  expect(sup).not.toContain('op-99')
  expect(sup).not.toContain('op-100')
  await w.get('#tab-talkback').trigger('click')
  expect(w.get('#panel-talkback').text()).toContain('alice declined Original date, Replace cover')
})

test('partial acceptances show their planned diff in Files labeled Planned for this acceptance alongside the outcome', async () => {
  const d = detail()
  d.state = 'reviewed'
  d.ops = d.ops.map((o) => ({ ...o, decision: o.id === 'originaldate' ? 'accepted' : 'declined', acceptance: o.id === 'originaldate' ? 12 : null }))
  d.summary = { pending: 0, accepted: 1, declined: 2, superseded: 0 }
  const preview = { ...sameOnSix(), files: [file(1, [{ key: 'ORIGINALDATE', before: [], after: ['1974'] }])], warnings: [] }
  d.acceptances = [{ id: 12, revision: 1, ops: ['originaldate'], change_id: 44, author: 'alice', created_at: '2026-11-09T10:00:00Z', preview, outcome: { state: 'partial', files: 2, written: 1, error: 'Track 2 is read-only.' } }]
  const w = await openDetail({ '/api/inbox/proposals/4': d })
  await w.get('#tab-files').trigger('click')
  const files = w.get('#panel-files')
  expect(files.text()).toContain('Planned for this acceptance')
  expect(files.text()).toContain('Partly written')
  expect(files.text()).toContain('1 of 2 files written')
  expect(files.text()).toContain('ORIGINALDATE')
  expect(files.find('a[href="/library?history=1&change=44"]').exists()).toBe(true)
})

test('transient detail load failure offers Retry, and 404 shows an unavailable message without Retry', async () => {
  let fail = true
  stubApi({
    '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] },
    '/api/inbox/proposals/4': () => (fail ? new Reply(500, { error: 'Temporary outage' }) : detail()),
  })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox/4')
  await useInbox().open(4)
  const w = mount(InboxDetail, { global: { plugins: [router] } })
  await flushPromises()
  expect(w.get('.ib-detail-err').text()).toContain('Temporary outage')
  const retryBtn = w.get('.ib-detail-err button')
  expect(retryBtn.text()).toBe('Retry')
  fail = false
  await retryBtn.trigger('click')
  await flushPromises()
  expect(w.find('.ib-detail-err').exists()).toBe(false)
  expect(w.find('.ib-title').text()).toBe('Dates from MusicBrainz')
  w.unmount()

  useInbox().close()
  stubApi({ '/api/inbox/proposals/99': new Reply(404, { error: 'Not found' }) })
  await useInbox().open(99)
  const missing = mount(InboxDetail, { global: { plugins: [router] } })
  await flushPromises()
  expect(missing.get('.ib-detail-err').text()).toContain('This proposal is no longer available.')
  expect(missing.find('.ib-detail-err button').exists()).toBe(false)
  missing.unmount()
})

test('expanded InboxOp rows wrap long track names and Was lines instead of ellipsizing them', async () => {
  const longBefore = 'A very long previous value that must remain readable without hover tooltips on touch screens'
  const w = await openDetail({ '/api/inbox/proposals/4': withBefore({ albumartists: six([longBefore]) }) })
  const row = w.get('[data-op="albumartists"]')
  await row.get('.ib-label').trigger('click')
  await row.findAll('button').find((b) => b.text() === 'Per track')!.trigger('click')
  const trackHead = row.get('.ib-track .lbl-f')
  expect(trackHead.classes()).not.toContain('ell')
  expect(trackHead.classes()).toContain('ib-wrap')
  const was = row.get('.ib-op-val .lf-was')
  expect(was.classes()).not.toContain('ell')
  expect(was.classes()).toContain('ib-wrap')
  expect(was.text()).toBe(`Was ${longBefore}`)
})

test('InboxDiff disambiguates duplicate basenames, expands long values, and includes mobile labels for Files and Tag', async () => {
  const longText = Array.from({ length: 8 }, (_, i) => `Line ${i + 1}: A long lyrical verse for verification`).join('\n')
  const p: Preview = {
    album: { id: 7, folder: 'box', title: 'Box', artist: 'Artist' },
    files: [
      { track_id: 1, path: 'box/Disc 1/01.flac', keys: [{ key: 'LYRICS', before: ['Short'], after: [longText] }], cover_before: 'a', cover_after: 'b' },
      { track_id: 2, path: 'box/Disc 2/01.flac', keys: [{ key: 'LYRICS', before: ['Other'], after: ['Verse'] }], cover_before: 'a', cover_after: 'b' },
      { track_id: 3, path: 'box/Disc 1/02.flac', keys: [{ key: 'LYRICS', before: ['Third'], after: ['Outro'] }], cover_before: '', cover_after: '' },
    ],
    folder: [
      { path: 'box/Disc 1/cover.jpg', before: 'old1', after: 'new1' },
      { path: 'box/Disc 2/cover.jpg', before: 'old2', after: 'new2' },
    ],
    warnings: [],
    digest: 'x',
  }
  const w = mount(InboxDiff, { props: { preview: p, title: 'What Accept would write' } })
  const rows = w.findAll('.ib-tr:not(.head)')
  expect(rows.map((r) => r.find('.ib-cell-v').text())).toEqual(['Disc 1/01.flac', 'Disc 2/01.flac', '02.flac'])
  expect(rows[0]!.findAll('.ib-mobile-label').map((l) => l.text())).toEqual(['Files', 'Tag', 'Before', 'After'])
  const expand = rows[0]!.get('.ib-expand')
  expect(expand.text()).toBe('Show full')
  expect(expand.attributes('aria-expanded')).toBe('false')
  await expand.trigger('click')
  expect(expand.text()).toBe('Show less')
  expect(expand.attributes('aria-expanded')).toBe('true')
  expect(rows[0]!.findAll('.ib-val-text')[1]!.classes()).toContain('expanded')
  expect(w.findAll('[aria-label="Folder artwork"] .ib-cover .sm').map((s) => s.text())).toEqual(['Disc 1/cover.jpg', 'Disc 2/cover.jpg'])
  expect(w.findAll('[aria-label="Embedded artwork"] .ib-cover .sm').map((s) => s.text())).toEqual(['Disc 1/01.flac', 'Disc 2/01.flac'])
})

test('InboxList omits dead stale text, puts ScrollMore inside ul.ib-rows, and offers Retry on list errors', async () => {
  let fail = true
  stubApi({
    '/api/inbox/proposals': () => (fail ? new Reply(500, { error: 'List unavailable' }) : { proposals: [summary({ id: 4, stale: true })], next: 4 }),
  })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox')
  const inbox = useInbox()
  await inbox.loadList()
  const w = mount(InboxList, { global: { plugins: [router] } })
  await flushPromises()

  expect(w.get('.ib-list-err').text()).toContain('List unavailable')
  const retryBtn = w.get('.ib-list-err button')
  expect(retryBtn.text()).toBe('Retry')

  fail = false
  await retryBtn.trigger('click')
  await flushPromises()
  expect(w.find('.ib-list-err').exists()).toBe(false)
  expect(w.text()).not.toContain('Changed since proposed')
  expect(w.find('ul.ib-rows .scroll-more').exists()).toBe(true)
  w.unmount()
})

test('InboxPage shows Retry on transient startup failure and redirects immediately when disabled', async () => {
  let fail = true
  stubApi({
    '/api/inbox': () => (fail ? new Reply(503, { error: 'Backend starting' }) : { pending: 1, changes_requested: 0, connections: 1, accounts: 'none', can_create_connections: false }),
    '/api/inbox/proposals': { proposals: [summary({ id: 4 })], next: null },
    '/api/agents': { connections: [] },
  })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox')
  const w = mountInboxPage(router)
  await flushPromises()

  expect(router.currentRoute.value.path).toBe('/inbox')
  expect(w.get('.ib-detect-err').text()).toContain('Backend starting')
  fail = false
  await w.get('.ib-detect-err button').trigger('click')
  await flushPromises()
  expect(w.find('.ib-detect-err').exists()).toBe(false)
  expect(w.find('.ib-rows').exists()).toBe(true)
  w.unmount()

  // Confirmed disabled redirects immediately on a deep link.
  const inbox = useInbox()
  inbox.enabled = false
  const router2 = createAppRouter(createMemoryHistory())
  await router2.push('/inbox/4')
  const w2 = mountInboxPage(router2)
  await flushPromises()
  expect(router2.currentRoute.value.path).toBe('/library')
  w2.unmount()
})

test('stacked tablet layout focuses detail heading on open and restores focus to the row on close', async () => {
  vi.stubGlobal('matchMedia', (q: string) => ({
    matches: q === '(max-width: 1100px)',
    media: q,
    addEventListener: () => {},
    removeEventListener: () => {},
  }))
  stubApi({
    '/api/inbox': { pending: 1, changes_requested: 0, connections: 1, accounts: 'none', can_create_connections: false },
    '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] },
    '/api/inbox/proposals/4': detail(),
    '/api/inbox/proposals': { proposals: [summary({ id: 4 })], next: null },
    '/api/agents': { connections: [] },
  })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox')
  const w = mountInboxPage(router, { attachTo: document.body })
  await flushPromises()

  await router.push('/inbox/4')
  await flushPromises()
  expect(document.activeElement?.id).toBe('ibd-h')

  await w.get('button[aria-label="Close the proposal"]').trigger('click')
  await flushPromises()
  expect(document.activeElement?.getAttribute('data-proposal-id')).toBe('4')
  w.unmount()
})

test('focus moves into Decline on open, returns on Cancel, moves to new input on Add value, and stays in the group on Remove and Leave out', async () => {
  const d = detail()
  d.current.ops = [
    { id: 'genre', type: 'tag', field: 'genre', tracks: [{ track_id: 1, values: ['Rock', 'Pop'] }, { track_id: 2, values: ['Jazz'] }] },
  ]
  d.ops = [{ id: 'genre', type: 'tag', decision: 'pending' }]
  d.summary = { pending: 1, accepted: 0, declined: 0, superseded: 0 }
  stubApi({ '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] }, '/api/inbox/proposals/4': d })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox/4')
  await useInbox().open(4)
  // LibTip teleports its notes into the app's .rs root.
  const rs = document.createElement('div')
  rs.className = 'rs'
  document.body.appendChild(rs)
  const w = mount(InboxDetail, { attachTo: rs, global: { plugins: [router] } })
  onTestFinished(() => { w.unmount(); rs.remove() })
  await flushPromises()

  // Decline focus open & Cancel restore (switch field off so Decline is available).
  await w.get('.ib-op [role="switch"]').trigger('click')
  await flushPromises()
  const declineBtn = w.findAll('.ib-review-actions button').find((b) => b.text().includes('Decline'))!
  await declineBtn.trigger('click')
  await flushPromises()
  expect(document.activeElement).toBe(w.get('.ib-decline-form input').element)

  await w.findAll('.ib-decline-form button').find((b) => b.text() === 'Cancel')!.trigger('click')
  await flushPromises()
  expect(document.activeElement).toBe(w.findAll('.ib-review-actions button').find((b) => b.text().includes('Decline'))!.element)

  // Open Genre editor: two track groups because their values differ.
  await w.get('.ib-op .ib-label').trigger('click')
  await flushPromises()
  const firstGroup = w.findAll('[role="group"][aria-label^="Genre"]')[0]!
  await firstGroup.get('.lf-add').trigger('click')
  await flushPromises()
  const inputsAfterAdd = firstGroup.findAll('input')
  expect(inputsAfterAdd).toHaveLength(3)
  expect(document.activeElement).toBe(inputsAfterAdd[2]!.element)

  // Remove the last input -> focus moves to the previous input.
  await firstGroup.findAll('button[aria-label^="Remove "]')[2]!.trigger('click')
  await flushPromises()
  expect(document.activeElement).toBe(firstGroup.findAll('input')[1]!.element)

  // Remove remaining values -> focus moves to Add value.
  await firstGroup.findAll('button[aria-label^="Remove "]')[1]!.trigger('click')
  await flushPromises()
  expect(document.activeElement).toBe(firstGroup.findAll('input')[0]!.element)
  await firstGroup.findAll('button[aria-label^="Remove "]')[0]!.trigger('click')
  await flushPromises()
  expect(document.activeElement).toBe(firstGroup.get('.lf-add').element)

  // Leave out a track -> focus stays inside the op's remaining value group.
  const leaveOutBtn = w.findAll('.ib-track-head button').find((b) => b.text() === 'Leave out')!
  await leaveOutBtn.trigger('click')
  await flushPromises()
  expect(w.get('.ib-op-val').element.contains(document.activeElement)).toBe(true)
})

test('clicking a cover thumbnail in InboxDiff or InboxOp opens the uncropped preview and Escape closes it', async () => {
  const p: Preview = {
    album: { id: 7, folder: 'a', title: 'Album', artist: 'Artist' },
    files: [{ track_id: 1, path: 'a/01.flac', keys: [], cover_before: 'oldhash', cover_after: 'newhash' }],
    folder: [{ path: 'a/cover.jpg', before: '', after: 'newhash' }],
    warnings: [],
    digest: 'cov',
  }
  const w = mount(InboxDiff, { attachTo: document.body, props: { preview: p, title: 'What Accept would write' } })
  const beforeBtn = w.get('button[aria-label="Inspect 01.flac, before"]')
  await beforeBtn.trigger('click')
  await flushPromises()

  const dlg = w.get('[role="dialog"]')
  expect(dlg.attributes('aria-label')).toBe('01.flac, before')
  expect(dlg.get('img').attributes('src')).toBe('/api/library/covers/oldhash')
  expect(dlg.get('img').attributes('alt')).toBe('01.flac, before')

  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await flushPromises()
  expect(w.find('[role="dialog"]').exists()).toBe(false)
  expect(document.activeElement).toBe(beforeBtn.element)
  w.unmount()

  // Also works from InboxOp cover thumbnail.
  const d = detail()
  d.current.ops = [{ id: 'cover', type: 'cover', action: 'replace', image: 'newhash' }]
  d.ops = [{ id: 'cover', type: 'cover', decision: 'pending' }]
  d.summary = { pending: 1, accepted: 0, declined: 0, superseded: 0 }
  stubApi({ '/api/inbox/proposals/4/preview': { preview: p, refusal: null, missing: [] }, '/api/inbox/proposals/4': d })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox/4')
  await useInbox().open(4)
  const wd = mount(InboxDetail, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  await wd.get('.ib-op .ib-label').trigger('click')
  await flushPromises()
  const opThumb = wd.get('button[aria-label="Inspect Replace cover, after"]')
  await opThumb.trigger('click')
  await flushPromises()
  expect(wd.get('[role="dialog"]').attributes('aria-label')).toBe('Replace cover, after')
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await flushPromises()
  expect(wd.find('[role="dialog"]').exists()).toBe(false)
  expect(document.activeElement).toBe(opThumb.element)
  wd.unmount()
})

test('arrow keys navigate the Inbox state rotary and PickMenu, restoring focus on close', async () => {
  stubApi({ '/api/inbox/proposals': { proposals: [], next: null } })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox')
  const w = mount(InboxList, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()

  const radios = w.findAll('[role="radiogroup"] [role="radio"]')
  expect(radios.map((r) => r.attributes('tabindex'))).toEqual(['0', '-1', '-1', '-1'])
  ;(radios[0]!.element as HTMLElement).focus()
  await radios[0]!.trigger('keydown', { key: 'ArrowRight' })
  await flushPromises()
  expect(router.currentRoute.value.query.state).toBe('reviewed')
  expect(document.activeElement).toBe(radios[1]!.element)

  await radios[1]!.trigger('keydown', { key: 'End' })
  await flushPromises()
  expect(router.currentRoute.value.query.state).toBe('all')
  expect(document.activeElement).toBe(radios[3]!.element)
  w.unmount()

  const { default: PickMenu } = await import('./PickMenu.vue')
  const picked: string[] = []
  const pm = mount(PickMenu, {
    attachTo: document.body,
    props: {
      label: 'Kind',
      value: '',
      options: [{ id: '', label: 'Tags and covers' }, { id: 'tag', label: 'Tags' }, { id: 'cover', label: 'Covers' }],
      onPick: (id: string | number) => picked.push(String(id)),
    },
  })
  const trigger = pm.get('.pick-cap')
  await trigger.trigger('keydown', { key: 'ArrowDown' })
  await flushPromises()
  const items = pm.findAll('[role="menuitemradio"]')
  expect(items.map((i) => i.attributes('tabindex'))).toEqual(['0', '-1', '-1'])
  expect(document.activeElement).toBe(items[0]!.element)

  await items[0]!.trigger('keydown', { key: 'ArrowDown' })
  await flushPromises()
  expect(document.activeElement).toBe(items[1]!.element)
  expect(items[1]!.attributes('tabindex')).toBe('0')

  // Escape closes and restores focus to trigger.
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
  await flushPromises()
  expect(pm.find('[role="menu"]').exists()).toBe(false)
  expect(document.activeElement).toBe(trigger.element)

  // Picking an option emits pick and restores focus to trigger.
  await trigger.trigger('click')
  await flushPromises()
  await pm.findAll('[role="menuitemradio"]')[2]!.trigger('click')
  await flushPromises()
  expect(picked).toEqual(['cover'])
  expect(document.activeElement).toBe(trigger.element)
  pm.unmount()
})

test('LibTip opens on keyboard focus and closes on Escape, and number-only change/event evidence renders with IDs', async () => {
  const rs = document.createElement('div')
  rs.className = 'rs'
  document.body.appendChild(rs)

  const d = detail()
  d.current.evidence = [
    { kind: 'change', change: 42 },
    { kind: 'event', event: 17 },
  ]
  stubApi({ '/api/inbox/proposals/4/preview': { preview: sameOnSix(), refusal: null, missing: [] }, '/api/inbox/proposals/4': d })
  const router = createAppRouter(createMemoryHistory())
  await router.push('/inbox/4')
  await useInbox().open(4)
  const w = mount(InboxDetail, { attachTo: rs, global: { plugins: [router] } })
  await flushPromises()

  // Standalone LibTip (e.g. revision) is keyboard focusable and opens tooltip on focus.
  const revTip = w.findAll('.has-tip').find((n) => n.text().includes('revision 1'))!
  expect(revTip.attributes('tabindex')).toBe('0')
  await revTip.trigger('focus')
  await flushPromises()
  expect(rs.querySelector('[role="tooltip"]')?.textContent).toContain('Revision')
  await revTip.trigger('keydown', { key: 'Escape' })
  await flushPromises()
  expect(rs.querySelector('[role="tooltip"]')).toBeNull()

  // Evidence renders Change 42 and Event 17.
  await w.get('#tab-brief').trigger('click')
  const tapes = w.findAll('.ib-tapes .sug-v').map((n) => n.text())
  expect(tapes).toEqual(['Change 42', 'Event 17'])
  w.unmount()
  rs.remove()
})

test('Decline keeps the typed reason when a conflict removes the last pending field or when editing a value', async () => {
  const d = detail()
  d.current.ops = [d.current.ops.find((o) => o.id === 'albumartists')!]
  d.ops = [d.ops.find((o) => o.id === 'albumartists')!]
  d.summary = { pending: 1, accepted: 0, declined: 0, superseded: 0 }

  const w = await openDetail({
    '/api/inbox/proposals/4': () => structuredClone(d),
    '/api/inbox/proposals/4/decline': () => {
      d.state = 'reviewed'
      d.summary = { pending: 0, accepted: 1, declined: 0, superseded: 0 }
      d.ops[0]!.decision = 'accepted'
      return new Reply(409, { code: 'decided_op', error: 'Already decided' })
    },
  })

  // Turn off the single pending field and open Decline.
  await w.get('[data-op="albumartists"] [role="switch"]').trigger('click')
  await flushPromises()
  await w.findAll('button.key').find((b) => b.text().includes('Decline all'))!.trigger('click')
  await w.get('form.ib-decline-form input').setValue('Keep my reason')

  // Editing a value switches to dirty mode without wiping the Decline reason; discarding restores the form with the reason.
  await w.get('[data-op="albumartists"] .ib-label').trigger('click')
  await w.get('[data-op="albumartists"] input').setValue('Temporary edit')
  expect(w.find('[aria-label="Unsaved values"]').exists()).toBe(true)
  expect(useInbox().declineReason).toBe('Keep my reason')
  await w.findAll('button.key').find((b) => b.text() === 'Discard')!.trigger('click')
  await flushPromises()
  expect(w.find('form.ib-decline-form').exists()).toBe(true)
  expect((w.get('form.ib-decline-form input').element as HTMLInputElement).value).toBe('Keep my reason')

  // Submitting hits decided_op removing the final pending field: form stays visible with reason and submit disabled.
  await w.get('form.ib-decline-form').trigger('submit')
  await flushPromises()
  expect(w.find('form.ib-decline-form').exists()).toBe(true)
  expect((w.get('form.ib-decline-form input').element as HTMLInputElement).value).toBe('Keep my reason')
  expect(w.text()).toContain('No pending fields are left to decline.')
  expect(w.get('form.ib-decline-form button[type="submit"]').attributes('disabled')).toBeDefined()

  // Remounting InboxBar across the phone breakpoint keeps the open Decline form and reason accessible to Cancel.
  const { default: InboxBar } = await import('./InboxBar.vue')
  const phoneBar = mount(InboxBar, { props: { phone: true } })
  expect(phoneBar.find('form.ib-decline-form').exists()).toBe(true)
  expect((phoneBar.get('form.ib-decline-form input').element as HTMLInputElement).value).toBe('Keep my reason')
  phoneBar.unmount()

  // Explicit Cancel clears the reason and shows the result bar.
  await w.get('form.ib-decline-form button[type="button"]').trigger('click')
  await flushPromises()
  expect(useInbox().declineReason).toBe('')
  expect(w.find('form.ib-decline-form').exists()).toBe(false)
})

test('Talkback send preserves Ask for changes intent for text typed while the comment is in flight', async () => {
  let resolveComment!: (v: unknown) => void
  const w = await openDetail({
    '/api/inbox/proposals/4/comments': () => new Promise((r) => { resolveComment = r }),
  })
  await w.get('#tab-talkback').trigger('click')
  await w.get('textarea.ib-text').setValue('First note')
  const sending = w.get('#panel-talkback form').trigger('submit')
  await flushPromises()

  // User types a follow-up draft and enables Ask for changes while the first comment is in flight.
  await w.get('textarea.ib-text').setValue('First note Please also fix the date')
  await w.get('#panel-talkback [role="switch"]').trigger('click')
  expect(useInbox().replyAsking).toBe(true)

  resolveComment({})
  await sending
  await flushPromises()
  expect(useInbox().replyDraft).toBe('Please also fix the date')
  expect(useInbox().replyAsking).toBe(true)
})

test('Escape closes only the topmost modal when the discard dialog opens above the artwork preview', async () => {
  const { w, router } = await openInboxApp()
  document.body.appendChild(w.element)

  // Mount artwork preview first (just like when opened from InboxDetail).
  const { default: InboxCoverDialog } = await import('./InboxCoverDialog.vue')
  let coverClosed = false
  const cd = mount(InboxCoverDialog, {
    attachTo: document.body,
    props: { url: '/cover.jpg', label: 'Cover', onClose: () => { coverClosed = true } },
  })
  // Make sure the discard dialog will appear AFTER the cover dialog in DOM order.
  document.body.appendChild(w.element)

  const row = w.get('[data-op="albumartists"]')
  await row.get('.ib-label').trigger('click')
  await row.get('input').setValue('Edited')
  const nav = router.push('/inbox/5')
  await flushPromises()
  expect(w.find('[role="alertdialog"]').exists()).toBe(true)

  // Pressing Escape closes ONLY the discard dialog, not the artwork preview underneath.
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
  await nav
  await flushPromises()
  expect(w.find('[role="alertdialog"]').exists()).toBe(false)
  expect(coverClosed).toBe(false)

  // A second Escape now closes the artwork preview.
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
  await flushPromises()
  expect(coverClosed).toBe(true)
  cd.unmount()
  w.unmount()
})

test('Talkback preserves Ask for changes intent across responsive layout remount and resets on proposal switch', async () => {
  const { w, router } = await openInboxApp()
  await w.get('#tab-talkback').trigger('click')
  await w.get('textarea.ib-text').setValue('Fix the release date')
  await w.get('#panel-talkback [role="switch"]').trigger('click')
  expect(useInbox().replyAsking).toBe(true)
  expect(w.get('#panel-talkback button[type="submit"]').text()).toContain('Ask for changes')

  // Unmounting and remounting InboxDetail (as happens across the phone breakpoint) preserves both draft and asking state.
  const detailW = mount(InboxDetail, { props: { phone: true }, global: { plugins: [router] } })
  await flushPromises()
  await detailW.get('#tab-talkback').trigger('click')
  expect((detailW.get('textarea.ib-text').element as HTMLTextAreaElement).value).toBe('Fix the release date')
  expect(detailW.get('#panel-talkback [role="switch"]').attributes('aria-checked')).toBe('true')
  expect(detailW.get('#panel-talkback button[type="submit"]').text()).toContain('Ask for changes')
  detailW.unmount()

  // Switching proposals resets both draft and asking intent.
  await useInbox().open(5)
  expect(useInbox().replyDraft).toBe('')
  expect(useInbox().replyAsking).toBe(false)
  w.unmount()
})

test('unsaved-changes dialog takes and traps focus, closes on Escape restoring focus, and cover dialog traps Tab', async () => {
  const { w, router } = await openInboxApp()
  // Attach to body so document.activeElement tracks focus.
  document.body.appendChild(w.element)

  const row = w.get('[data-op="albumartists"]')
  await row.get('.ib-label').trigger('click')
  await row.get('input').setValue('Edited')
  const closeBtn = w.get('[aria-label="Close the proposal"]').element as HTMLElement
  closeBtn.focus()

  const nav = router.push('/inbox/5')
  await flushPromises()
  const dlg = w.get('[role="alertdialog"]')
  const buttons = dlg.findAll('button.key')
  expect(document.activeElement).toBe(buttons[0]!.element)

  // Tab wraps from Discard back to Keep editing, and Shift+Tab wraps from Keep editing to Discard.
  ;(buttons[1]!.element as HTMLElement).focus()
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
  expect(document.activeElement).toBe(buttons[0]!.element)
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
  expect(document.activeElement).toBe(buttons[1]!.element)

  // Escape closes the dialog, stays on proposal 4, and restores focus to closeBtn.
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
  await nav
  await flushPromises()
  expect(w.find('[role="alertdialog"]').exists()).toBe(false)
  expect(router.currentRoute.value.path).toBe('/inbox/4')
  expect(document.activeElement).toBe(closeBtn)
  w.unmount()

  // Cover dialog traps Tab on its close button.
  const { default: InboxCoverDialog } = await import('./InboxCoverDialog.vue')
  const cd = mount(InboxCoverDialog, { attachTo: document.body, props: { url: '/cover.jpg', label: 'Cover' } })
  const coverClose = cd.get('button[aria-label="Close artwork preview"]').element as HTMLElement
  expect(document.activeElement).toBe(coverClose)
  const tabEv = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
  document.dispatchEvent(tabEv)
  expect(tabEv.defaultPrevented).toBe(true)
  expect(document.activeElement).toBe(coverClose)
  cd.unmount()
})

test('switching from scrolled pinned Fields to Brief keeps pinned min-height until scrolled back to top', async () => {
  const w = await openDetail()
  const body = w.get('.ib-body').element as HTMLElement
  const tabBar = w.get('.ib-tabs').element as HTMLElement
  body.scrollTop = 180
  vi.spyOn(body, 'getBoundingClientRect').mockReturnValue({ top: 100 } as DOMRect)
  vi.spyOn(tabBar, 'getBoundingClientRect').mockReturnValue({ top: 100 } as DOMRect)

  await w.get('#tab-brief').trigger('click')
  await flushPromises()
  expect(w.get('.ib-body').classes()).toContain('ib-keep-pinned')

  body.scrollTop = 0
  await w.get('.ib-body').trigger('scroll')
  expect(w.get('.ib-body').classes()).not.toContain('ib-keep-pinned')
})
