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
  expect(bar.text()).toContain('3 fields · 6 files')
  expect(bar.text()).not.toContain('Accept writes')
  expect(bar.text()).toContain('Accept 3 fields')
  expect(w.findAll('.ib-lcds .lcd').map((l) => l.findAll(':scope > span').map((s) => s.text()).join(' '))).toEqual(['3 FIELDS', '6 FILES', '18 TAG WRITES'])
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


test('evidence without a label shows its URL and never links non-web schemes', async () => {
  const d = detail()
  d.current.evidence = [
    { kind: 'link', url: 'http://example.com/credits' },
    { kind: 'link', url: 'data:text/html,unsafe' },
    { kind: 'link', url: '//example.com/credits' },
    { kind: 'link', text: 'https://example.com/label-only' },
  ]
  const w = await openDetail({ '/api/inbox/proposals/4': d })
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
  expect(w.findAll('.ib-op .lf-was').map((e) => e.text())).toEqual(['Was empty', 'Was 2012', 'Was different on each track'])
  await w.findAll('.ib-op')[2]!.findAll('button').find((b) => b.text() === 'Per track')!.trigger('click')
  const perTrack = w.findAll('.ib-op')[2]!.findAll('.lf-was').map((e) => e.text())
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
  expect(w.find('.ib-bank').classes()).toContain('cabled')
  expect(w.find('.ib-plug').exists()).toBe(false)
  expect(w.findAll('.ib-op .sr').map((s) => s.text())).toEqual(['Goes with Original date. They switch on and off together.'])
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
  const { default: InboxPage } = await import('../pages/InboxPage.vue')
  const w = mount(InboxPage, { global: { plugins: [router] } })
  await flushPromises()
  return w
}

test('on the phone the review bar is a slot above the dock, outside the proposal window', async () => {
  const w = await openOnPhone()
  const slot = w.find('.stack > section.win.slot[aria-label="Review"]')
  expect(slot.text()).toContain('3 fields · 6 files')
  expect(slot.findAll('button').map((b) => b.text())).toEqual(['Accept 3'])
  expect(w.find('.ib-detail [aria-label="Review"]').exists()).toBe(false)
  // Decline shows once a field is off, and declines the fields left off.
  await w.findAll('.ib-op .rocker')[2]!.trigger('click')
  await flushPromises()
  expect(w.find('.stack > section.win.slot').findAll('button').map((b) => b.text())).toEqual(['Decline 1', 'Accept 2'])
})

test('on the phone a field row shows the new value, and pressing its label opens the field', async () => {
  const w = await openOnPhone()
  const row = w.findAll('.ib-op')[0]!
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
  expect(out.text()).toContain('6 of 6 files')
  expect(out.text()).toContain('Change 31 in History')
})

test('on the phone, which has no History, the change is named without a link', async () => {
  const d = detail()
  d.acceptances = [{ id: 9, revision: 1, ops: ['originaldate', 'releasedate'], change_id: 31, author: 'alice', created_at: '2026-11-09T10:00:00Z', outcome: { state: 'done', files: 6, written: 6 } } as never]
  const w = await openDetail({ '/api/inbox/proposals/4': d }, true)
  expect(w.text()).toContain('Change 31 is in History on a desktop.')
  expect(w.findAll('a').some((a) => (a.attributes('href') ?? '').includes('history=1'))).toBe(false)
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
  const { default: InboxPage } = await import('../pages/InboxPage.vue')
  const w = mount(InboxPage, { global: { plugins: [router] } })
  await flushPromises()
  expect(w.find('.lib-split').exists()).toBe(false)
  await router.push('/inbox/4')
  await flushPromises()
  const split = w.find('.lib-split')
  expect(split.attributes('aria-label')).toContain('proposal list')
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
  w.unmount()
  const restored = mount(InboxPage, { global: { plugins: [router] } })
  await flushPromises()
  expect((restored.find('.ib-page').element as HTMLElement).style.getPropertyValue('--list-w')).toBe('620px')
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
    expect(tips()).toEqual(expect.arrayContaining(['Open album', 'revision 1', '86%', 'Not sure:', 'Release credits', 'Release date', 'Original date', 'TAG WRITES', 'Talkback', 'Ask for changes', 'Accept 3 fields']))
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
