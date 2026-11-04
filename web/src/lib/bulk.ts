import type { RunLine, RunProgress, RunSummary } from '../api/types'
import { count } from './format'
import { issueName } from './tags'

// The fixes a run offers, in the order the bank shows them.
export const bulkFixes = ['no_releasedate', 'values_differ', 'tags_disagree', 'various_artists', 'replaygain']

export const fixName = (fix: string) => (fix === 'replaygain' ? 'ReplayGain' : issueName(fix))

const fixCount = (n: number) => `${n} ${n === 1 ? 'fix' : 'fixes'}`

export function runTitle(fixes: string[]): string {
  return fixes.length === 1 ? `Run: ${fixName(fixes[0]!)}` : `Run: ${fixCount(fixes.length)}`
}

export const runSub = (r: RunSummary) => `${count(r.albums, 'album')} · ${count(r.files, 'file')}`

// lineText is a line of the run's log.
export function lineText(l: RunLine): string {
  if (l.state === 'skipped') return `${l.album}: skipped, ${l.message}`
  if (l.state === 'failed') return `${l.album}: failed, ${l.message}`
  if (l.files === 0) return `${l.album}: ${l.message || 'nothing to change'}`
  return l.fixes > 0 ? `${l.album}: ${fixCount(l.fixes)}, ${count(l.files, 'file')}` : `${l.album}: ${count(l.files, 'file')}`
}

// backupWords says what the nightly backups do with a run's files.
export function backupWords(size: string): string {
  return `The nightly HDD backup copies these ${size} again. The remote backup keeps the overwritten files for 30 days, so it grows by up to ${size}.`
}

const maxCells = 40

// meterCells fits a run into the meter. Each lamp is one album until the run
// outgrows the row, and then lamps share albums.
export function meterCells(r: Pick<RunProgress, 'total' | 'done' | 'skipped' | 'failed'>) {
  const total = Math.min(r.total, maxCells)
  const scale = r.total > 0 ? total / r.total : 0
  return { total, done: Math.floor(r.done * scale), failed: Math.floor((r.skipped + r.failed) * scale) }
}

export const ended = (r: Pick<RunProgress, 'state'>) => r.state !== 'queued' && r.state !== 'writing'
