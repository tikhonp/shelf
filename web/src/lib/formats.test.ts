import { expect, test } from 'vitest'
import { knobAngles, topFrequency } from './formats'

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
