import { expect, test } from 'vitest'
import { item } from '../testing/api'
import { releaseCounts } from './artist'

test('a release in two sections counts once, and the library is left out', () => {
  const a = item('q', 'a')
  const b = item('q', 'b', { status: { state: 'in_library' } })
  const c = item('q', 'c')
  const r = releaseCounts(
    [
      { id: 'albums', title: 'Albums', items: [a, b] },
      { id: 'eps', title: 'EPs and singles', items: [c, a] },
    ],
    (it) => it.status?.state === 'in_library',
  )
  expect(r).toEqual({
    sections: [
      { title: 'Albums', count: 1 },
      { title: 'EPs and singles', count: 1 },
    ],
    toDownload: 2,
    inLibrary: 1,
  })
})
