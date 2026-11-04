import type { ItemStatus, Source, Wrapper } from '../api/types'

export type Lamp = 'ok' | 'warn' | 'bad' | 'off'

// A wrapper that only waits for a person is yellow: nothing is broken yet.
function wrapperLamp(detail: unknown): Lamp {
  const state = (detail as Partial<Wrapper> | undefined)?.state
  return state === 'signed_out' || state === 'awaiting_code' || state === 'signing_in' || state === 'starting' ? 'warn' : 'bad'
}

export function sourceLamp(s: Source, switchedOff = false): Lamp {
  if (!s.enabled || switchedOff) return 'off'
  return s.health.ok ? 'ok' : wrapperLamp(s.health.detail)
}

// sourceState is the short text beside a source's rocker.
export function sourceState(s: Source, switchedOff: boolean): string {
  if (!s.enabled) return 'Not set up'
  if (switchedOff) return 'Off'
  return s.health.ok ? 'On' : s.health.message || "Doesn't work"
}

// percent is how far a download is, in whole percents.
export function percent(st?: Pick<ItemStatus, 'tracks_done' | 'tracks_total'>): number {
  if (!st?.tracks_total) return 0
  return Math.min(100, Math.round(((st.tracks_done ?? 0) / st.tracks_total) * 100))
}

export interface StateText {
  led: string
  text: string
}

// stateText is the status of a row, with its lamp's classes.
export function stateText(st?: ItemStatus): StateText | null {
  switch (st?.state) {
    case 'in_library':
      return { led: 'ok', text: 'In library' }
    case 'queued':
      return { led: '', text: 'Queued' }
    case 'running':
      return { led: 'run blink', text: `Downloading ${percent(st)}%` }
    case 'partial':
      return { led: 'warn', text: 'Done with errors' }
    case 'failed':
      return { led: 'bad', text: 'Failed' }
    default:
      return null
  }
}
