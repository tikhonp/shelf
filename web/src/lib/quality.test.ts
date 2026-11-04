import { expect, test } from 'vitest'
import { sameQuality, short, tapes, words, libraryTape } from './quality'

test.each([
  ['Hi-Res 24/192', 'master'],
  ['Hi-Res 24/96', 'hi'],
  ['CD 16/44.1', ''],
  ['MP3 320', 'lossy'],
  ['Hi-Res Lossless', 'hi'],
  ['Lossless', ''],
  ['AAC', 'lossy'],
  ['Mixed quality', ''],
])('%s gets its tape', (badge, cls) => {
  expect(tapes({ badge })).toEqual([{ text: badge, cls }])
})

test('Dolby Atmos is a second tape beside the badge', () => {
  expect(tapes({ badge: 'Lossless', atmos: true })).toEqual([
    { text: 'Lossless', cls: '' },
    { text: 'Dolby Atmos', cls: 'atmos' },
  ])
})

test('nothing known gives no tape', () => {
  expect(tapes({})).toEqual([])
  expect(tapes(undefined)).toEqual([])
})

test('the panel says the quality in words', () => {
  expect(words({ codec: 'flac', bit_depth: 24, sample_rate: 96000 })).toBe('FLAC 24-bit 96 kHz')
  expect(words({ codec: 'flac', bit_depth: 16, sample_rate: 44100 })).toBe('FLAC 16-bit 44.1 kHz')
  expect(words({ codec: 'aac', badge: 'AAC' })).toBe('AAC')
})

test('a delivered file reads short', () => {
  expect(short('flac', 24, 96000)).toBe('FLAC 24/96')
  expect(short('ec-3', 0, 0)).toBe('EC-3')
})

test('a tracklist of one quality says so', () => {
  const t = (badge: string) => ({ id: badge, position: 1, title: 't', quality: { badge } })
  expect(sameQuality([t('CD 16/44.1'), t('CD 16/44.1')])).toBe('All CD 16/44.1')
  expect(sameQuality([t('CD 16/44.1'), t('Hi-Res 24/96')])).toBe('')
})

test('a file quality gets the words and the tape of its group', () => {
  expect(libraryTape('FLAC 24/96')).toEqual({ text: 'Hi-Res 24/96', cls: 'hi' })
  expect(libraryTape('FLAC 24/192')).toEqual({ text: 'Hi-Res 24/192', cls: 'master' })
  expect(libraryTape('ALAC 24/44.1')).toEqual({ text: 'Hi-Res 24/44.1', cls: 'hi' })
  expect(libraryTape('FLAC 16/44.1')).toEqual({ text: 'CD 16/44.1', cls: '' })
  expect(libraryTape('FLAC 16/48')).toEqual({ text: 'Lossless', cls: '' })
  expect(libraryTape('MP3')).toEqual({ text: 'MP3', cls: 'lossy' })
  expect(libraryTape('OPUS')).toEqual({ text: 'OPUS', cls: 'lossy' })
  expect(libraryTape('')).toEqual({ text: '', cls: '' })
})
