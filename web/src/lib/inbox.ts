import type { Acceptance, Decision, MemoryEvidence, Op, OpDecision, Preview, ProposalDetail, ProposalEvent, ProposalState, ProposalSummary } from '../api/agents'
import { fieldLabel } from './tags'

const base = (path: string) => path.split('/').pop() || path

// trackLabel names a track by its file, from whichever record still has the path.
export function trackLabel(d: ProposalDetail, trackId: number): string {
  const previews = [d.live?.preview, d.current.preview].filter((p): p is Preview => !!p)
  for (const p of previews) {
    const f = p.files.find((x) => x.track_id === trackId)
    if (f) return base(f.path)
  }
  const b = d.current.base.find((x) => x.track_id === trackId)
  return b?.path ? base(b.path) : `Track ${trackId}`
}

// opLabel is the field an op changes, in the editor's words where it has them.
export function opLabel(op: Op): string {
  if (op.type === 'cover') return op.action === 'remove' ? 'Remove cover' : 'Replace cover'
  if (op.field) return fieldLabel(op.field)
  return op.tag_key ?? op.id
}

export function opName(ops: Op[], id: string): string {
  const op = ops.find((item) => item.id === id)
  return op ? opLabel(op) : fieldLabel(id)
}

// wasLine says what the tracks hold now, under a proposed value. Nothing shows when Shelf couldn't read them.
export function wasLine(before: (string[] | undefined)[]): string {
  const known = before.filter((v): v is string[] => !!v)
  if (known.length === 0) return ''
  const first = known[0]!
  if (known.some((v) => v.length !== first.length || v.some((x, i) => x !== first[i]))) return 'Was different on each track'
  return first.length ? `Was ${first.join(' / ')}` : 'Was empty'
}

export const isLong = (op: Op) => op.type === 'tag' && /lyrics/i.test(op.field ?? op.tag_key ?? '')

export function stateWord(p: Pick<ProposalSummary, 'state' | 'changes_requested'>): string {
  if (p.state === 'withdrawn') return 'Withdrawn'
  if (p.state === 'reviewed') return 'Reviewed'
  return p.changes_requested ? 'Changes requested' : 'Pending'
}

export function stateClass(s: ProposalState, changesRequested = false): string {
  if (s === 'withdrawn') return 'canceled'
  if (s === 'reviewed') return 'done'
  return changesRequested ? 'partial' : 'queued'
}

const decisions: Record<Decision, string> = { pending: 'Pending', accepted: 'Accepted', declined: 'Declined', superseded: 'Superseded' }
export const decisionWord = (d: Decision) => decisions[d]
export const decisionClass = (d: Decision) => (d === 'accepted' ? 'done' : d === 'declined' ? 'failed' : d === 'superseded' ? 'skipped' : 'queued')

// summaryText counts a proposal's fields by decision: "2 pending · 1 accepted".
export function summaryText(s: ProposalSummary['summary']): string {
  return (['pending', 'accepted', 'declined', 'superseded'] as const)
    .filter((k) => s[k] > 0)
    .map((k) => `${s[k]} ${k}`)
    .join(' · ')
}

// outcome says what the library did with an acceptance. Accepted means approved, not written.
export function outcomeWord(a: Acceptance): string {
  const o = a.outcome
  if (o.expired) return 'Left the history'
  if (o.undone) return 'Undone'
  switch (o.state) {
    case 'done': return 'Written'
    case 'partial': return 'Partly written'
    case 'failed': return 'Failed'
    case 'writing': return 'Writing'
    case 'undone': return 'Undone'
    case 'expired': return 'Left the history'
    default: return 'Queued'
  }
}

export function outcomeClass(a: Acceptance): string {
  const o = a.outcome
  if (o.expired || o.undone || o.state === 'undone' || o.state === 'expired') return 'canceled'
  if (o.state === 'done') return 'done'
  if (o.state === 'partial') return 'partial'
  if (o.state === 'failed') return 'failed'
  if (o.state === 'writing') return 'run'
  return 'queued'
}

const eventWords: Record<string, string> = {
  proposal_created: 'proposed',
  proposal_revised: 'revised the proposal',
  proposal_edited: 'edited values',
  proposal_withdrawn: 'withdrew the proposal',
  comment: 'commented',
  changes_requested: 'requested changes',
  accepted: 'accepted',
  declined: 'declined',
  outcome: 'reported the outcome',
}

// eventLine is one entry of the discussion, without its body.
export function eventLine(e: ProposalEvent, ops: Op[] = []): string {
  const actor = e.actor_kind === 'system' ? 'Shelf' : e.actor || (e.actor_kind === 'agent' ? 'Agent' : 'Unknown')
  const what = eventWords[e.kind] ?? e.kind.replace(/_/g, ' ')
  const data = e.data ?? {}
  const names = Array.isArray(data.ops) ? ` ${(data.ops as string[]).map((id) => opName(ops, id)).join(', ')}` : ''
  if (e.kind === 'proposal_revised' || e.kind === 'proposal_edited') return `${actor} ${what}: revision ${String(data.from ?? '?')} to ${String(data.to ?? '?')}`
  if (e.kind === 'outcome') return `${actor}: change ${String(data.change_id ?? '')} ${String(data.state ?? '')}${data.undone ? ', undone' : ''}`
  return `${actor} ${what}${names}`
}

export function decidedLine(op: OpDecision): string {
  if (op.decision === 'pending' || !op.decided_by) return ''
  return `${decisionWord(op.decision)} by ${op.decided_by}${op.decided_rev ? ` at revision ${op.decided_rev}` : ''}`
}

// evidenceText names one memory evidence item; a proposal gets a link beside it.
export function evidenceText(e: MemoryEvidence): string {
  if (e.proposal != null) return `Proposal ${e.proposal}`
  if (e.event != null) return `Event ${e.event}`
  if (e.change != null) return `Library change ${e.change}`
  if (e.note) return e.note
  return JSON.stringify(e)
}

// coverSrc shows a cover hash from the library's cover store; "" is no cover.
export const coverSrc = (hash: string) => (hash ? `/api/library/covers/${encodeURIComponent(hash)}` : '')
