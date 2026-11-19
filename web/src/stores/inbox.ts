import { defineStore } from 'pinia'
import { computed, reactive, ref } from 'vue'
import type { LocationQuery } from 'vue-router'
import {
  errorCode,
  listOf,
  newRequestId,
  type AcceptAnswer,
  type CoverOp,
  type InboxInfo,
  type Op,
  type OpDecision,
  type Preview,
  type PreviewAnswer,
  type ProposalDetail,
  type ProposalPage,
  type ProposalSummary,
  type Refusal,
  type RevisionOp,
  type TagOp,
  type Warning,
} from '../api/agents'
import { ApiError, errorText, get, post } from '../api/client'
import { previewCounts } from '../lib/inbox'

export type StateFilter = 'pending' | 'reviewed' | 'withdrawn' | 'all'
export interface Filters {
  state: StateFilter
  album: number | null
  kind: '' | 'tag' | 'cover'
  connection: number | null
}

const states: StateFilter[] = ['pending', 'reviewed', 'withdrawn', 'all']
const one = (v: unknown) => (Array.isArray(v) ? v[0] : v)
const id = (v: unknown) => {
  const n = Number(one(v))
  return Number.isInteger(n) && n > 0 ? n : null
}

// The list's filters live in the address, so a filtered inbox has a link like any other page.
export function filtersFromQuery(q: LocationQuery): Filters {
  const state = String(one(q.state) ?? '')
  const kind = String(one(q.kind) ?? '')
  return {
    state: states.includes(state as StateFilter) ? (state as StateFilter) : 'pending',
    album: id(q.album),
    kind: kind === 'tag' || kind === 'cover' ? kind : '',
    connection: id(q.agent),
  }
}

export function filtersToQuery(f: Filters): Record<string, string> {
  const q: Record<string, string> = {}
  if (f.state !== 'pending') q.state = f.state
  if (f.album) q.album = String(f.album)
  if (f.kind) q.kind = f.kind
  if (f.connection) q.agent = String(f.connection)
  return q
}

// A draft is the reviewer's values for one op that aren't a revision yet.
export interface Draft {
  tracks: Record<number, string[]>
  dropped: number[]
  every_track?: boolean
}

const sorted = (xs: string[]) => [...xs].sort()
const sameList = (a: string[], b: string[]) => a.length === b.length && a.every((v, i) => v === b[i])
// An empty input is a value the reviewer hasn't typed yet; every other value is sent as typed, never split.
const typed = (values: string[]) => values.filter((v) => v.trim() !== '')

// retryable is an accepted op whose write failed before it wrote anything, the one case the server accepts again.
export function retryable(d: ProposalDetail, op: OpDecision): boolean {
  if (op.decision !== 'accepted' || op.acceptance == null) return false
  const a = d.acceptances.find((x) => x.id === op.acceptance)
  return a?.outcome.state === 'failed' && !a.via_save && !a.outcome.expired && !a.outcome.undone && (a.outcome.written ?? 0) === 0
}

// held is a pending field the files already hold. The Inbox hides it and counts it nowhere, but keeps it on, so Accept
// marks it accepted with the fields that write and the proposal can close.
export function held(d: ProposalDetail, opId: string): boolean {
  return !!d.live?.op_unchanged?.[opId] && d.ops.some((o) => o.id === opId && o.decision === 'pending')
}

export function selectable(d: ProposalDetail, opId: string): boolean {
  if (d.state === 'withdrawn') return false
  const op = d.ops.find((o) => o.id === opId)
  if (!op || !d.current.ops.some((o) => o.id === opId)) return false
  return op.decision === 'pending' || retryable(d, op)
}

// needs is every op an op requires, directly or through another op.
export function needs(ops: Op[], opId: string): string[] {
  const out = new Set<string>()
  const walk = (id: string) => {
    for (const r of ops.find((o) => o.id === id)?.requires ?? []) if (r !== opId && !out.has(r)) { out.add(r); walk(r) }
  }
  walk(opId)
  return [...out]
}

// ordered lists a required op above the ops that need it and keeps the agent's order otherwise.
export function ordered(ops: Op[]): Op[] {
  const out: Op[] = []
  const seen = new Set<string>()
  const visit = (op: Op, path: Set<string>) => {
    if (seen.has(op.id) || path.has(op.id)) return
    path.add(op.id)
    for (const r of op.requires ?? []) {
      const dep = ops.find((o) => o.id === r)
      if (dep) visit(dep, path)
    }
    seen.add(op.id)
    out.push(op)
  }
  for (const op of ops) visit(op, new Set())
  return out
}

