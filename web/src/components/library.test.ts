import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { nextTick } from 'vue'
import { createMemoryHistory } from 'vue-router'
import App from '../App.vue'
import type { LibraryAlbum, LibraryAlbumDetail, LibraryChange, LibraryTrack } from '../api/types'
import { createAppRouter } from '../router'
import { useLibrary } from '../stores/library'
import { download, FakeEventSource, list, Reply, source, stubApi } from '../testing/api'

function track(id: number, over: Partial<LibraryTrack> = {}): LibraryTrack {
  return {
    id, path: `rock/x/${id}.flac`, disc: 1, track: id, title: `Song ${id}`, artist: 'A', duration: 61, quality: 'FLAC 24/96', format: 'flac', size: 1, revision: `r${id}`, error: '', issues: [],
    extended: [{ key: 'UPC', values: ['123'] }], pictures: [],
    standard: [
      { key: 'TITLE', fields: ['title'], values: [`Song ${id}`] },
      { key: 'ALBUM', fields: ['album'], values: ['Blue'] },
      { key: 'GENRE', fields: ['genre'], values: id === 1 ? ['Rock'] : ['Jazz'] },
      { key: 'DATE', fields: ['recordingdate'], values: ['2006'] },
      { key: 'ORIGINALDATE', fields: ['originaldate'], values: ['1958'] },
    ],
  }
}
const row = (over: Partial<LibraryAlbum> = {}): LibraryAlbum => ({
  id: 7, folder: 'rock/x', title: 'Blue', artist: 'Joni', year: 1971, tracks: 2, quality: 'FLAC 24/96', duration: 122, size: 2, added_at: '', navidrome_id: 'n', has_mbid: false, lamp: true,
  issues: [{ kind: 'no_releasedate', fields: ['releasedate'] }, { kind: 'guide_replaygain' }], source: { id: 'qobuz', name: 'Qobuz', by: 'download', download_id: 418, at: '2026-11-03T11:05:11Z' }, ...over,
})
const detail = (over: Partial<LibraryAlbumDetail> = {}): LibraryAlbumDetail => ({ ...row(), tracks: [track(1), track(2)], stale: false, ...over })
const suggestions = {
  anchor: null, tracks: [], covers: [], notes: [{ field: 'genre', message: 'Navidrome shows the first genre.' }], unavailable: [{ source: 'MusicBrainz', message: 'down' }],
  album: [{ field: 'genre', values: ['Folk'], source: 'Qobuz', sources: ['Qobuz'], confidence: 71 }],
}
const routes = (over: Record<string, unknown> = {}) => ({
  '/api/downloads': list([]),
  '/api/sources': [source('qobuz', 'Qobuz')],
  '/api/library/scan': { enabled: false },
  '/api/telegram': { enabled: false, connected: false, username: '', users: 0 },
  '/api/library/index': { indexing: false, ready: true, albums: 1, tracks: 2, read: 2, error: '' },
  '/api/library/musicbrainz': { ok: true, message: '', latency_ms: 120, checked_at: '' },
  '/api/library/filters': { albums: 1, issues: 1, no_mbid: 1, kinds: [{ name: 'no_releasedate', albums: 1 }], folders: [{ name: 'rock', albums: 1 }], sources: [{ id: 'qobuz', name: 'Qobuz', albums: 1 }], no_source: 2 },
  '/api/library/keys': { keys: ['TITLE', 'BPM'] },
  '/api/library/albums/7/suggestions': suggestions,
  '/api/library/albums/7': detail(),
  '/api/library/albums': { albums: [row()], total: 1, limit: 100, offset: 0 },
  ...over,
})

beforeEach(() => {
  localStorage.clear()
  FakeEventSource.all = []
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => vi.unstubAllGlobals())

async function at(path: string, over: Record<string, unknown> = {}) {
  const calls = stubApi(routes(over))
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createAppRouter(createMemoryHistory())
  await router.push(path)
  const w = mount(App, { global: { plugins: [pinia, router] }, attachTo: document.body })
  await flushPromises()
  return { w, router, calls }
}
const button = (w: Awaited<ReturnType<typeof at>>['w'], name: string) => w.findAll('button').find((b) => b.text() === name || b.attributes('aria-label') === name)!
const key = async (k: string, init: KeyboardEventInit = {}) => {
  document.body.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, ...init }))
  await flushPromises()
}

