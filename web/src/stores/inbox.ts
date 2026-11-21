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
import type { LibraryChange } from '../api/types'
import { count } from '../lib/format'
import { opLabel, opName, previewCounts, trackLabel } from '../lib/inbox'

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

export interface DraftTarget {
  type: Op['type']
  field?: string
  tag_key?: string
  action?: CoverOp['action']
  track_ids: number[]
}

// A draft is the reviewer's values for one op that aren't a revision yet.
export interface Draft {
  target?: DraftTarget
  label?: string
  tracks: Record<number, string[]>
  dropped: number[]
  every_track?: boolean
}

export interface DraftConflict {
  opId: string
  originalLabel: string
  currentLabel: string
  reason: string
  text: string
}

export interface DroppedDraft {
  opId: string
  label: string
  reason: string
  text: string
}

export interface DeclineSnapshot {
  revision: number
  ops: string[]
  names?: string[]
}

const sorted = (xs: string[]) => [...xs].sort()
const sameList = (a: string[], b: string[]) => a.length === b.length && a.every((v, i) => v === b[i])
// An empty input is a value the reviewer hasn't typed yet; every other value is sent as typed, never split.
const typed = (values: string[]) => values.filter((v) => v.trim() !== '')

function opTargetSnapshot(op: Op): DraftTarget {
  if (op.type === 'cover') return { type: 'cover', action: op.action, track_ids: [] }
  return {
    type: 'tag',
    field: op.field,
    tag_key: op.tag_key,
    track_ids: op.tracks.map((t) => t.track_id).sort((a, b) => a - b),
  }
}

function sameDraftTarget(a: DraftTarget, b: DraftTarget): boolean {
  if (a.type !== b.type) return false
  if (a.type === 'cover') return a.action === b.action
  if ((a.field ?? '') !== (b.field ?? '') || (a.tag_key ?? '') !== (b.tag_key ?? '')) return false
  return a.track_ids.length === b.track_ids.length && a.track_ids.every((id, i) => id === b.track_ids[i])
}

function isStaleDetail(next: ProposalDetail, cur: ProposalDetail | null): boolean {
  if (!cur || cur.id !== next.id) return false
  if (next.head < cur.head) return true
  if (next.head > cur.head) return false
  if (next.events.length < cur.events.length) return true
  if (next.acceptances.length < cur.acceptances.length) return true
  if (cur.state === 'withdrawn' && next.state !== 'withdrawn') return true
  return false
}

function sameTarget(a: DraftTarget, op: Op): boolean {
  if (a.type !== op.type) return false
  if (op.type === 'cover') return a.action === op.action
  if ((a.field ?? '') !== (op.field ?? '') || (a.tag_key ?? '') !== (op.tag_key ?? '')) return false
  const ids = op.tracks.map((t) => t.track_id).sort((x, y) => x - y)
  return a.track_ids.length === ids.length && a.track_ids.every((id, i) => id === ids[i])
}

