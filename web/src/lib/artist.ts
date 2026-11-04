import type { Item, Section } from '../api/types'

export interface ReleaseCounts {
  // sections lists what would download, per section that has any.
  sections: { title: string; count: number }[]
  toDownload: number
  inLibrary: number
}

// releaseCounts counts an artist's releases once each, since a release can sit
// in two sections, and leaves out the ones the library has.
export function releaseCounts(sections: Section[], inLibrary: (it: Item) => boolean): ReleaseCounts {
  const seen = new Set<string>()
  const out: ReleaseCounts = { sections: [], toDownload: 0, inLibrary: 0 }
  for (const s of sections) {
    let n = 0
    for (const it of s.items) {
      if (seen.has(it.ref.id)) continue
      seen.add(it.ref.id)
      if (inLibrary(it)) out.inLibrary++
      else n++
    }
    if (n > 0) out.sections.push({ title: s.title, count: n })
    out.toDownload += n
  }
  return out
}
