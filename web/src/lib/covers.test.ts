import { expect, test } from 'vitest'
import { coverUrl } from './covers'

test('Apple artwork gets the size the cell shows, times the pixel ratio', () => {
  expect(coverUrl('https://is1.mzstatic.com/a/b/600x600bb.jpg', 46, 2)).toBe('https://is1.mzstatic.com/a/b/92x92bb.jpg')
})

test('Qobuz covers use the sizes it offers', () => {
  const url = 'https://static.qobuz.com/images/covers/ab/cd/x_600.jpg'
  expect(coverUrl(url, 46, 1)).toBe('https://static.qobuz.com/images/covers/ab/cd/x_50.jpg')
  expect(coverUrl(url, 46, 2)).toBe('https://static.qobuz.com/images/covers/ab/cd/x_100.jpg')
  expect(coverUrl(url, 200, 2)).toBe(url)
  expect(coverUrl(url, 800, 2)).toBe(url)
})

test('other URLs and no URL pass through', () => {
  expect(coverUrl('data:image/svg+xml,x', 46, 2)).toBe('data:image/svg+xml,x')
  expect(coverUrl(undefined, 46, 2)).toBe('')
})
