import type { Format } from '../api/types'

// menuFormats puts the source's default first and keeps the others in order.
export function menuFormats(formats: Format[]): Format[] {
  return [...formats.filter((f) => f.default), ...formats.filter((f) => !f.default)]
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
