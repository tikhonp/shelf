import { expect, test } from 'vitest'
import { sourceWords } from './source'

test('each way of knowing a source has its words', () => {
  const at = new Date(2026, 10, 3, 14, 5).toISOString()
  expect(sourceWords({ id: 'qobuz', name: 'Qobuz', by: 'download', at })).toBe('Downloaded from Qobuz on 3 November 2026')
  expect(sourceWords({ id: 'qobuz', name: 'Qobuz', by: 'download' })).toBe('Downloaded from Qobuz')
  expect(sourceWords({ id: 'qobuz', name: 'Qobuz', by: 'import' })).toBe("From Qobuz, in the old bot's history")
  expect(sourceWords({ id: 'applemusic', name: 'Apple Music', by: 'file', at })).toBe('Downloaded from Apple Music on 3 November 2026, says its shelf.txt')
  expect(sourceWords({ id: 'applemusic', name: 'Apple Music', by: 'folder' })).toBe('From Apple Music, going by its folder')
  expect(sourceWords({ id: '', name: '' })).toBe('No known source')
})