test('the Library tab lists albums with their quality and a lamp for real issues only', async () => {
  const { w } = await at('/library', { '/api/library/albums': { albums: [row(), row({ id: 8, title: 'Hejira', lamp: false, issues: [{ kind: 'guide_replaygain' }] })], total: 2, limit: 100, offset: 0 } })
  expect(w.findAll('nav a').map((a) => a.text())).toContain('Library')
  const rows = w.findAll('.g-lib:not(.thead)')
  expect(rows[0]!.text()).toContain('Hi-Res 24/96')
  expect(rows[0]!.find('.led.iss').exists()).toBe(true)
  expect(rows[1]!.find('.led.iss').exists()).toBe(false)
  w.unmount()
})

test('the list shows each album\'s source, and the filter menu picks a source or none', async () => {
  const { w, calls } = await at('/library', { '/api/library/albums': { albums: [row(), row({ id: 8, title: 'Hejira', source: { id: '', name: '' } })], total: 2, limit: 100, offset: 0 } })
  const rows = w.findAll('.g-lib:not(.thead)')
  expect(rows[0]!.find('.lib-src').text()).toBe('Qobuz')
  expect(rows[0]!.find('.lib-src').attributes('title')).toContain('Downloaded from Qobuz')
  expect(rows[1]!.find('.lib-src').text()).toBe('—')
  await button(w, 'Filter: All albums').trigger('click')
  const menu = w.find('[role=menu]')
  expect(menu.text()).toContain('Sources')
  expect(menu.text()).toContain('No known source')
  await menu.findAll('button').find((b) => b.text().startsWith('Qobuz'))!.trigger('click')
  await flushPromises()
  expect(calls.some((c) => c.url.includes('/api/library/albums?') && c.url.includes('source=qobuz'))).toBe(true)
  expect(button(w, 'Filter: Qobuz').exists()).toBe(true)
  await button(w, 'Filter: Qobuz').trigger('click')
  await w.find('[role=menu]').findAll('button').find((b) => b.text().startsWith('No known source'))!.trigger('click')
  await flushPromises()
  expect(calls.some((c) => c.url.includes('source=none'))).toBe(true)
  w.unmount()
})

test('an opened album says where it came from, with links to its download and its page', async () => {
  const { w } = await at('/library/7', { '/api/library/albums/7': detail({ source: { id: 'qobuz', name: 'Qobuz', by: 'download', download_id: 418, at: '2026-11-03T11:05:11Z', link: 'https://www.qobuz.com/album/x/1' } }) })
  const from = w.find('.lib-from')
  expect(from.text()).toContain('Downloaded from Qobuz on 3\u00a0November\u00a02026')
  expect(from.find('a[href="/downloads/418"]').text()).toBe('Download 418')
  expect(from.find('a[href="https://www.qobuz.com/album/x/1"]').text()).toBe('Open on Qobuz')
  w.unmount()
})

test('an album moved out of its recorded folder says where the record has it, and an import has no download link', async () => {
  const { w } = await at('/library/7', { '/api/library/albums/7': detail({ source: { id: 'qobuz', name: 'Qobuz', by: 'import', recorded: 'qobuz/Blue' } }) })
  const from = w.find('.lib-from')
  expect(from.text()).toContain("From Qobuz, in the old bot's history")
  expect(from.text()).toContain('Recorded in qobuz/Blue')
  expect(from.find('a').exists()).toBe(false)
  w.unmount()
})

test('no match says what was searched, and Clear brings the list back', async () => {
  let empty = true
  const { w } = await at('/library', { '/api/library/albums': () => (empty ? { albums: [], total: 0, limit: 100, offset: 0 } : { albums: [row()], total: 1, limit: 100, offset: 0 }) })
  useLibrary().query = 'northren'
  await nextTick()
  expect(w.text()).toContain('NO ALBUMS')
  expect(w.text()).toContain('Nothing matches "northren".')
  empty = false
  await button(w, 'Clear filter and search').trigger('click')
  await flushPromises()
  expect(w.findAll('.g-lib:not(.thead)').length).toBe(1)
  w.unmount()
})

