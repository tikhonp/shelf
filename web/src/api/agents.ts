// The library agent's human API under /api/inbox, as docs/plans/library-agent-contracts.md fixes it.
// The agent's own API under /api/agent/v1 never runs in the browser.
import type { LibraryChange } from './types'

export type ProposalState = 'pending' | 'reviewed' | 'withdrawn'
export type Decision = 'pending' | 'accepted' | 'declined' | 'superseded'
export type OutcomeState = 'queued' | 'writing' | 'done' | 'partial' | 'failed' | 'undone' | 'expired'

export interface InboxInfo {
  pending: number
  changes_requested: number
  connections: number
  accounts: 'simple' | 'none'
  can_create_connections: boolean
}

export interface ProposalAlbum {
  id: number
  folder: string
  title: string
  artist: string
  // current_id is the album the tracks belong to now, 0 when the index lost them.
  current_id?: number
}

export interface DecisionCounts {
  pending: number
  accepted: number
  declined: number
  superseded: number
}

export interface ProposalSummary {
  id: number
  state: ProposalState
  changes_requested: boolean
  title: string
  album: ProposalAlbum
  connection: { id: number; name: string }
  kinds: string[]
  // fields names the head revision's ops without their values.
  fields?: OpName[]
  confidence?: number | null
  summary: DecisionCounts
  updated_at: string
  stale: boolean
}

export interface ProposalPage {
  proposals: ProposalSummary[]
  next: number | null
}

export interface OpTrack {
  track_id: number
  values: string[]
}

export interface TagOp {
  id: string
  type: 'tag'
  field?: string
  tag_key?: string
  tracks: OpTrack[]
  anchor?: string | null
  requires?: string[]
}

export interface CoverOp {
  id: string
  type: 'cover'
  action: 'replace' | 'remove'
  image?: string
  every_track?: boolean
  requires?: string[]
}

export type Op = TagOp | CoverOp

// OpName is an op without its values, as the proposal list sends it.
export interface OpName {
  id: string
  type: 'tag' | 'cover'
  field?: string
  tag_key?: string
  action?: 'replace' | 'remove'
}

export interface KeyChange {
  key: string
  before: string[]
  after: string[]
}

export interface PreviewFile {
  track_id: number
  path: string
  keys: KeyChange[]
  cover_before: string
  cover_after: string
}

export interface PreviewCover {
  path: string
  before: string
  after: string
}

export interface Warning {
  kind: string
  message: string
}

export interface Preview {
  album: { id: number; folder: string; title: string; artist: string }
  files: PreviewFile[]
  folder: PreviewCover[]
  warnings: Warning[]
  digest: string
}

export interface Refusal {
  error: string
  reason: string
  track_id?: number
}

export interface Evidence {
  kind: string
  text?: string
  source?: string
  field?: string
  anchor?: string
  key?: string
  [extra: string]: unknown
}

export interface RevisionInfo {
  revision: number
  author_kind: 'agent' | 'user'
  author: string
  created_at: string
}

export interface OpDecision {
  id: string
  type: 'tag' | 'cover'
  decision: Decision
  decided_rev?: number | null
  acceptance?: number | null
  decided_by?: string | null
  decided_at?: string | null
}

export interface Outcome {
  state?: OutcomeState
  reason?: string
  error?: string
  undone?: boolean
  written?: number
  files?: number
  expired?: boolean
}

export interface Acceptance {
  id: number
  revision: number
  ops: string[]
  change_id: number
  author: string
  created_at: string
  outcome: Outcome
}

export interface ProposalEvent {
  id: number
  kind: string
  actor_kind: 'user' | 'agent' | 'system'
  actor: string
  at: string
  body?: string
  data?: Record<string, unknown>
}

export interface ProposalDetail {
  id: number
  url: string
  state: ProposalState
  changes_requested: boolean
  connection: { id: number; name: string }
  run_id: number
  album: ProposalAlbum
  group: string
  follow_up_of: number | null
  head: number
  revisions: RevisionInfo[]
  current: {
    title: string
    reason: string
    uncertainties: string
    confidence: number | null
    evidence: Evidence[]
    base: { track_id: number; path?: string; revision: string }[]
    ops: Op[]
    preview: Preview
  }
  ops: OpDecision[]
  summary: DecisionCounts
  acceptances: Acceptance[]
  events: ProposalEvent[]
  // live is the inbox's own addition: the pending ops planned against the files now.
  live?: { preview: Preview | null; op_stale: Record<string, boolean>; refusal: Refusal | null }
}

export interface PreviewAnswer {
  preview: Preview | null
  refusal: Refusal | null
  missing: string[]
}

export interface AcceptBody {
  revision: number
  ops: string[]
  digest: string
  confirm: string[]
  request_id: string
}

export interface AcceptAnswer {
  acceptance: Acceptance
  change: LibraryChange
}

export interface RevisionOp {
  id: string
  tracks?: OpTrack[]
  every_track?: boolean
}

export type MemoryLayer = 'rule' | 'preference' | 'context'
export type MemoryStatus = 'active' | 'proposed' | 'deleted'

export interface MemoryEvidence {
  event?: number
  change?: number
  proposal?: number
  note?: string
}

export interface MemoryDoc {
  id: number
  key: string
  layer: MemoryLayer
  status: MemoryStatus
  title: string
  scope: string
  body: string
  confidence: number | null
  evidence: MemoryEvidence[]
  version: number
  author_kind: 'user' | 'agent'
  author: string
  corrected_event: number | null
  created_at: string
  updated_at: string
}

export interface MemoryCorrection {
  expected_version: number
  title?: string
  scope?: string
  body?: string
  confidence?: number | null
}

export interface Connection {
  id: number
  name: string
  operations: string[]
  created_by: string
  created_at: string
  last_used_at: string | null
  revoked_at: string | null
  revoke_reason: string
}

export interface Setup {
  api_base: string
  review_base: string
  operations: { name: string; summary: string }[]
  prompt: string
}

// Some answers are a bare list in one place of the contract and wrapped in another, so readers accept both.
export function listOf<T>(answer: unknown, key: string): T[] {
  if (Array.isArray(answer)) return answer as T[]
  const inner = (answer as Record<string, unknown> | null)?.[key]
  return Array.isArray(inner) ? (inner as T[]) : []
}

// errorCode is the slug an agents refusal carries beside its message.
export function errorCode(body: unknown): string {
  const code = (body as { code?: unknown } | undefined)?.code
  return typeof code === 'string' ? code : ''
}

// opTarget names what a tag op changes, as the diff and the editor label it.
export function opTarget(op: Op): string {
  if (op.type === 'cover') return op.action === 'remove' ? 'Remove cover' : 'Replace cover'
  return op.field ?? op.tag_key ?? op.id
}

// proposalOrigin reads a library change's origin, "proposal:<p>/acceptance:<a>".
export function proposalOrigin(origin: string | undefined): number | null {
  const m = origin?.match(/^proposal:(\d+)\/acceptance:\d+$/)
  return m ? Number(m[1]) : null
}

// newRequestId names one deliberate acceptance, so a retry of the same request runs at most once.
export function newRequestId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}
