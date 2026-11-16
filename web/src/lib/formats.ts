import type { Format, Kind, Quality } from '../api/types'

// menuFormats puts the source's default first and keeps the others in order.
export function menuFormats(formats: Format[]): Format[] {
  return [...formats.filter((f) => f.default), ...formats.filter((f) => !f.default)]
}

// releaseFormats drops a lossless format when a lower one already covers the
// release, since both would deliver the same files. A CD album loses Max and
// 24/96. When the default goes, the format that covers the release becomes
// the default, because the default would have delivered it. A playlist mixes
// qualities, so it keeps every format.
export function releaseFormats(formats: Format[], kind: Kind, quality: Quality | undefined): Format[] {
  const bits = quality?.bit_depth
  const rate = quality?.sample_rate
  if ((kind !== 'album' && kind !== 'track') || !bits || !rate) return formats
  const covering = formats.filter((f) => f.bit_depth && f.sample_rate && f.bit_depth >= bits && f.sample_rate >= rate)
  const least = covering.reduce<Format | undefined>((a, f) => (!a || below(f, a) ? f : a), undefined)
  if (!least) return formats
  const kept = formats.filter((f) => f === least || !covering.includes(f))
  if (kept.some((f) => f.default)) return kept
  return kept.map((f) => (f === least ? { ...f, default: true } : f))
}

function below(f: Format, o: Format): boolean {
  return f.bit_depth! !== o.bit_depth! ? f.bit_depth! < o.bit_depth! : f.sample_rate! < o.sample_rate!
}

export function defaultFormat(formats: Format[]): Format | undefined {
  return menuFormats(formats)[0]
}

// knobAngles spreads a format knob's positions from 45 to 135 degrees, which
// gives the design's 45, 75, 105, 135 for four and 45, 90, 135 for three.
export function knobAngles(n: number): number[] {
  if (n < 2) return [45]
  return Array.from({ length: n }, (_, i) => 45 + (90 * i) / (n - 1))
}

export const typeAngles = [-75, -40, 40, 75]

// topFrequency is the highest frequency a format can carry for a release: half
// its sample rate, or of the format's own limit when that is lower.
export function topFrequency(format: Format | undefined, sampleRate: number | undefined): number | undefined {
  const limit = format?.description.match(/([\d.]+) kHz/)?.[1]
  const rates = [sampleRate, limit ? Number(limit) * 1000 : undefined].filter((r): r is number => !!r)
  return rates.length ? Math.min(...rates) / 2 : undefined
}