test('an address with ?q= fills the search and asks for it', async () => {
  const { w, router, calls } = await at('/library?q=blue%20joni')
  expect((w.find('input[type=search]').element as HTMLInputElement).value).toBe('blue joni')
  expect(calls.some((c) => c.url.includes('/api/library/albums?') && c.url.includes('q=blue+joni'))).toBe(true)
  await router.push('/library?q=hejira')
  await new Promise((r) => setTimeout(r, 300))
  await flushPromises()
  expect(useLibrary().query).toBe('hejira')
  expect(calls.some((c) => c.url.includes('q=hejira'))).toBe(true)
  w.unmount()
})

test('the first index shows its progress in place of the list', async () => {
  const { w } = await at('/library', { '/api/library/albums': { albums: [], total: 0, limit: 100, offset: 0 }, '/api/library/index': { indexing: true, ready: false, albums: 0, tracks: 0, read: 412, error: '' } })
  expect(w.text()).toContain('412')
  expect(w.text()).not.toContain('NO ALBUMS')
  w.unmount()
})

test('an opened album shows fields, a tag of one track locked, and Mixed for differing values', async () => {
  const { w } = await at('/library/7')
  expect(w.find('#lf-title').attributes('readonly')).toBeDefined()
  expect(w.find('#lf-album').attributes('readonly')).toBeUndefined()
  expect((w.find('#lf-album').element as HTMLInputElement).value).toBe('Blue')
  expect(w.find('#lf-genre').attributes('placeholder')).toBe('Mixed')
  expect(w.text()).toContain('ALL 2 TRACKS')
  w.unmount()
})

test('the Compilation switch shows Mixed with a level paddle, and pressing it stages On', async () => {
  const flagged = (id: number, on: boolean) => {
    const t = track(id)
    return { ...t, standard: [...t.standard, { key: 'COMPILATION', fields: ['compilation'], values: on ? ['1'] : [] }] }
  }
  const { w } = await at('/library/7', { '/api/library/albums/7': detail({ tracks: [flagged(1, true), flagged(2, false)] }) })
  const sw = w.find('#lf-compilation')
  expect(sw.attributes('aria-checked')).toBe('false')
  expect(sw.classes()).toContain('mixed')
  expect(sw.text()).toBe('Mixed')
  await sw.trigger('click')
  expect(w.find('#lf-compilation').attributes('aria-checked')).toBe('true')
  expect(w.find('#lf-compilation').classes()).not.toContain('mixed')
  expect(w.find('#lf-compilation').text()).toBe('On')
  w.unmount()
})

test('selecting a track narrows the edit and unlocks its tags', async () => {
  const { w } = await at('/library/7')
  await w.findAll('.g-lt .lib-open')[2]!.trigger('click')
  expect(w.text()).toContain('TRACK 02 ONLY')
  expect(w.find('#lf-title').attributes('readonly')).toBeUndefined()
  expect((w.find('#lf-title').element as HTMLInputElement).value).toBe('Song 2')
  w.unmount()
})

test('a staged edit shows its old value and a Save bar, and Revert clears both', async () => {
  const { w } = await at('/library/7')
  expect(w.find('.lib-save').exists()).toBe(false)
  await w.find('#lf-album').setValue('Blue II')
  expect(w.find('.lf-was').text()).toBe('Was Blue')
  expect(w.find('.lib-save').text()).toContain('1 CHANGE')
  await button(w, 'Revert Album').trigger('click')
  expect(w.find('.lib-save').exists()).toBe(false)
  w.unmount()
})

test('a suggestion, a note and a silent service all show, and pressing one adds a value', async () => {
  const { w } = await at('/library/7')
  expect(w.text()).toContain("MusicBrainz doesn't answer")
  expect(w.text()).toContain('Navidrome shows the first genre.')
  await w.find('.sug').trigger('click')
  expect(w.find('.lib-save').text()).toContain('1 CHANGE')
  w.unmount()
})

