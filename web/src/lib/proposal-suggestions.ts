import type { ProposalDetail } from '../api/agents'
import type { Suggestion, Suggestions } from '../api/types'

export function proposalSuggestions(proposals: ProposalDetail[], tracks: { id: number; path: string }[]): Suggestion[] {
  return proposals.flatMap((p) => p.state === 'withdrawn' ? [] : p.current.ops.flatMap((op): Suggestion[] => {
    if (op.type !== 'tag' || !p.ops.some((d) => d.id === op.id && d.decision === 'pending')) return []
    const base = p.current.base
    if (!op.tracks.every((t) => tracks.some((current) => current.id === t.track_id && base.some((b) => b.track_id === t.track_id && b.path === current.path)))) return []
    const values = op.tracks[0]?.values ?? []
    return [{ field: op.field ?? `key:${op.tag_key}`, values: op.tracks.every((t) => JSON.stringify(t.values) === JSON.stringify(values)) ? values : undefined,
      tracks: Object.fromEntries(op.tracks.map((t) => [String(t.track_id), t.values])),
      anchor: op.anchor ?? undefined, source: `proposal:${p.id}:${op.id}`, sources: [p.connection.name], confidence: p.current.confidence ?? 0, proposal: true, unrated: p.current.confidence === null }]
  }))
}

export function withProposals(s: Suggestions | null, proposals: Suggestion[]): Suggestions | null {
  if (!proposals.length) return s
  return { anchor: null, tracks: [], covers: [], notes: [], unavailable: [], fingerprinting: false, ...s, album: [...proposals, ...(s?.album ?? [])] }
}
