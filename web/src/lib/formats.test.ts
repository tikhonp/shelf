import { expect, test } from 'vitest'
import type { Format } from '../api/types'
import { knobAngles, releaseFormats, topFrequency } from './formats'

test('knob positions follow the design', () => {
  expect(knobAngles(4)).toEqual([45, 75, 105, 135])
  expect(knobAngles(3)).toEqual([45, 90, 135])
  expect(knobAngles(1)).toEqual([45])
})

test('the top frequency is half the lower of the release and the format', () => {
  const f = (description: string) => ({ id: 'x', label: 'x', description })
  expect(topFrequency(f('FLAC up to 24-bit 192 kHz'), 96000)).toBe(48000)
  expect(topFrequency(f('FLAC 16-bit 44.1 kHz'), 96000)).toBe(22050)
  expect(topFrequency(f('MP3 at 320 kbps'), 44100)).toBe(22050)
  expect(topFrequency(f('MP3'), undefined)).toBeUndefined()
})

test('a release keeps the lowest lossless format that covers it and drops the ones above', () => {
  const formats: Format[] = [
    { id: 'max', label: 'Max', description: '', default: true, bit_depth: 24, sample_rate: 192000 },
    { id: 'hi', label: '24/96', description: '', bit_depth: 24, sample_rate: 96000 },
    { id: 'cd', label: 'CD', description: '', bit_depth: 16, sample_rate: 44100 },
    { id: 'mp3', label: 'MP3', description: '' },
  ]
  const ids = (bits?: number, rate?: number, kind: 'album' | 'track' | 'playlist' = 'album') =>
    releaseFormats(formats, kind, { bit_depth: bits, sample_rate: rate }).map((f) => `${f.id}${f.default ? '*' : ''}`)
  expect(ids(16, 44100)).toEqual(['cd*', 'mp3'])
  expect(ids(16, 44100, 'track')).toEqual(['cd*', 'mp3'])
  // 16-bit 48 kHz is past CD's rate, so 24/96 is the lowest that covers it.
  expect(ids(16, 48000)).toEqual(['hi*', 'cd', 'mp3'])
  expect(ids(24, 96000)).toEqual(['hi*', 'cd', 'mp3'])
  expect(ids(24, 192000)).toEqual(['max*', 'hi', 'cd', 'mp3'])
  expect(ids(16, 44100, 'playlist')).toEqual(['max*', 'hi', 'cd', 'mp3'])
  expect(ids()).toEqual(['max*', 'hi', 'cd', 'mp3'])
  // A default that stays keeps its place.
  const mp3Default = formats.map((f) => ({ ...f, default: f.id === 'mp3' }))
  expect(releaseFormats(mp3Default, 'album', { bit_depth: 16, sample_rate: 44100 }).map((f) => `${f.id}${f.default ? '*' : ''}`)).toEqual(['cd', 'mp3*'])
})