test('Fix stages the date pair, and the fields show it for review', async () => {
  const { w } = await at('/library/7')
  await button(w, 'Fix').trigger('click')
  expect((w.find('#lf-releasedate').element as HTMLInputElement).value).toBe('2006')
  expect((w.find('#lf-recordingdate').element as HTMLInputElement).value).toBe('1958')
  expect(w.find('.lib-save').text()).toContain('2 CHANGES')
  w.unmount()
})

test('guide issues stay out of the album, and a reissue year has no Fix', async () => {
  const { w } = await at('/library/7', { '/api/library/albums/7': detail({ issues: [{ kind: 'reissue_year' }, { kind: 'guide_replaygain' }] }) })
  expect(w.findAll('.lib-iss').length).toBe(1)
  expect(button(w, 'Fix')).toBeUndefined()
  w.unmount()
})

test('a confirm opens as a dialog and Save anyway sends it', async () => {
  let n = 0
  const { w, calls } = await at('/library/7', {
    '/api/library/albums/7/save': () => (n++ === 0 ? new Reply(409, { error: 'c', reason: 'confirm', confirm: [{ kind: 'split', message: 'The star moves.' }] }) : { id: 1, state: 'queued' }),
  })
  await w.find('#lf-album').setValue('Blue II')
  await button(w, 'Save').trigger('click')
  await flushPromises()
  expect(w.find('[role=alertdialog]').text()).toContain('Split this album?')
  await button(w, 'Save anyway').trigger('click')
  await flushPromises()
  expect(w.find('[role=alertdialog]').exists()).toBe(false)
  expect(JSON.parse(calls.filter((c) => c.url.endsWith('/save'))[1]!.init!.body as string).confirm).toEqual(['split'])
  w.unmount()
})

test('an undo that renames an album artist asks first, and Undo anyway sends the confirm', async () => {
  const saved: LibraryChange = {
    id: 9, kind: 'save', target: null, album_id: 7, folder: 'rock/x', title: 'Blue', artist: 'Joni', state: 'done', reason: '', error: '',
    undone: false, tags: 2, cover: false, conflicts: [], job_id: 1, created_at: '2026-11-04T18:00:00Z', finished_at: '2026-11-04T18:00:01Z',
  }
  let n = 0
  const { w, calls } = await at('/library', {
    '/api/library/history': { changes: [saved], next: null },
    '/api/library/changes/9/undo': () => (n++ === 0 ? new Reply(409, { error: 'c', reason: 'confirm', confirm: [{ kind: 'album_artist', message: 'The old name keeps its star.' }] }) : { ...saved, id: 10, kind: 'undo', target: 9, state: 'queued' }),
  })
  await button(w, 'History').trigger('click')
  await flushPromises()
  await button(w, 'Undo').trigger('click')
  await flushPromises()
  expect(w.find('[role=alertdialog]').text()).toContain('Rename the album artist?')
  expect(w.text()).not.toContain('confirm what')
  await button(w, 'Undo anyway').trigger('click')
  await flushPromises()
  expect(w.find('[role=alertdialog]').exists()).toBe(false)
  const undos = calls.filter((c) => c.url.endsWith('/undo'))
  expect(undos[0]!.init!.body).toBeUndefined()
  expect(JSON.parse(undos[1]!.init!.body as string).confirm).toEqual(['album_artist'])
  w.unmount()
})

test.each([
  ['conflict', 'FILES CHANGED', 'Reload album'],
  ['locked', 'LOCKED', ''],
  ['busy', 'BUSY', 'Open the download'],
  ['verify', 'SAVE FAILED', 'Try again'],
])('a %s refusal shows its state and keeps the edit', async (reason, big, action) => {
  const { w } = await at('/library/7', { '/api/library/albums/7/save': new Reply(409, { error: 'read-back differs', reason }) })
  await w.find('#lf-album').setValue('Blue II')
  await button(w, 'Save').trigger('click')
  await flushPromises()
  expect(w.find('.not-saved').text()).toContain(big)
  if (action) expect(w.find('.not-saved').text().toLowerCase()).toContain(action.toLowerCase())
  expect((w.find('#lf-album').element as HTMLInputElement).value).toBe('Blue II')
  w.unmount()
})

