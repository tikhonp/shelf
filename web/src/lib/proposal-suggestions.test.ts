import { beforeEach, expect, test, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory } from 'vue-router'
import type { ProposalDetail } from '../api/agents'
import type { LibraryTrack, Suggestions } from '../api/types'
import { proposalSuggestions, withProposals } from './proposal-suggestions'
import { accept, offersFor, saveBody } from './tags'
import { useEditor } from '../stores/editor'
import { stubApi } from '../testing/api'
import LibPhoneField from '../components/LibPhoneField.vue'
import LibField from '../components/LibField.vue'
import { useInbox } from '../stores/inbox'
import { createAppRouter } from '../router'
import AlbumProposals from '../components/AlbumProposals.vue'
import LibSug from '../components/LibSug.vue'

const tracks = [1, 2].map((id) => ({ id, path: `album/${id}.flac`, revision: `r${id}`, standard: [], extended: [] } as unknown as LibraryTrack))
function proposal(): ProposalDetail {
  return { id: 4, url: '/inbox/4', state: 'pending', changes_requested: false, connection: { id: 1, name: 'Claude at home' }, run_id: 1, album: { id: 7, current_id: 7, folder: 'album', title: 'Album', artist: 'Artist' }, group: '', follow_up_of: null, head: 1, revisions: [],
    current: { title: 'Dates', reason: '', uncertainties: '', confidence: 86, evidence: [], base: tracks.map((t) => ({ track_id: t.id, path: t.path, revision: t.revision })), ops: [{ id: 'opaque-id', type: 'tag', field: 'albumartists', tracks: tracks.map((t) => ({ track_id: t.id, values: ['Talk Talk'] })) }], preview: { album: { id: 7, folder: 'album', title: 'Album', artist: 'Artist' }, files: [], folder: [], warnings: [], digest: '' } },
    ops: [{ id: 'opaque-id', type: 'tag', decision: 'pending' }], summary: { pending: 1, accepted: 0, declined: 0, superseded: 0 }, acceptances: [], events: [] }
}
beforeEach(() => setActivePinia(createPinia()))

test('proposal tape names the agent and stages exact values in an ordinary Save', () => {
  const tape = proposalSuggestions([proposal()], tracks)[0]!
  expect(tape.sources).toEqual(['Claude at home'])
  expect(tape.confidence).toBe(86)
  const stage = accept({}, tracks, tracks, tape, true)
  expect(saveBody(tracks, stage).tracks.map((t) => t.edits)).toEqual(tracks.map(() => [{ field: 'albumartists', values: ['Talk Talk'] }]))
})

test.each(['accepted', 'declined', 'superseded'] as const)('%s operations have no tape', (decision) => {
  const p = proposal(); p.ops[0]!.decision = decision
  expect(proposalSuggestions([p], tracks)).toEqual([])
})

test('withdrawn proposals and reused track IDs have no tape', () => {
  const p = proposal(); p.state = 'withdrawn'
  expect(proposalSuggestions([p], tracks)).toEqual([])
  p.state = 'pending'; p.current.base[0]!.path = 'other.flac'
  expect(proposalSuggestions([p], tracks)).toEqual([])
})

test('track selection keeps each proposed value on its own track', () => {
  const p = proposal(); const op = p.current.ops[0]!
  if (op.type !== 'tag') throw new Error('tag')
  op.tracks[1]!.values = ['Other']
  const s = withProposals(null, proposalSuggestions([p], tracks))
  const tape = offersFor(s, 'albumartists', 2, false, [])[0]!
  const stage = accept({}, tracks, [tracks[1]!], tape, true)
  expect(Object.keys(stage)).toEqual(['2'])
  expect(saveBody(tracks, stage).tracks[0]!.edits[0]!.values).toEqual(['Other'])
})

