import { expect, test } from 'vitest'
import { ago, clock, count, duration, elapsed, finishedAt, longDate, originWord, releaseDate, resultWord, size, took } from './format'

test('durations', () => {
  expect(duration(252)).toBe('4:12')
  expect(duration(2892)).toBe('48:12')
  expect(duration(3723)).toBe('1:02:03')
  expect(duration(0)).toBe('')
  expect(duration(undefined)).toBe('')
})

test('running time on a display', () => {
  expect(elapsed(161)).toBe('02:41')
  expect(elapsed(3723)).toBe('01:02:03')
  expect(elapsed(-3)).toBe('00:00')
})

test('how long a download took', () => {
  const at = (s: number) => new Date(Date.UTC(2026, 10, 4, 10, 0, s)).toISOString()
  expect(took(at(0), at(2))).toBe('2s')
  expect(took(at(0), at(108))).toBe('1m 48s')
  expect(took(at(0), at(362))).toBe('6m 02s')
  expect(took(at(0), at(3720))).toBe('1h 02m')
  expect(took(null, at(2))).toBe('—')
})

test('sizes', () => {
  expect(size(612e6)).toBe('612 MB')
  expect(size(1.9e9)).toBe('1.9 GB')
  expect(size(4200)).toBe('4 KB')
  expect(size(0)).toBe('—')
})

test('"3 minutes ago"', () => {
  const now = Date.UTC(2026, 10, 4, 12, 0, 0)
  const before = (s: number) => new Date(now - s * 1000).toISOString()
  expect(ago(before(20), now)).toBe('just now')
  expect(ago(before(60), now)).toBe('1 minute ago')
  expect(ago(before(180), now)).toBe('3 minutes ago')
  expect(ago(before(7200), now)).toBe('2 hours ago')
  expect(ago(before(86400 * 3), now)).toBe('3 days ago')
})

test('the history shows the clock today and the date before', () => {
  const now = new Date(2026, 10, 4, 15, 0).getTime()
  expect(finishedAt(new Date(2026, 10, 4, 14, 32).toISOString(), now)).toBe(clock(new Date(2026, 10, 4, 14, 32)))
  expect(finishedAt(new Date(2026, 10, 2, 14, 32).toISOString(), now)).toBe('2 Nov')
  expect(finishedAt(null, now)).toBe('—')
})

test('a long date names the weekday and the local time', () => {
  expect(longDate(new Date(2026, 10, 11, 12, 31).toISOString())).toBe('Wednesday 11 November 2026, 12:31')
  expect(longDate(new Date(2026, 0, 4, 9, 5).toISOString())).toBe('Sunday 4 January 2026, 09:05')
  expect(longDate('not a time')).toBe('')
})

test('a release date stays on its day', () => {
  expect(releaseDate('2019-03-15')).toBe('15 March 2019')
  expect(releaseDate('2019')).toBe('2019')
})

test('results and who asked', () => {
  expect(resultWord('partial')).toBe('Done with errors')
  expect(originWord('web')).toBe('Web')
  expect(originWord('telegram', 'tikhon')).toBe('@tikhon')
  expect(originWord('telegram')).toBe('Telegram')
  expect(count(1, 'track')).toBe('1 track')
  expect(count(12, 'track')).toBe('12 tracks')
})
