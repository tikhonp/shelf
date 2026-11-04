import type { Item } from '../api/types'
import { count } from './format'

// rowLine is a result's line of details, which depends on its kind.
export function rowLine(it: Item, wide = false): string {
  switch (it.ref.kind) {
    case 'track':
      return [it.artist, it.album].filter(Boolean).join(' · ')
    case 'playlist':
      return [it.artist, it.track_count ? count(it.track_count, 'track') : ''].filter(Boolean).join(' · ')
    case 'artist':
      return it.track_count ? count(it.track_count, 'release') : ''
    default:
      return [it.artist, it.year ? String(it.year) : '', wide ? it.label : ''].filter(Boolean).join(' · ')
  }
}

export const itemKey = (it: Item) => `${it.ref.source}:${it.ref.kind}:${it.ref.id}`

// downloadable says whether a row gets a Download button. An artist opens instead.
export const downloadable = (it: Item) => it.ref.kind !== 'artist' && it.ref.kind !== 'label'