function formatDraftText(draft: Draft, d: ProposalDetail | null): string {
  if (draft.target?.type === 'cover') {
    return draft.every_track ? 'Embed in every track' : 'Folder and tracks with a cover'
  }
  const entries = Object.entries(draft.tracks)
    .map(([tid, vals]) => [Number(tid), typed(vals)] as const)
    .sort(([a], [b]) => a - b)
  const parts: string[] = []
  if (entries.length > 0) {
    const first = entries[0]![1]
    if (entries.every(([, v]) => sameList(v, first))) {
      parts.push(first.length ? first.join(' / ') : 'Removed')
    } else {
      parts.push(
        entries
          .map(([tid, vals]) => `${d ? trackLabel(d, tid) : `Track ${tid}`}: ${vals.length ? vals.join(' / ') : 'Removed'}`)
          .join('; '),
      )
    }
  }
  if (draft.dropped.length > 0) parts.push(`${count(draft.dropped.length, 'track')} left out`)
  return parts.join(' · ')
}

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
  const detecting = ref(false)
  const detectError = ref('')

  async function detect() {
    detecting.value = true
    try {
      info.value = await get<InboxInfo>('/api/inbox')
      enabled.value = true
      detectError.value = ''
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        enabled.value = false
        info.value = null
        detectError.value = ''
      } else {
        detectError.value = errorText(err)
      }
    } finally {
      detecting.value = false
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
  let loadedPages = 0
  let failedListOp: 'load' | 'more' | 'refresh' | null = null

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
    failedListOp = null
    listLoading.value = true
    listError.value = ''
    try {
      const page = await get<ProposalPage>(listPath())
      if (seq !== listSeq) return
      proposals.value = page.proposals
      next.value = page.next ?? null
      listLoaded = true
      loadedPages = 1
    } catch (err) {
      if (seq === listSeq) {
        failedListOp = 'load'
        listError.value = errorText(err)
      }
    } finally {
      if (seq === listSeq) listLoading.value = false
    }
  }

  async function moreList() {
    if (next.value === null || listLoading.value || failedListOp === 'refresh') return
    const seq = listSeq
    failedListOp = null
    listLoading.value = true
    listError.value = ''
    try {
      const page = await get<ProposalPage>(listPath(next.value))
      if (seq !== listSeq) return
      const seen = new Set(proposals.value.map((p) => p.id))
      proposals.value = [...proposals.value, ...page.proposals.filter((p) => !seen.has(p.id))]
      next.value = page.next ?? null
      loadedPages = Math.max(loadedPages, 1) + 1
    } catch (err) {
      if (seq === listSeq) {
        failedListOp = 'more'
        listError.value = errorText(err)
      }
    } finally {
      if (seq === listSeq) listLoading.value = false
    }
  }

  // refreshList reloads the currently paginated pages in place so SSE refreshes keep pagination and scroll position.
  async function refreshList() {
    if (!listLoaded) return
    const seq = ++listSeq
    const pages = Math.max(1, loadedPages)
    failedListOp = null
    listLoading.value = true
    try {
      const items: ProposalSummary[] = []
      const seen = new Set<number>()
      let cursor: number | null = null
      let fetched = 0
      while (fetched < pages) {
        const page: ProposalPage = await get<ProposalPage>(listPath(cursor ?? undefined))
        if (seq !== listSeq) return
        for (const p of page.proposals) {
          if (!seen.has(p.id)) {
            seen.add(p.id)
            items.push(p)
          }
        }
        cursor = page.next ?? null
        fetched++
        if (cursor === null) break
      }
      if (seq !== listSeq) return
      proposals.value = items
      next.value = cursor
      loadedPages = fetched
      listError.value = ''
    } catch (err) {
      if (seq === listSeq) {
        failedListOp = 'refresh'
        listError.value = errorText(err)
      }
    } finally {
      if (seq === listSeq) listLoading.value = false
    }
  }

  function retryList() {
    if (failedListOp === 'refresh') return refreshList()
    if (failedListOp === 'more' && next.value !== null) return moreList()
    return loadList()
  }

  function setFilters(f: Filters) {
    const same = f.state === filters.state && f.album === filters.album && f.kind === filters.kind && f.connection === filters.connection
    Object.assign(filters, f)
    if (!same) {
      proposals.value = []
      next.value = null
      listError.value = ''
      listLoaded = false
      loadedPages = 0
      failedListOp = null
    }
    if (!same || !listLoaded) void loadList()
  }

  // The proposal under review.
  const detailId = ref<number | null>(null)
  const detail = ref<ProposalDetail | null>(null)
  const detailLoading = ref(false)
  const detailRefreshing = ref(false)
  const detailError = ref('')
  const detailMissing = ref(false)
  const selected = ref<string[]>([])
  const drafts = ref<Record<string, Draft>>({})
  const droppedDrafts = ref<DroppedDraft[]>([])
  const replyDraft = ref('')
  const replyAsking = ref(false)
  const declining = ref(false)
  const declineSnap = ref<DeclineSnapshot | null>(null)
  const declineReason = ref('')
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
  // reviewedDigests tracks the last reviewed preview digest per op selection across revisions.
  let reviewedDigests = new Map<string, string>()
  let lastDigest: string | null = null
  let revisionChanged = false
  // attempt keeps the request ID of an acceptance whose answer never came, so pressing Accept again retries it.
  let attempt: { key: string; id: string } | null = null

  function reset() {
    ++reviewSeq
    ++previewSeq
    recoveringKey = ''
    reviewedDigests = new Map()
    lastDigest = null
    revisionChanged = false
    previewing.value = false
    detailRefreshing.value = false
    detail.value = null
    selected.value = []
    drafts.value = {}
    droppedDrafts.value = []
    replyDraft.value = ''
    replyAsking.value = false
    declining.value = false
    declineSnap.value = null
    declineReason.value = ''
    selection.value = null
    selectionKey.value = ''
    reviewRequired.value = false
    lastAccepted.value = null
    confirm.value = []
    cascade.value = null
    error.value = ''
    notice.value = ''
    detailError.value = ''
    detailMissing.value = false
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
      if (!isStaleDetail(d, detail.value)) take(d)
      detailError.value = ''
      detailMissing.value = false
    } catch (err) {
      // A failed reload keeps what the page shows and the reviewer's values.
      if (seq === detailSeq) {
        detailMissing.value = err instanceof ApiError && err.status === 404
        detailError.value = detailMissing.value ? 'This proposal is no longer available.' : errorText(err)
      }
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
  function take(d: ProposalDetail, fromSave = false) {
    const prev = detail.value
    const first = prev === null || prev.id !== d.id
    detail.value = d
    if (!first && d.head !== prev.head) {
      if (fromSave) {
        reviewedDigests = new Map()
        lastDigest = null
        revisionChanged = false
      } else {
        revisionChanged = true
      }
    }
    const open =
      d.state === 'withdrawn'
        ? new Set<string>()
        : new Set(d.ops.filter((o) => o.decision === 'pending' && d.current.ops.some((c) => c.id === o.id)).map((o) => o.id))
    const kept: Record<string, Draft> = {}
    const newlyDropped: DroppedDraft[] = []
    for (const [k, draft] of Object.entries(drafts.value)) {
      if (open.has(k)) {
        kept[k] = draft
        continue
      }
      const decision = d.ops.find((o) => o.id === k)?.decision
      const reason = d.state === 'withdrawn' ? 'withdrawn' : decision && decision !== 'pending' ? decision : 'removed'
      const label = draft.label ?? (prev ? opName(prev.current.ops, k) : opName(d.current.ops, k))
      newlyDropped.push({ opId: k, label, reason, text: formatDraftText(draft, prev ?? d) })
    }
    drafts.value = kept
    if (newlyDropped.length > 0) {
      const replaced = new Set(newlyDropped.map((x) => x.opId))
      droppedDrafts.value = [...droppedDrafts.value.filter((x) => !replaced.has(x.opId)), ...newlyDropped]
    }
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
      if (d.head !== headSeen.value && Object.keys(drafts.value).length > 0 && draftConflicts.value.length === 0) {
        notice.value = `Revision ${d.head} arrived while you edited. Your values are kept; check them against it before you save.`
      }
      if (Object.keys(drafts.value).length === 0) headSeen.value = d.head
    }
    void refreshPreview(true)
  }

  function dismissDroppedDraft(opId?: string) {
    droppedDrafts.value = opId === undefined ? [] : droppedDrafts.value.filter((x) => x.opId !== opId)
  }

  const keyOf = (revision: number, ops: string[]) => JSON.stringify([revision, sorted(ops)])
  const opsKeyOf = (ops: string[]) => JSON.stringify(sorted(ops))

  async function refreshPreview(force = false) {
    const d = detail.value
    if (!d || selected.value.every((o) => held(d, o))) {
      ++previewSeq
      if (!d || !d.ops.some((o) => selectable(d, o.id) && !held(d, o.id))) reviewRequired.value = false
      selection.value = null
      selectionKey.value = ''
      previewing.value = false
      return
    }
    const ops = sorted(selected.value)
    const key = keyOf(d.head, ops)
    const opsKey = opsKeyOf(ops)
    if (!force && !previewing.value && key === selectionKey.value && selection.value) return
    const seq = ++previewSeq
    previewing.value = true
    try {
      const answer = await post<PreviewAnswer>(`/api/inbox/proposals/${d.id}/preview`, { revision: d.head, ops })
      if (seq !== previewSeq) return
      recoveringKey = ''
      setSelection(answer, key, opsKey)
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
        setSelection({ preview: null, refusal, missing: [] }, key, opsKey)
        return
      }
      selection.value = null
      selectionKey.value = ''
      error.value = errorText(err)
    } finally {
      if (seq === previewSeq) previewing.value = false
    }
  }

  function setSelection(answer: PreviewAnswer, key: string, opsKey: string) {
    const previous = selection.value?.preview?.warnings ?? []
    const digest = answer.refusal ? undefined : answer.preview?.digest
    const reviewed = reviewedDigests.get(opsKey)
    const expected = reviewed ?? (revisionChanged ? lastDigest : undefined)
    const changed = digest !== undefined && expected != null && expected !== digest
    if (changed) reviewRequired.value = true
    if (digest !== undefined) {
      if (reviewed === undefined && !changed) reviewedDigests.set(opsKey, digest)
      lastDigest = digest
      revisionChanged = false
    }
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

  function isConflicted(opId: string): boolean {
    const draft = drafts.value[opId]
    if (!draft?.target) return false
    const op = headOp(opId)
    return !op || !sameTarget(draft.target, op)
  }

  const draftConflicts = computed<DraftConflict[]>(() => {
    const d = detail.value
    if (!d) return []
    const out: DraftConflict[] = []
    for (const [opId, draft] of Object.entries(drafts.value)) {
      if (!draft.target) continue
      const op = headOp(opId)
      if (op && sameTarget(draft.target, op)) continue
      const originalLabel = draft.label ?? opId
      const currentLabel = op ? opLabel(op) : 'removed'
      const reason = !op
        ? 'removed from this revision'
        : draft.target.type !== op.type ||
            (op.type === 'cover' ? draft.target.action !== op.action : (draft.target.field ?? '') !== (op.field ?? '') || (draft.target.tag_key ?? '') !== (op.tag_key ?? ''))
          ? `changed from ${originalLabel} to ${currentLabel}`
          : 'target tracks changed'
      out.push({ opId, originalLabel, currentLabel, reason, text: formatDraftText(draft, d) })
    }
    return out
  })

  function values(op: TagOp, trackId: number): string[] {
    if (isConflicted(op.id)) return op.tracks.find((t) => t.track_id === trackId)?.values ?? []
    return drafts.value[op.id]?.tracks[trackId] ?? op.tracks.find((t) => t.track_id === trackId)?.values ?? []
  }

  function tracksOf(op: TagOp) {
    if (isConflicted(op.id)) return op.tracks
    const dropped = drafts.value[op.id]?.dropped ?? []
    return op.tracks.filter((t) => !dropped.includes(t.track_id))
  }

  function everyTrack(op: CoverOp): boolean {
    if (isConflicted(op.id)) return !!op.every_track
    return drafts.value[op.id]?.every_track ?? !!op.every_track
  }

  function draftFor(opId: string): Draft {
    const existing = drafts.value[opId]
    if (existing) return existing
    const op = headOp(opId)
    return { target: op ? opTargetSnapshot(op) : undefined, label: op ? opLabel(op) : opId, tracks: {}, dropped: [] }
  }

  function setValues(opId: string, trackIds: number[], next: string[]) {
    if (isConflicted(opId)) return
    const d = draftFor(opId)
    const tracks = { ...d.tracks }
    for (const t of trackIds) tracks[t] = [...next]
    drafts.value = { ...drafts.value, [opId]: { ...d, tracks } }
    prune(opId)
  }

  function dropTrack(opId: string, trackId: number) {
    if (isConflicted(opId)) return
    const d = draftFor(opId)
    drafts.value = { ...drafts.value, [opId]: { ...d, dropped: [...new Set([...d.dropped, trackId])] } }
  }

  function setEveryTrack(opId: string, on: boolean) {
    if (isConflicted(opId)) return
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
      if (!op || isConflicted(opId)) continue
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
    if (!d || !op || isConflicted(opId)) return
    const unchanged =
      op.type === 'cover'
        ? d.every_track === undefined || d.every_track === !!op.every_track
        : d.dropped.length === 0 && Object.entries(d.tracks).every(([t, v]) => sameList(v, op.tracks.find((x) => x.track_id === Number(t))?.values ?? []))
    // Returning to the old head is still an edit against the revision being saved.
    if (unchanged && savingReview !== reviewSeq) discard(opId)
  }

  const dirty = computed(() => revisionOps().length > 0 || draftConflicts.value.length > 0)
  const hasValueDrafts = computed(() => Object.keys(drafts.value).length > 0)
  const hasReplyDraft = computed(() => replyDraft.value.trim().length > 0)
  const hasDeclineDraft = computed(() => declineReason.value.trim().length > 0)
  const hasUnsaved = computed(() => hasValueDrafts.value || hasReplyDraft.value || hasDeclineDraft.value)

  async function saveValues(): Promise<boolean> {
    const d = detail.value
    const ops = revisionOps()
    if (!d || ops.length === 0 || draftConflicts.value.length > 0) return false
    const seq = reviewSeq
    savingReview = seq
    const parentRevision = headSeen.value || d.head
    const savedRevision = parentRevision + 1
    const submittedById = new Map(ops.map((op) => [op.id, op]))
    const submittedTargets = new Map(ops.map((op) => [op.id, drafts.value[op.id]?.target]))
    const sent = Object.fromEntries(ops.map((op) => [op.id, JSON.stringify(drafts.value[op.id])]))
    busy.value = true
    error.value = ''
    notice.value = ''
    try {
      const answer = await post<ProposalDetail>(`/api/inbox/proposals/${d.id}/revisions`, { parent_revision: parentRevision, ops })
      if (seq !== reviewSeq) return true
      const remaining: Record<string, Draft> = {}
      for (const [id, draft] of Object.entries(drafts.value)) {
        if (sent[id] === JSON.stringify(draft)) continue
        const submitted = submittedById.get(id)
        const submittedTarget = submittedTargets.get(id)
        if (submitted?.tracks && draft.target?.type === 'tag' && submittedTarget && sameDraftTarget(draft.target, submittedTarget)) {
          const savedTrackIds = submitted.tracks.map((t) => t.track_id).sort((a, b) => a - b)
          const validIds = new Set(savedTrackIds)
          remaining[id] = {
            ...draft,
            target: { ...draft.target, track_ids: savedTrackIds },
            dropped: draft.dropped.filter((tid) => validIds.has(tid)),
          }
        } else {
          remaining[id] = draft
        }
      }
      drafts.value = remaining
      headSeen.value = savedRevision
      notice.value = `Saved as revision ${savedRevision}. Nothing in the library changed.`
      if (!isStaleDetail(answer, detail.value)) {
        ++detailSeq
        take(answer, answer.head === savedRevision)
      }
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

  // After a stale save, the reviewer saves on top of the head they now see, only when every draft still targets the same op.
  function rebase(): boolean {
    if (!detail.value || draftConflicts.value.length > 0) return false
    headSeen.value = detail.value.head
    notice.value = ''
    return true
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
    if (!dirty.value && preview.value?.digest === digest) {
      reviewedDigests.set(opsKeyOf(selected.value), digest)
      lastDigest = digest
      reviewRequired.value = false
    }
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
      setSelection({ preview: body.preview, refusal: null, missing: body.preview.warnings.map((w) => w.kind) }, submittedKey, opsKeyOf(selected.value))
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
        setSelection({ preview: null, refusal: body?.refusal ?? { reason: code, error: err.message }, missing: [] }, submittedKey, opsKeyOf(selected.value))
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
  const declineTargets = computed(() => {
    const d = detail.value
    if (!d || d.state === 'withdrawn') return []
    const ids = [...rest.value]
    if (shown.value.length === 0) {
      for (const o of d.ops) {
        if (held(d, o.id) && d.current.ops.some((c) => c.id === o.id) && !ids.includes(o.id)) ids.push(o.id)
      }
    }
    return sorted(ids)
  })
  const declineCount = computed(() => declineTargets.value.length)

  function declineRest(reason: string, snapshot?: DeclineSnapshot) {
    const d = detail.value
    if (!d) return Promise.resolve(false)
    if (snapshot) {
      if (snapshot.ops.length === 0) return Promise.resolve(false)
      return act('decline', { revision: snapshot.revision, ops: sorted(snapshot.ops), reason }, 'Declined. Nothing in the library changed.')
    }
    if (declineCount.value === 0) return Promise.resolve(false)
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
    if (listLoaded) void refreshList()
    if (e.proposal != null && e.proposal === detailId.value) void loadDetail()
    for (const a of Object.keys(byAlbum.value)) void loadAlbum(Number(a))
  }

  // applyLibraryChange refreshes an open proposal when a completed Library change touches its album.
  function applyLibraryChange(c: LibraryChange) {
    if (c.state !== 'done' && c.state !== 'partial') return
    const d = detail.value
    if (!d || detailId.value === null) return
    const sameAlbum =
      (c.album_id > 0 && (c.album_id === d.album.current_id || c.album_id === d.album.id)) ||
      (!!c.folder && c.folder === d.album.folder)
    if (sameAlbum || c.run) void loadDetail()
  }

  async function reload() {
    await detect()
    if (!enabled.value) return
    if (listLoaded) void refreshList()
    if (detailId.value !== null) void loadDetail()
    for (const a of Object.keys(byAlbum.value)) void loadAlbum(Number(a))
  }

  return {
    enabled, info, detecting, detectError, detect, reload,
    filters, proposals, next, listLoading, listError, loadList, moreList, refreshList, retryList, setFilters,
    detailId, detail, detailLoading, detailRefreshing, detailError, detailMissing, selected, drafts, draftConflicts, droppedDrafts, dismissDroppedDraft, replyDraft, replyAsking, declining, declineSnap, declineReason, hasValueDrafts, hasReplyDraft, hasDeclineDraft, hasUnsaved, selection, previewing, confirm, cascade, error, notice, busy, headSeen, lastAccepted, writes,
    open, close, loadDetail, checkFiles, refreshPreview, toggle, selectAll, switchFields, toggleConfirm,
    currentSelection, preview, reviewRequired, reviewPreview,
    values, tracksOf, everyTrack, setValues, dropTrack, setEveryTrack, discard, revisionOps, dirty, saveValues, rebase,
    warnings, confirmed, canAccept, shown, pendingSelected, accept, decline, rest, declineTargets, declineCount, declineRest, requestChanges, comment,
    byAlbum, loadAlbum, applyHub, applyLibraryChange,
    headOp: (opId: string): Op | undefined => headOp(opId),
  }
})