// Cascade says which fields moved with the one the reviewer switched, because one requires the other.
export interface Cascade {
  op: string
  on: boolean
  moved: string[]
}

export const useInbox = defineStore('inbox', () => {
  // enabled is null until GET /api/inbox answers. A 404 means the feature is off, and nothing shows.
  const enabled = ref<boolean | null>(null)
  const info = ref<InboxInfo | null>(null)

  async function detect() {
    try {
      info.value = await get<InboxInfo>('/api/inbox')
      enabled.value = true
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        enabled.value = false
        info.value = null
      } else if (enabled.value === null) {
        enabled.value = false
      }
    }
  }

  // The list.
  const filters = reactive<Filters>({ state: 'pending', album: null, kind: '', connection: null })
  const proposals = ref<ProposalSummary[]>([])
  const next = ref<number | null>(null)
  const listLoading = ref(false)
  const listError = ref('')
  let listSeq = 0
  let listLoaded = false

  function listPath(before?: number) {
    const p = new URLSearchParams({ state: filters.state })
    if (filters.album) p.set('album', String(filters.album))
    if (filters.kind) p.set('kind', filters.kind)
    if (filters.connection) p.set('connection', String(filters.connection))
    if (before != null) p.set('before', String(before))
    p.set('limit', '50')
    return `/api/inbox/proposals?${p}`
  }

  async function loadList() {
    const seq = ++listSeq
    listLoading.value = true
    listError.value = ''
    try {
      const page = await get<ProposalPage>(listPath())
      if (seq !== listSeq) return
      proposals.value = page.proposals
      next.value = page.next ?? null
      listLoaded = true
    } catch (err) {
      if (seq === listSeq) listError.value = errorText(err)
    } finally {
      if (seq === listSeq) listLoading.value = false
    }
  }

  async function moreList() {
    if (next.value === null || listLoading.value) return
    const seq = listSeq
    listLoading.value = true
    try {
      const page = await get<ProposalPage>(listPath(next.value))
      if (seq !== listSeq) return
      proposals.value = [...proposals.value, ...page.proposals]
      next.value = page.next ?? null
    } catch (err) {
      listError.value = errorText(err)
    } finally {
      if (seq === listSeq) listLoading.value = false
    }
  }

  function setFilters(f: Filters) {
    const same = f.state === filters.state && f.album === filters.album && f.kind === filters.kind && f.connection === filters.connection
    Object.assign(filters, f)
    if (!same || !listLoaded) void loadList()
  }

  // The proposal under review.
  const detailId = ref<number | null>(null)
  const detail = ref<ProposalDetail | null>(null)
  const detailLoading = ref(false)
  const detailRefreshing = ref(false)
  const detailError = ref('')
  const selected = ref<string[]>([])
  const drafts = ref<Record<string, Draft>>({})
  const selection = ref<PreviewAnswer | null>(null)
  // selectionKey says which revision and ops the selection preview answers for.
  const selectionKey = ref('')
  const reviewRequired = ref(false)
  const lastAccepted = ref<AcceptAnswer | null>(null)
  const previewing = ref(false)
  const confirm = ref<string[]>([])
  const cascade = ref<Cascade | null>(null)
  const error = ref('')
  const notice = ref('')
  const busy = ref(false)
  // headSeen is the revision the reviewer's drafts started from, to say when another one arrived.
  const headSeen = ref(0)
  let reviewSeq = 0
  let savingReview: number | null = null
  let detailSeq = 0
  let previewSeq = 0
  let recoveringKey = ''
  let lastPreview: { key: string; digest: string } | null = null
  // attempt keeps the request ID of an acceptance whose answer never came, so pressing Accept again retries it.
  let attempt: { key: string; id: string } | null = null

  function reset() {
    ++reviewSeq
    ++previewSeq
    recoveringKey = ''
    lastPreview = null
    previewing.value = false
    detailRefreshing.value = false
    detail.value = null
    selected.value = []
    drafts.value = {}
    selection.value = null
    selectionKey.value = ''
    reviewRequired.value = false
    lastAccepted.value = null
    confirm.value = []
    cascade.value = null
    error.value = ''
    notice.value = ''
    detailError.value = ''
    attempt = null
    headSeen.value = 0
  }

  async function open(pid: number) {
    if (detailId.value !== pid) {
      detailId.value = pid
      reset()
    }
    await loadDetail()
  }

  function close() {
    detailId.value = null
    ++detailSeq
    reset()
  }

  async function loadDetail() {
    const pid = detailId.value
    if (pid === null) return
    const seq = ++detailSeq
    const review = reviewSeq
    detailLoading.value = !detail.value
    detailRefreshing.value = true
    try {
      const d = await get<ProposalDetail>(`/api/inbox/proposals/${pid}`)
      if (seq !== detailSeq || review !== reviewSeq) return
      take(d)
      detailError.value = ''
    } catch (err) {
      // A failed reload keeps what the page shows and the reviewer's values.
      if (seq === detailSeq) detailError.value = errorText(err)
    } finally {
      if (seq === detailSeq) {
        detailLoading.value = false
        detailRefreshing.value = false
      }
    }
  }

  function checkFiles() {
    error.value = ''
    return loadDetail()
  }

  // take shows a detail and keeps the reviewer's work that still applies to it.
  function take(d: ProposalDetail) {
    const first = detail.value === null || detail.value.id !== d.id
    detail.value = d
    if (first) {
      selected.value = d.ops.filter((o) => selectable(d, o.id)).map((o) => o.id)
      headSeen.value = d.head
    } else {
      selected.value = selected.value.filter((o) => selectable(d, o))
      // A new dependency must not turn back on a field the reviewer left off.
      const removed = selected.value.filter((o) => needs(d.current.ops, o).some((r) => selectable(d, r) && !selected.value.includes(r)))
      if (removed.length) {
        const required = needs(d.current.ops, removed[0]!).find((r) => selectable(d, r) && !selected.value.includes(r))!
        selected.value = selected.value.filter((o) => !removed.includes(o))
        cascade.value = { op: required, on: false, moved: removed }
      }
      if (d.head !== headSeen.value && Object.keys(drafts.value).length > 0) {
        notice.value = `Revision ${d.head} arrived while you edited. Your values are kept; check them against it before you save.`
      }
      if (Object.keys(drafts.value).length === 0) headSeen.value = d.head
    }
    const open = new Set(d.ops.filter((o) => o.decision === 'pending').map((o) => o.id))
    drafts.value = Object.fromEntries(Object.entries(drafts.value).filter(([k]) => open.has(k)))
    void refreshPreview(true)
  }

  const keyOf = (revision: number, ops: string[]) => JSON.stringify([revision, sorted(ops)])

  async function refreshPreview(force = false) {
    const d = detail.value
    if (!d || selected.value.every((o) => held(d, o))) {
      ++previewSeq
      selection.value = null
      selectionKey.value = ''
      previewing.value = false
      return
    }
    const ops = sorted(selected.value)
    const key = keyOf(d.head, ops)
    if (!force && !previewing.value && key === selectionKey.value && selection.value) return
    const seq = ++previewSeq
    previewing.value = true
    try {
      const answer = await post<PreviewAnswer>(`/api/inbox/proposals/${d.id}/preview`, { revision: d.head, ops })
      if (seq !== previewSeq) return
      recoveringKey = ''
      setSelection(answer, key)
    } catch (err) {
      if (seq !== previewSeq) return
      const body = err instanceof ApiError ? err.body : undefined
      const code = errorCode(body)
      if (code === 'stale_revision' || code === 'decided_op') {
        selection.value = null
        selectionKey.value = ''
        reviewRequired.value = true
        error.value = code === 'stale_revision' ? 'The proposal has a new revision. Check it before accepting.' : 'One of the selected fields has already been decided.'
        if (recoveringKey !== key) {
          recoveringKey = key
          void loadDetail()
        }
        return
      }
      if (err instanceof ApiError && err.status === 422) {
        const refusal = (body as { refusal?: Refusal } | undefined)?.refusal ?? { error: err.message, reason: code || 'refused' }
        setSelection({ preview: null, refusal, missing: [] }, key)
        return
      }
      selection.value = null
      selectionKey.value = ''
      error.value = errorText(err)
    } finally {
      if (seq === previewSeq) previewing.value = false
    }
  }

  function setSelection(answer: PreviewAnswer, key: string) {
    const previous = selection.value?.preview?.warnings ?? []
    const digest = answer.refusal ? undefined : answer.preview?.digest
    const changed = digest !== undefined && lastPreview?.key === key && lastPreview.digest !== digest
    if (changed) reviewRequired.value = true
    if (digest !== undefined) lastPreview = { key, digest }
    const unchanged = new Set((answer.preview?.warnings ?? []).filter((w) => previous.some((p) => p.kind === w.kind && p.message === w.message)).map((w) => w.kind))
    selection.value = { preview: answer.preview ?? null, refusal: answer.refusal ?? null, missing: answer.missing ?? [] }
    selectionKey.value = key
    confirm.value = changed ? [] : confirm.value.filter((k) => unchanged.has(k))
  }

  function toggle(opId: string) {
    switchFields([opId], !selected.value.includes(opId), opId)
  }

  function selectAll(on: boolean) {
    const d = detail.value
    if (!d) return
    if (on) {
      switchFields(d.current.ops.map((o) => o.id), true)
      return
    }
    selected.value = d.ops.filter((o) => selectable(d, o.id) && held(d, o.id)).map((o) => o.id)
    cascade.value = null
    error.value = ''
    void refreshPreview()
  }

  // Switch the whole group before asking for a preview, including dependencies in other groups.
  function switchFields(ids: string[], on: boolean, label?: string) {
    const d = detail.value
    if (!d) return
    const targets = new Set(ids.filter((id) => selectable(d, id) && (on || !held(d, id))))
    const previous = new Set(selected.value)
    const next = new Set(previous)
    const required = new Set(on ? [...targets].flatMap((id) => needs(d.current.ops, id)) : [])
    const moved: string[] = []
    for (const id of d.current.ops.map((o) => o.id)) {
      if (!selectable(d, id)) continue
      const linked = on ? required.has(id) : needs(d.current.ops, id).some((dep) => targets.has(dep))
      if (!targets.has(id) && !linked) continue
      if (on) next.add(id)
      else next.delete(id)
      if (!targets.has(id) && previous.has(id) !== on) moved.push(id)
    }
    if (on) {
      for (const o of d.current.ops) {
        if (held(d, o.id) && needs(d.current.ops, o.id).every((id) => !selectable(d, id) || next.has(id))) next.add(o.id)
      }
    }
    selected.value = [...next]
    cascade.value = label && moved.length ? { op: label, on, moved } : null
    error.value = ''
    void refreshPreview()
  }

  function toggleConfirm(kind: string) {
    confirm.value = confirm.value.includes(kind) ? confirm.value.filter((k) => k !== kind) : [...confirm.value, kind]
  }

  // Values the reviewer edits.
  const headOp = (opId: string) => detail.value?.current.ops.find((o) => o.id === opId)

  function values(op: TagOp, trackId: number): string[] {
    return drafts.value[op.id]?.tracks[trackId] ?? op.tracks.find((t) => t.track_id === trackId)?.values ?? []
  }

  function tracksOf(op: TagOp) {
    const dropped = drafts.value[op.id]?.dropped ?? []
    return op.tracks.filter((t) => !dropped.includes(t.track_id))
  }

  function everyTrack(op: CoverOp): boolean {
    return drafts.value[op.id]?.every_track ?? !!op.every_track
  }

  function draftFor(opId: string): Draft {
    return drafts.value[opId] ?? { tracks: {}, dropped: [] }
  }

  function setValues(opId: string, trackIds: number[], next: string[]) {
    const d = draftFor(opId)
    const tracks = { ...d.tracks }
    for (const t of trackIds) tracks[t] = [...next]
    drafts.value = { ...drafts.value, [opId]: { ...d, tracks } }
    prune(opId)
  }

  function dropTrack(opId: string, trackId: number) {
    const d = draftFor(opId)
    drafts.value = { ...drafts.value, [opId]: { ...d, dropped: [...new Set([...d.dropped, trackId])] } }
  }

  function setEveryTrack(opId: string, on: boolean) {
    drafts.value = { ...drafts.value, [opId]: { ...draftFor(opId), every_track: on } }
    prune(opId)
  }

  function discard(opId?: string) {
    if (opId === undefined) drafts.value = {}
    else {
      const { [opId]: _, ...rest } = drafts.value
      drafts.value = rest
    }
    if (Object.keys(drafts.value).length === 0 && detail.value) headSeen.value = detail.value.head
  }

  // revisionOps is what a user revision sends: only the ops whose values differ from the head.
  function revisionOps(): RevisionOp[] {
    const out: RevisionOp[] = []
    for (const [opId, d] of Object.entries(drafts.value)) {
      const op = headOp(opId)
      if (!op) continue
      if (op.type === 'cover') {
        if (d.every_track !== undefined && d.every_track !== !!op.every_track) out.push({ id: opId, every_track: d.every_track })
        continue
      }
      const tracks = op.tracks.filter((t) => !d.dropped.includes(t.track_id)).map((t) => ({ track_id: t.track_id, values: typed(d.tracks[t.track_id] ?? t.values) }))
      const changed = d.dropped.length > 0 || tracks.some((t) => !sameList(t.values, op.tracks.find((x) => x.track_id === t.track_id)!.values))
      if (changed) out.push({ id: opId, tracks })
    }
    return out
  }

  // prune forgets a draft that came back to the head's values.
  function prune(opId: string) {
    const d = drafts.value[opId]
    const op = headOp(opId)
    if (!d || !op) return
    const unchanged =
      op.type === 'cover'
        ? d.every_track === undefined || d.every_track === !!op.every_track
        : d.dropped.length === 0 && Object.entries(d.tracks).every(([t, v]) => sameList(v, op.tracks.find((x) => x.track_id === Number(t))?.values ?? []))
    // Returning to the old head is still an edit against the revision being saved.
    if (unchanged && savingReview !== reviewSeq) discard(opId)
  }

  const dirty = computed(() => revisionOps().length > 0)

  async function saveValues(): Promise<boolean> {
    const d = detail.value
    const ops = revisionOps()
    if (!d || ops.length === 0) return false
    const seq = reviewSeq
    savingReview = seq
    const sent = Object.fromEntries(ops.map((op) => [op.id, JSON.stringify(drafts.value[op.id])]))
    busy.value = true
    error.value = ''
    notice.value = ''
    try {
      const answer = await post<ProposalDetail>(`/api/inbox/proposals/${d.id}/revisions`, { parent_revision: headSeen.value || d.head, ops })
      if (seq !== reviewSeq) return true
      drafts.value = Object.fromEntries(Object.entries(drafts.value).filter(([id, draft]) => sent[id] !== JSON.stringify(draft)))
      headSeen.value = answer.head
      take(answer)
      notice.value = `Saved as revision ${answer.head}. Nothing in the library changed.`
      return true
    } catch (err) {
      if (seq !== reviewSeq) return false
      const code = errorCode(err instanceof ApiError ? err.body : undefined)
      error.value =
        code === 'stale_revision'
          ? 'The proposal changed while you edited. Your values are kept; check the new revision, then save again.'
          : errorText(err)
      if (code === 'stale_revision' || code === 'decided_op') await loadDetail()
      return false
    } finally {
      if (savingReview === seq) savingReview = null
      busy.value = false
    }
  }

  // After a stale save, the reviewer saves on top of the head they now see.
  function rebase() {
    if (detail.value) headSeen.value = detail.value.head
    notice.value = ''
  }

  const current = computed(() => (detail.value ? keyOf(detail.value.head, selected.value) : ''))
  const currentSelection = computed(() => selectionKey.value === current.value ? selection.value : null)
  const preview = computed(() => !previewing.value && !detailRefreshing.value && !detailError.value && !currentSelection.value?.refusal ? currentSelection.value?.preview ?? null : null)
  // shown is the selected fields the reviewer sees, without the held ones.
  const shown = computed(() => {
    const d = detail.value
    return d ? selected.value.filter((o) => !held(d, o)) : []
  })
  const warnings = computed<Warning[]>(() => preview.value?.warnings ?? [])
  const confirmed = computed(() => warnings.value.every((w) => confirm.value.includes(w.kind)))
  function reviewPreview(digest: string) {
    if (!dirty.value && preview.value?.digest === digest) reviewRequired.value = false
  }
  const canAccept = computed(
    () =>
      !!detail.value &&
      detail.value.state !== 'withdrawn' &&
      shown.value.length > 0 &&
      !!preview.value &&
      !reviewRequired.value &&
      confirmed.value &&
      !dirty.value &&
      !busy.value,
  )
  const pendingSelected = computed(() => {
    const d = detail.value
    return d ? selected.value.filter((o) => d.ops.find((x) => x.id === o)?.decision === 'pending') : []
  })

  // writes counts what Accept writes for the fields that are on: the fields, the files and the tag writes.
  const writes = computed(() => {
    const p = shown.value.length > 0 ? preview.value : null
    if (!p) return null
    return { fields: shown.value.length, ...previewCounts(p) }
  })

  async function accept(): Promise<boolean> {
    const d = detail.value
    const preview = selection.value?.preview
    if (!d || !preview || !canAccept.value) return false
    const seq = reviewSeq
    const body = { revision: d.head, ops: sorted(selected.value), digest: preview.digest, confirm: sorted(confirm.value) }
    const key = JSON.stringify(body)
    if (attempt?.key !== key) attempt = { key, id: newRequestId() }
    busy.value = true
    error.value = ''
    notice.value = ''
    try {
      const answer = await post<AcceptAnswer>(`/api/inbox/proposals/${d.id}/accept`, { ...body, request_id: attempt.id })
      if (seq !== reviewSeq) return true
      attempt = null
      lastAccepted.value = answer
      notice.value = 'Accepted. The library writes it in the background; the outcome shows below.'
      await loadDetail()
      return true
    } catch (err) {
      if (seq === reviewSeq) refused(err, keyOf(body.revision, body.ops))
      return false
    } finally {
      busy.value = false
    }
  }

  // Fields stay switchable while an acceptance is in flight, so a refusal speaks only for the selection that was sent.
  function refused(err: unknown, submittedKey: string) {
    if (!(err instanceof ApiError) || err.status >= 500) {
      // The server may have taken it. The same request ID makes pressing Accept again safe.
      error.value = `${errorText(err)}. It may have gone through; press Accept again to retry the same request, which runs at most once.`
      void loadDetail()
      return
    }
    attempt = null
    const body = err.body as { preview?: Preview; confirm?: Warning[]; refusal?: Refusal } | undefined
    const code = errorCode(body)
    if (code === 'preview_changed' && body?.preview) {
      if (submittedKey !== current.value) {
        void refreshPreview()
        return
      }
      ++previewSeq
      previewing.value = false
      setSelection({ preview: body.preview, refusal: null, missing: body.preview.warnings.map((w) => w.kind) }, submittedKey)
      reviewRequired.value = true
      confirm.value = []
      return
    }
    if (code === 'confirm' && submittedKey === current.value && selection.value?.preview) {
      const extra = (body?.confirm ?? []).filter((w) => !warnings.value.some((x) => x.kind === w.kind))
      const preview = { ...selection.value.preview, warnings: [...selection.value.preview.warnings, ...extra] }
      selection.value = { ...selection.value, preview, missing: (body?.confirm ?? []).map((w) => w.kind) }
      confirm.value = confirm.value.filter((kind) => !selection.value?.missing.includes(kind))
      error.value = 'Confirm each warning before you accept.'
      return
    }
    if (err.status === 422) {
      if (submittedKey === current.value) {
        ++previewSeq
        previewing.value = false
        setSelection({ preview: null, refusal: body?.refusal ?? { reason: code, error: err.message }, missing: [] }, submittedKey)
      }
      return
    }
    error.value =
      code === 'stale_revision'
        ? 'The proposal has a new revision. It shows now; check it before you accept.'
        : code === 'decided_op'
          ? 'Someone already decided one of the selected fields. The proposal shows its current state.'
          : err.message
    if (code === 'stale_revision' || code === 'decided_op' || code === 'stale_base') {
      ++previewSeq
      previewing.value = false
      selection.value = null
      selectionKey.value = ''
      reviewRequired.value = true
      void loadDetail()
    }
  }

  async function act(path: string, body: unknown, done: string): Promise<boolean> {
    const d = detail.value
    if (!d) return false
    const seq = reviewSeq
    busy.value = true
    error.value = ''
    notice.value = ''
    try {
      await post(`/api/inbox/proposals/${d.id}/${path}`, body)
      if (seq !== reviewSeq) return true
      notice.value = done
      await loadDetail()
      return true
    } catch (err) {
      if (seq !== reviewSeq) return false
      const code = errorCode(err instanceof ApiError ? err.body : undefined)
      error.value = code === 'stale_revision' ? 'The proposal has a new revision. It shows now; your text is kept.' : errorText(err)
      if (code === 'stale_revision' || code === 'decided_op') await loadDetail()
      return false
    } finally {
      busy.value = false
    }
  }

  function decline(remaining: boolean, reason: string) {
    const d = detail.value
    if (!d) return Promise.resolve(false)
    const ops = pendingSelected.value
    if (!remaining && ops.length === 0) return Promise.resolve(false)
    const body = remaining ? { revision: d.head, remaining: true, reason } : { revision: d.head, ops: sorted(ops), reason }
    return act('decline', body, 'Declined. Nothing in the library changed.')
  }

  // rest is the pending fields the reviewer left off, which Decline the rest declines.
  const rest = computed(() => {
    const d = detail.value
    if (!d || d.state === 'withdrawn') return []
    return d.ops.filter((o) => o.decision === 'pending' && d.current.ops.some((c) => c.id === o.id) && !selected.value.includes(o.id) && !held(d, o.id)).map((o) => o.id)
  })
  const declineCount = computed(() => {
    const d = detail.value
    return rest.value.length + (d && d.state !== 'withdrawn' && !shown.value.length ? d.ops.filter((o) => held(d, o.id)).length : 0)
  })

  function declineRest(reason: string) {
    const d = detail.value
    if (!d || declineCount.value === 0) return Promise.resolve(false)
    const body = shown.value.length === 0 ? { revision: d.head, remaining: true, reason } : { revision: d.head, ops: sorted(rest.value), reason }
    return act('decline', body, 'Declined. Nothing in the library changed.')
  }

  function requestChanges(comment: string) {
    const d = detail.value
    if (!d || !comment.trim()) return Promise.resolve(false)
    return act('request-changes', { revision: d.head, comment }, 'Changes requested. The agent sees it on its next run.')
  }

  function comment(body: string) {
    if (!body.trim()) return Promise.resolve(false)
    return act('comments', { body }, 'Comment added.')
  }

  // Album pages show pending values without disturbing the Inbox selection.
  const byAlbum = ref<Record<number, ProposalDetail[]>>({})
  const albumRequests = new Map<number, number>()
  async function loadAlbum(albumId: number) {
    if (!enabled.value) return
    const request = (albumRequests.get(albumId) ?? 0) + 1
    albumRequests.set(albumId, request)
    try {
      const answer = await get<unknown>(`/api/inbox/albums/${albumId}/proposals`)
      if (albumRequests.get(albumId) !== request || !enabled.value) return
      byAlbum.value = { ...byAlbum.value, [albumId]: listOf<ProposalDetail>(answer, 'proposals') }
    } catch {
      // Local suggestions still work while the Inbox cannot answer.
    }
  }

  // applyHub follows the inbox topic, so an open page shows another reviewer's or the agent's work.
  function applyHub(e: { proposal?: number }) {
    void detect()
    if (listLoaded) void loadList()
    if (e.proposal != null && e.proposal === detailId.value) void loadDetail()
    for (const a of Object.keys(byAlbum.value)) void loadAlbum(Number(a))
  }

  async function reload() {
    await detect()
    if (!enabled.value) return
    if (listLoaded) void loadList()
    if (detailId.value !== null) void loadDetail()
    for (const a of Object.keys(byAlbum.value)) void loadAlbum(Number(a))
  }

  return {
    enabled, info, detect, reload,
    filters, proposals, next, listLoading, listError, loadList, moreList, setFilters,
    detailId, detail, detailLoading, detailRefreshing, detailError, selected, drafts, selection, previewing, confirm, cascade, error, notice, busy, headSeen, lastAccepted, writes,
    open, close, loadDetail, checkFiles, refreshPreview, toggle, selectAll, switchFields, toggleConfirm,
    currentSelection, preview, reviewRequired, reviewPreview,
    values, tracksOf, everyTrack, setValues, dropTrack, setEveryTrack, discard, revisionOps, dirty, saveValues, rebase,
    warnings, confirmed, canAccept, shown, pendingSelected, accept, decline, rest, declineCount, declineRest, requestChanges, comment,
    byAlbum, loadAlbum, applyHub,
    headOp: (opId: string): Op | undefined => headOp(opId),
  }
})