test('All fields groups tags, adds a custom field to its group, and sends a key Navidrome reads to Standard', async () => {
  const { w } = await at('/library/7')
  await button(w, 'All fields').trigger('click')
  await flushPromises()
  expect(w.findAll('.lib-h').map((h) => h.text())).toEqual(expect.arrayContaining([expect.stringContaining('Standard'), expect.stringContaining('Extended')]))
  await button(w, 'Add field').trigger('click')
  await w.find('[aria-label="New field\'s key"]').setValue('bpm')
  await w.find('[aria-label="New field\'s value"]').setValue('120')
  await w.find('form').trigger('submit')
  const headers = w.findAll('.lib-fields > *').map((n) => n.text())
  expect(headers.findIndex((t) => t.includes('BPM'))).toBeLessThan(headers.findIndex((t) => t.includes('Extended')))
  w.unmount()
})

test('a custom key that has a row goes to that row', async () => {
  const { w } = await at('/library/7')
  await button(w, 'All fields').trigger('click')
  await flushPromises()
  await button(w, 'Add field').trigger('click')
  await w.find('[aria-label="New field\'s key"]').setValue('upc')
  await w.find('[aria-label="New field\'s value"]').setValue('999')
  await w.find('form').trigger('submit')
  expect(w.findAll('.lf.tagrow').filter((r) => r.text().includes('UPC')).length).toBe(1)
  expect((w.find('#tag-UPC').element as HTMLInputElement).value).toBe('999')
  w.unmount()
})

test('a deleted tag stays in its row, struck through, until Revert', async () => {
  const { w } = await at('/library/7')
  await button(w, 'All fields').trigger('click')
  await flushPromises()
  await button(w, 'Delete UPC').trigger('click')
  expect(w.find('#tag-UPC').classes()).toContain('struck')
  await button(w, 'Revert UPC').trigger('click')
  expect(w.find('#tag-UPC').classes()).not.toContain('struck')
  w.unmount()
})

test('keys act outside a field and leave typing alone', async () => {
  const { w } = await at('/library/7')
  await key('?')
  expect(w.find('[aria-labelledby=dk-t]').exists() || document.querySelector('[aria-labelledby=dk-t]')).toBeTruthy()
  await key('Escape')
  await key('i')
  expect(w.find('.lib-save').text()).toContain('2 CHANGES')
  await key(']')
  expect(w.text()).toContain('TRACK 01 ONLY')
  await key('0')
  expect(w.text()).toContain('ALL 2 TRACKS')
  await key('d', { ctrlKey: true, shiftKey: true })
  expect(w.find('.lib-save').exists()).toBe(false)
  w.unmount()
})

test('the Status page shows the Library part', async () => {
  const { w } = await at('/status')
  expect(w.text()).toContain('1 ALBUM · 2 TRACKS')
  expect(w.text()).toContain('MUSICBRAINZ ANSWERS · 120 MS')
  expect(w.find('[aria-label="Library"] .led').classes()).toContain('ok')
  w.unmount()
})

test('the Library part warns while MusicBrainz is not answering', async () => {
  const { w } = await at('/status', { '/api/library/musicbrainz': { ok: false, message: 'MusicBrainz is not answering', latency_ms: 0, checked_at: '' } })
  expect(w.text()).toContain('MUSICBRAINZ IS NOT ANSWERING')
  expect(w.text()).toContain('Editing and saving still work.')
  expect(w.find('[aria-label="Library"] .led').classes()).toContain('warn')
  w.unmount()
})

test('pasting a release into the album ID field asks for suggestions from it', async () => {
  const id = '8d2338a8-5e0e-4e3b-9f3e-6b1e4b8f6c11'
  const t = track(1)
  t.standard.push({ key: 'MUSICBRAINZ_ALBUMID', fields: ['musicbrainz_albumid'], values: [] })
  const { w, calls } = await at('/library/7', { '/api/library/albums/7': detail({ tracks: [t] }) })
  await button(w, 'All fields').trigger('click')
  await flushPromises()
  await w.find('#tag-MUSICBRAINZ_ALBUMID').setValue(`https://musicbrainz.org/release/${id}`)
  await flushPromises()
  expect(calls.some((c) => c.url.endsWith(`/suggestions?anchor=${id}`))).toBe(true)
  w.unmount()
})

