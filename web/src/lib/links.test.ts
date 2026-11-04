import { expect, test } from 'vitest'
import { linkKind } from './links'

test.each([
  ['https://play.qobuz.com/album/abc', 'qobuz'],
  ['https://open.qobuz.com/album/abc', 'qobuz'],
  ['https://www.qobuz.com/gb-en/album/x/abc', 'qobuz'],
  ['  https://qobuz.com/album/abc  ', 'qobuz'],
  ['https://music.apple.com/ru/album/x/123', 'applemusic'],
  ['https://geo.music.apple.com/album/123', 'applemusic'],
  ['https://itunes.apple.com/us/album/123', 'applemusic'],
  ['HTTPS://MUSIC.APPLE.COM/ru/album/x/123', 'applemusic'],
])('%s is a %s link', (value, kind) => {
  expect(linkKind(value)).toBe(kind)
})

test('another link goes to shelf, which knows the fake source and refuses the rest', () => {
  expect(linkKind('https://example.com/album/1')).toBe('other')
  expect(linkKind('https://fake.test/album/album-1')).toBe('other')
})

test.each(['northern static', 'qobuz.com', 'http', 'https://', 'https://a b', 'ftp://music.apple.com/x', ''])('%j is text to search', (value) => {
  expect(linkKind(value)).toBeNull()
})