test('proposal tapes stay beside regular suggestions even with three existing offers', () => {
  const p = proposal()
  const regular = { field: 'albumartists', values: ['Other'], source: 'files', sources: ['Files'], confidence: 50 }
  const s = withProposals({ album: [], tracks: [{ id: 1, suggestions: [regular, regular, regular] }] } as unknown as Suggestions, proposalSuggestions([p], tracks))
  expect(offersFor(s, 'albumartists', 1, false, [])).toHaveLength(4)
})

test('removal tape keeps an empty value list and its zero confidence', async () => {
  const p = proposal(); p.current.confidence = 0
  const op = p.current.ops[0]!
  if (op.type !== 'tag') throw new Error('tag')
  op.tracks.forEach((t) => { t.values = [] })
  const tape = proposalSuggestions([p], tracks)[0]!
  const w = mount(LibSug, { props: { s: tape } })
  expect(w.text()).toContain('Remove tag')
  expect(w.text()).toContain('0%')
  await w.get('button').trigger('click')
  expect(w.emitted('use')).toHaveLength(1)
  w.unmount()
})

test('Proposed row uses editor labels and links directly to its Inbox review', async () => {
  const inbox = useInbox(); inbox.enabled = true; inbox.byAlbum[7] = [proposal()]
  const router = createAppRouter(createMemoryHistory()); await router.push('/library/7')
  const w = mount(AlbumProposals, { props: { albumId: 7 }, global: { plugins: [router] } })
  expect(w.text()).toContain('Proposed')
  expect(w.text()).toContain('Claude at home proposes 1 field')
  expect(w.text()).toContain('Album artists')
  expect(w.text()).not.toContain('opaque-id')
  expect(w.get('a').attributes('href')).toBe('/inbox/4')
  inbox.byAlbum[7] = []
  await flushPromises()
  expect(w.find('.lib-iss').exists()).toBe(false)
  w.unmount()
  vi.unstubAllGlobals()
})

test('an omitted confidence is not reported as zero percent', () => {
  const p = proposal(); p.current.confidence = null
  const w = mount(LibSug, { props: { s: proposalSuggestions([p], tracks)[0]! } })
  expect(w.text()).toContain('Not rated')
  expect(w.text()).not.toContain('0%')
  w.unmount()
})


test.each(['desktop', 'phone'])('%s fields stage the same proposal tape', async (layout) => {
  const p = proposal()
  const op = p.current.ops[0]!
  if (op.type !== 'tag') throw new Error('tag')
  op.field = 'originaldate'
  op.tracks.forEach((t) => { t.values = ['1988-09-12'] })
  stubApi({ '/api/inbox/albums/7/proposals': { proposals: [p] } })
  const inbox = useInbox(); inbox.enabled = true; inbox.byAlbum[7] = [p]
  const editor = useEditor()
  editor.album = { id: 7, tracks } as unknown as NonNullable<typeof editor.album>
  const router = createAppRouter(createMemoryHistory()); await router.push('/library/7')
  const options = { global: { plugins: [router] } }
  const w = layout === 'phone' ? mount(LibPhoneField, { ...options, props: { field: 'originaldate' } }) : mount(LibField, { ...options, props: { f: { field: 'originaldate', label: 'Original date' } } })
  await flushPromises()
  expect(w.get('.sug').text()).toContain('Claude at home')
  expect(w.get('.sug').text()).toContain('86%')
  await w.get('.sug').trigger('click')
  expect(saveBody(tracks, editor.stage).tracks.map((t) => t.edits)).toEqual(tracks.map(() => [{ field: 'originaldate', values: ['1988-09-12'] }]))
  w.unmount()
  vi.unstubAllGlobals()
})


test('proposal tapes preserve an anchor through an ordinary Save', () => {
  const p = proposal(); const op = p.current.ops[0]!
  if (op.type !== 'tag') throw new Error('tag')
  op.anchor = 'release-id'
  const stage = accept({}, tracks, tracks, proposalSuggestions([p], tracks)[0]!, true)
  expect(saveBody(tracks, stage).tracks[0]!.edits[0]!.anchor).toBe('release-id')
})
