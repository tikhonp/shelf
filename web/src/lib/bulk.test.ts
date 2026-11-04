import { expect, test } from 'vitest'
import type { RunLine } from '../api/types'
import { ended, lineText, meterCells, runSub, runTitle } from './bulk'

const line = (over: Partial<RunLine>): RunLine => ({ at: '', album: 'Blue', state: 'done', fixes: 2, files: 9, message: '', ...over })

test('a run names its one fix, or counts them', () => {
  expect(runTitle(['no_releasedate'])).toBe('Run: No RELEASEDATE')
  expect(runTitle(['replaygain'])).toBe('Run: ReplayGain')
  expect(runTitle(['no_releasedate', 'replaygain'])).toBe('Run: 2 fixes')
  expect(runSub({ fixes: [], albums: 20, files: 214, skipped: 0, failed: 0 })).toBe('20 albums · 214 files')
})

test('a log line says how the album went', () => {
  expect(lineText(line({}))).toBe('Blue: 2 fixes, 9 files')
  expect(lineText(line({ fixes: 1, files: 1 }))).toBe('Blue: 1 fix, 1 file')
  expect(lineText(line({ fixes: 0 }))).toBe('Blue: 9 files')
  expect(lineText(line({ files: 0, message: 'nothing to change' }))).toBe('Blue: nothing to change')
  expect(lineText(line({ state: 'skipped', message: 'locked' }))).toBe('Blue: skipped, locked')
  expect(lineText(line({ state: 'failed', message: 'no space' }))).toBe('Blue: failed, no space')
})

test('the meter keeps one lamp per album until it runs out of room', () => {
  expect(meterCells({ total: 10, done: 6, skipped: 1, failed: 0 })).toEqual({ total: 10, done: 6, failed: 1 })
  expect(meterCells({ total: 1000, done: 500, skipped: 25, failed: 25 })).toEqual({ total: 40, done: 20, failed: 2 })
  expect(meterCells({ total: 0, done: 0, skipped: 0, failed: 0 })).toEqual({ total: 0, done: 0, failed: 0 })
})

test('a run has ended once it leaves queued and writing', () => {
  expect(ended({ state: 'writing' })).toBe(false)
  expect(ended({ state: 'partial' })).toBe(true)
})