test('a release that fails to match says why', async () => {
  const anchor = { id: 'x', by: 'staged', title: 'Blue', artist: 'Joni', date: '', country: '', tracks: 12, url: '', matched: false, reason: 'The release has 12 tracks and the album 10.' }
  const { w } = await at('/library/7', { '/api/library/albums/7/suggestions': { ...suggestions, anchor } })
  expect(w.text()).toContain('The release has 12 tracks and the album 10.')
  w.unmount()
})

test('a long value opens the long-form editor, and Done stages it', async () => {
  const t = track(1)
  t.standard.push({ key: 'ALBUMARTIST', fields: ['albumartist'], values: ['x'.repeat(120)] })
  const { w } = await at('/library/7', { '/api/library/albums/7': detail({ tracks: [t] }) })
  await w.find('#lf-albumartist').trigger('click')
  await flushPromises()
  await w.find('textarea').setValue('Short')
  await button(w, 'Done').trigger('click')
  expect(w.find('.lib-save').text()).toContain('1 CHANGE')
  w.unmount()
})

test('a key does nothing behind an open dialog, and Esc closes it', async () => {
  const { w } = await at('/library/7')
  await key('c')
  expect(document.querySelector('[aria-labelledby=dc-t]')).toBeTruthy()
  await key('i')
  expect(w.find('.lib-save').exists()).toBe(false)
  await key('Escape')
  expect(document.querySelector('[aria-labelledby=dc-t]')).toBeNull()
  w.unmount()
})

test('the Open in Library link finds a finished download by its folder', async () => {
  const d = { ...download(5, { status: 'done', folder: 'rock/x' }), children: [], tracks: [], log: [] }
  const { w, router, calls } = await at('/downloads/5', { '/api/downloads/5': d })
  await button(w, 'Open in Library').trigger('click')
  await flushPromises()
  expect(calls.some((c) => c.url.includes('folder=rock%2Fx'))).toBe(true)
  expect(router.currentRoute.value.path).toBe('/library/7')
  w.unmount()
})

test('a file the browser refuses plays through the transcode, and a second failure says so', async () => {
  const { w } = await at('/library/7')
  await w.findAll('.g-lt .key')[0]!.trigger('click')
  await flushPromises()
  const audio = () => w.find('audio')
  expect(audio().attributes('src')).toBe('/api/library/tracks/1/audio')
  await audio().trigger('error')
  await flushPromises()
  expect(audio().attributes('src')).toBe('/api/library/tracks/1/audio?transcode=1')
  expect(w.find('.lib-scrub').attributes('disabled')).toBeDefined()
  await audio().trigger('error')
  await flushPromises()
  expect(w.find('.lib-player').text()).toContain("THIS BROWSER CAN'T PLAY THIS FILE.")
  w.unmount()
})

test('Delete asks in a dialog, shows the delete running, and goes back to the list when it ends', async () => {
  let n = 0
  const deleted = { files: 2, bytes: 2, albums: [], forgot: 0, navidrome: 'waiting', navidrome_tracks: 0 }
  const queued = { id: 4, kind: 'delete', album_id: 7, state: 'queued', deleted, conflicts: [] }
  const { w, router, calls } = await at('/library/7', {
    '/api/library/albums/7/delete': () => (n++ === 0 ? new Reply(409, { error: 'c', reason: 'confirm', confirm: [{ kind: 'delete', message: 'This deletes 2 files.' }] }) : queued),
  })
  await button(w, 'Delete').trigger('click')
  await flushPromises()
  const dialog = w.find('[role=alertdialog]')
  expect(dialog.text()).toContain('Delete this album?')
  expect(dialog.text()).toContain('This deletes 2 files.')
  await dialog.findAll('button').find((b) => b.text() === 'Delete')!.trigger('click')
  await flushPromises()
  expect(w.find('[role=alertdialog]').exists()).toBe(false)
  expect(JSON.parse(calls.filter((c) => c.url.endsWith('/delete'))[1]!.init!.body as string).confirm).toEqual(['delete'])
  expect(w.text()).toContain('DELETING 2 FILES')
  FakeEventSource.all[0]!.emit('change', { ...queued, state: 'done' })
  await flushPromises()
  expect(router.currentRoute.value.name).toBe('library')
  w.unmount()
})
