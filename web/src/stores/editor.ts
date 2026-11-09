import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { ApiError, errorText, get, post } from '../api/client'
import type { LibraryAlbumDetail, LibraryChange, LibraryTrack, SaveRefusal, SaveWarning, Suggestion, Suggestions } from '../api/types'
import { useLibrary } from './library'
import { accept, addValue, changes, fix, revert, saveBody, stageEdit, type CoverEdit, type Stage, type Target } from '../lib/tags'

// StagedCover is a cover edit waiting for Save, with what the editor previews.
export interface StagedCover extends CoverEdit {
  preview?: string
}

// Outcome is how the last Save ended, for the states under the editor.
export interface Outcome {
  ok: boolean
  // deleting says the outcome is a Delete's, not a Save's.
  deleting?: boolean
  reason: string
  message: string
  // changes and tracks say what a Save held, as the Save bar counted it.
  changes: number
  tracks: number
  cover: boolean
}

interface Held {
  stage: Stage
  cover: StagedCover | null
}

const terminal = (c: LibraryChange) => c.state === 'done' || c.state === 'partial' || c.state === 'failed'

// The opened album, its staged edits and its Save.
export const useEditor = defineStore('editor', () => {
  const album = ref<LibraryAlbumDetail | null>(null)
  const loading = ref(false)
  const error = ref('')
  const trackId = ref<number | null>(null)
  // cursor is the field the keyboard acts on, and noteOn the field showing its note.
  const cursor = ref<Target | null>(null)
  const noteOn = ref<Target | null>(null)
  const suggestions = ref<Suggestions | null>(null)
  const suggesting = ref(false)
  const suggestError = ref('')
  // held keeps staged edits per album, so opening another album loses none.
  const held = ref<Record<number, Held>>({})
  const saving = ref<LibraryChange | null>(null)
  const deleting = ref<LibraryChange | null>(null)
  // gone is an album a Delete removed, and the page leaves it.
  const gone = ref<number | null>(null)
  const outcome = ref<Outcome | null>(null)
  const asks = ref<SaveWarning[] | null>(null)
  // askKey is the dialog's confirm key, because an undo or redo uses the dialog too.
  const askKey = ref('Save anyway')
  // coverRev changes the cover's address, so a new cover shows at once.
  const coverRev = ref(0)
  let pending: ((confirm: string[]) => Promise<void>) | null = null
  let coverOnly = false
  let sent = { changes: 0, tracks: 0 }
  // moved is a finished change whose album may get a new ID when the walk ends.
  let moved: { change: number; album: number } | null = null
  let opening: AbortController | null = null
  let suggestingFor: AbortController | null = null

  const here = computed<Held>(() => (album.value && held.value[album.value.id]) || { stage: {}, cover: null })
  const stage = computed(() => here.value.stage)
  const cover = computed(() => here.value.cover)

  function hold(next: Partial<Held>) {
    if (album.value) held.value = { ...held.value, [album.value.id]: { ...here.value, ...next } }
  }

  const tracks = computed(() => album.value?.tracks ?? [])
  const readable = computed(() => tracks.value.filter((t) => !t.error))
  const selected = computed(() => tracks.value.find((t) => t.id === trackId.value) ?? null)
  // scope is the tracks an edit reaches.
  const scope = computed<LibraryTrack[]>(() => (selected.value ? [selected.value] : readable.value))
  const staged = computed(() => changes(stage.value) + (cover.value ? 1 : 0))
  // kept counts the staged edits of every album, which a session that ends keeps.
  const kept = computed(() => Object.values(held.value).reduce((n, h) => n + changes(h.stage) + (h.cover ? 1 : 0), 0))
  const coverUrl = computed(() => (album.value ? `/api/library/albums/${album.value.id}/cover?v=${coverRev.value}` : ''))

  async function read(id: number, signal?: AbortSignal) {
    return get<LibraryAlbumDetail>(`/api/library/albums/${id}`, signal)
  }

  async function open(id: number) {
    if (album.value?.id === id) return
    opening?.abort()
    const mine = new AbortController()
    opening = mine
    album.value = null
    trackId.value = null
    suggestions.value = null
    outcome.value = null
    asks.value = null
    loading.value = true
    error.value = ''
    try {
      album.value = await read(id, mine.signal)
      void loadSuggestions()
    } catch (err) {
      if (!mine.signal.aborted) error.value = errorText(err)
    } finally {
      if (opening === mine) loading.value = false
    }
  }

  function close() {
    opening?.abort()
    suggestingFor?.abort()
    album.value = null
    trackId.value = null
    suggestions.value = null
    outcome.value = null
    asks.value = null
    error.value = ''
  }

  // reload reads the files again and keeps what is staged.
  async function reload(keepOutcome = false) {
    if (!album.value) return
    album.value = await read(album.value.id)
    if (!keepOutcome) outcome.value = null
  }

  async function loadSuggestions(anchor?: string) {
    if (!album.value) return
    suggestingFor?.abort()
    const mine = new AbortController()
    suggestingFor = mine
    suggesting.value = true
    suggestError.value = ''
    const q = anchor ? `?anchor=${encodeURIComponent(anchor)}` : ''
    try {
      suggestions.value = await get<Suggestions>(`/api/library/albums/${album.value.id}/suggestions${q}`, mine.signal)
    } catch (err) {
      if (!mine.signal.aborted) suggestError.value = errorText(err)
    } finally {
      if (suggestingFor === mine) suggesting.value = false
    }
  }

  // A fingerprint job has news of an album, which matters only while it's open.
  function suggestionsChanged(id: number) {
    if (album.value?.id === id) void loadSuggestions()
  }

  const edit = (target: Target, values: string[]) => hold({ stage: stageEdit(stage.value, scope.value, target, values) })
  const drop = (target: Target) => hold({ stage: revert(stage.value, scope.value, target) })
  const add = (target: Target, value: string) => hold({ stage: addValue(stage.value, scope.value, target, value) })
  const take = (s: Suggestion, multi: boolean) => hold({ stage: accept(stage.value, readable.value, scope.value, s, multi) })
  const stageCover = (c: StagedCover | null) => hold({ cover: c })
  const discard = () => hold({ stage: {}, cover: null })

  function fixable(issue: { kind: string; fields?: string[] }) {
    return fix(issue, readable.value, suggestions.value, stage.value) !== null
  }

  function applyFix(issue: { kind: string; fields?: string[] }) {
    const next = fix(issue, readable.value, suggestions.value, stage.value)
    if (next) hold({ stage: next })
  }

  async function send(body: ReturnType<typeof saveBody>, again: (confirm: string[]) => Promise<void>) {
    if (!album.value) return
    outcome.value = null
    sent = { changes: changes(stage.value), tracks: body.tracks.length }
    try {
      saving.value = await post<LibraryChange>(`/api/library/albums/${album.value.id}/save`, body)
      asks.value = null
      pending = null
    } catch (err) {
      const refusal = err instanceof ApiError ? (err.body as SaveRefusal | undefined) : undefined
      if (refusal?.reason === 'confirm') {
        ask(refusal.confirm ?? [], again)
        return
      }
      asks.value = null
      outcome.value = failure(refusal?.reason ?? 'error', errorText(err))
    }
  }

  async function save(confirm: string[] = []) {
    coverOnly = false
    const c = cover.value
    const coverEdit = c ? { ...(c.upload ? { upload: c.upload } : {}), ...(c.delete ? { delete: true } : {}) } : undefined
    await send(saveBody(readable.value, stage.value, confirm, coverEdit), save)
  }

  // saveCoverAlone writes a cover at once as its own undo step, and staged edits
  // keep waiting.
  async function saveCoverAlone(upload: string, confirm: string[] = []) {
    coverOnly = true
    await send(saveBody(readable.value, {}, confirm, { upload }), (c) => saveCoverAlone(upload, c))
  }

  const failure = (reason: string, message: string, del = false): Outcome => ({ ok: false, deleting: del, reason, message, changes: 0, tracks: 0, cover: false })

  // remove deletes the album's release folder, once the owner confirms.
  async function remove(confirm: string[] = []) {
    if (!album.value) return
    outcome.value = null
    try {
      deleting.value = await post<LibraryChange>(`/api/library/albums/${album.value.id}/delete`, { confirm })
      asks.value = null
      pending = null
    } catch (err) {
      const refusal = err instanceof ApiError ? (err.body as SaveRefusal | undefined) : undefined
      if (refusal?.reason === 'confirm') {
        ask(refusal.confirm ?? [], remove, 'Delete')
        return
      }
      asks.value = null
      outcome.value = failure(refusal?.reason ?? 'error', errorText(err), true)
    }
  }

  function settleDelete(c: LibraryChange) {
    deleting.value = null
    void useLibrary().load()
    if (c.state === 'failed') {
      outcome.value = failure(c.reason, c.error, true)
      return
    }
    if (c.state === 'partial') {
      outcome.value = failure('partial', c.error, true)
      return
    }
    const { [c.album_id]: _, ...rest } = held.value
    held.value = rest
    gone.value = c.album_id
  }

  function ask(warnings: SaveWarning[], again: (confirm: string[]) => Promise<void>, key = 'Save anyway') {
    asks.value = warnings
    askKey.value = key
    pending = again
  }

  const confirmAsks = () => pending?.(asks.value?.map((w) => w.kind) ?? [])
  const cancelAsks = () => {
    asks.value = null
    pending = null
  }

  async function settle(c: LibraryChange) {
    saving.value = null
    if (c.state === 'done') {
      outcome.value = { ok: true, reason: '', message: '', ...sent, changes: coverOnly ? 0 : sent.changes, cover: c.cover }
      coverRev.value++
      if (album.value) moved = { change: c.id, album: album.value.id }
      if (coverOnly) {
        await takeRevisions(c.id)
        hold({ cover: null })
      } else {
        hold({ stage: {}, cover: null })
        await refresh(c)
        void loadSuggestions()
      }
      void useLibrary().load()
      return
    }
    outcome.value = failure(c.reason, c.error)
    // A partial write moved some revisions, and the staged edits go out with the new ones.
    if (c.state === 'partial') await refresh(c)
  }

  // A cover write moves the revisions of staged edits. Reopening would also
  // accept a change another tool made in between, so they come from the change.
  async function takeRevisions(id: number) {
    const c = await get<LibraryChange>(`/api/library/changes/${id}`)
    if (!album.value) return
    const next = new Map((c.files ?? []).filter((f) => f.kind === 'audio' && f.written).map((f) => [f.track_id, f.revision]))
    album.value = { ...album.value, tracks: album.value.tracks.map((t) => ({ ...t, revision: next.get(t.id) ?? t.revision })) }
  }

  // A Save can give the album a new ID, and the old one then answers 404.
  async function refresh(c: LibraryChange) {
    if (!album.value) return
    const id = c.album_id || album.value.id
    const kept = outcome.value
    try {
      album.value = await read(id)
    } catch {
      return
    }
    outcome.value = kept
  }

  // A Save that changes the album's Navidrome ID gives it a new row at the next
  // walk, and the old ID then answers 404. The staged edits go with the album.
  async function follow() {
    const m = moved
    moved = null
    if (!m || album.value?.id !== m.album) return
    const c = await get<LibraryChange>(`/api/library/changes/${m.change}`)
    if (c.album_id === m.album || album.value?.id !== m.album) return
    const kept = held.value[m.album]
    const next = await read(c.album_id)
    if (kept) {
      const { [m.album]: _, ...rest } = held.value
      held.value = { ...rest, [c.album_id]: kept }
    }
    album.value = next
  }

  // afterReverse catches up with an undo or redo, which can move the album's ID
  // back as a Save moved it.
  async function afterReverse(c: LibraryChange) {
    if (!album.value) return
    moved = { change: c.id, album: album.value.id }
    coverRev.value++
    await reload().catch(() => {})
  }

  function applyChange(c: LibraryChange) {
    if (deleting.value?.id === c.id) {
      deleting.value = c
      if (terminal(c)) settleDelete(c)
      return
    }
    if (saving.value?.id !== c.id) return
    saving.value = c
    if (terminal(c)) void settle(c)
  }

  // The event stream has no replay, so a reconnect asks about a running Save.
  async function recheck() {
    const c = saving.value ?? deleting.value
    if (!c) return
    applyChange(await get<LibraryChange>(`/api/library/changes/${c.id}`))
  }

  return {
    album, loading, error, trackId, cursor, noteOn, suggestions, suggesting, suggestError, saving, deleting, gone, outcome, asks, askKey, coverRev,
    held, stage, cover, tracks, readable, selected, scope, staged, kept, coverUrl,
    open, close, reload, loadSuggestions, suggestionsChanged, edit, drop, add, take, stageCover, discard, fixable, applyFix,
    save, saveCoverAlone, remove, ask, confirmAsks, cancelAsks, applyChange, recheck, follow, afterReverse,
  }
})
