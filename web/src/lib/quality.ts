import type { Quality, Track } from '../api/types'

export interface Tape {
  text: string
  // cls is the tape's color class from the design's quality table.
  cls: '' | 'hi' | 'master' | 'atmos' | 'lossy'
}

function tapeClass(badge: string): Tape['cls'] {
  if (/^Hi-Res 24\/(176\.4|192)$/.test(badge)) return 'master'
  if (badge.startsWith('Hi-Res')) return 'hi'
  if (badge.startsWith('MP3') || badge === 'AAC') return 'lossy'
  return ''
}

// tapes lists the labels a quality shows. Apple Music's Dolby Atmos is a flag
// beside the badge, so an album can carry two.
export function tapes(q?: Quality): Tape[] {
  if (!q) return []
  const out: Tape[] = []
  if (q.badge) out.push({ text: q.badge, cls: tapeClass(q.badge) })
  if (q.atmos) out.push({ text: 'Dolby Atmos', cls: 'atmos' })
  return out
}

// libraryTape is the label of a file's short quality, such as FLAC 24/96. The
// index has no bitrate, so a lossy file shows its codec.
export function libraryTape(quality: string): Tape {
  const m = /(\d+)\/([\d.]+)$/.exec(quality)
  if (!m) return { text: quality, cls: quality ? 'lossy' : '' }
  const [, bits, rate] = m
  if (Number(bits) > 16 || Number(rate) > 48) {
    const text = `Hi-Res ${bits}/${rate}`
    return { text, cls: tapeClass(text) }
  }
  return { text: bits === '16' && rate === '44.1' ? 'CD 16/44.1' : 'Lossless', cls: '' }
}

// words is a quality as the panel's Quality line says it: FLAC 24-bit 96 kHz.
export function words(q?: Quality): string {
  if (!q) return ''
  const codec = (q.codec ?? '').toUpperCase()
  if (q.bit_depth && q.sample_rate) return `${codec} ${q.bit_depth}-bit ${q.sample_rate / 1000} kHz`
  return q.badge ?? codec
}

// short is a delivered file's quality: FLAC 24/96, or the codec for a lossy one.
export function short(codec: string, bitDepth: number, sampleRate: number): string {
  const c = codec.toUpperCase()
  return bitDepth && sampleRate ? `${c} ${bitDepth}/${sampleRate / 1000}` : c
}

// sameQuality is the note beside a tracklist's heading, such as "All Hi-Res
// 24/96", or nothing when the tracks differ.
export function sameQuality(tracks: Track[]): string {
  const first = tracks[0]?.quality?.badge
  if (!first || tracks.some((t) => t.quality?.badge !== first)) return ''
  return `All ${first}`
}
