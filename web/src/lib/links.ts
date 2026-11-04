const qobuzHosts = new Set(['qobuz.com', 'www.qobuz.com', 'play.qobuz.com', 'open.qobuz.com'])
const appleHosts = new Set(['music.apple.com', 'geo.music.apple.com', 'beta.music.apple.com', 'classical.music.apple.com', 'itunes.apple.com'])

export type LinkKind = 'qobuz' | 'applemusic' | 'other'

// linkKind says what a search field's value is: a link of a known source, some
// other link, or null for text to search. shelf decides what a link means, so
// any link goes to /api/resolve, and its refusal is what the page shows.
export function linkKind(value: string): LinkKind | null {
  const text = value.trim()
  if (!/^https?:\/\/\S+$/i.test(text)) return null
  let host: string
  try {
    host = new URL(text).hostname.toLowerCase()
  } catch {
    return null
  }
  if (qobuzHosts.has(host)) return 'qobuz'
  if (appleHosts.has(host)) return 'applemusic'
  return 'other'
}
