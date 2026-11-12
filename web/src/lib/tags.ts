import type { LibraryIssue, LibraryTrack, Suggestion, Suggestions, TagKey } from '../api/types'

// A target names what an edit changes: a Navidrome field, or a raw key as "key:NAME".
export type Target = string
export const keyTarget = (key: string): Target => `key:${key}`

export interface Staged {
  values: string[]
  anchor?: string
}

// Stage holds the edits waiting for Save, per track and target. An edit that
// reaches the whole album is the same edit on every track.
export type Stage = Record<number, Record<Target, Staged>>

export interface MainField {
  field: string
  label: string
  // perTrack is a tag that belongs to one track, read-only at All tracks.
  perTrack?: boolean
  multi?: boolean
  flag?: boolean
}

export const mainFields: MainField[] = [
  { field: 'title', label: 'Title', perTrack: true },
  { field: 'artist', label: 'Artist' },
  { field: 'album', label: 'Album' },
  { field: 'albumartist', label: 'Album Artist' },
  { field: 'track', label: 'Track', perTrack: true },
  { field: 'disc', label: 'Disc' },
  { field: 'genre', label: 'Genre', multi: true },
  { field: 'releasedate', label: 'Release date' },
  { field: 'originaldate', label: 'Original date' },
  { field: 'recordingdate', label: 'Recording date' },
  { field: 'compilation', label: 'Compilation', flag: true },
]

const same = (a: string[], b: string[]) => a.length === b.length && a.every((v, i) => v === b[i])

function keyOf(t: LibraryTrack, target: Target): TagKey | undefined {
  if (target.startsWith('key:')) {
    const name = target.slice(4)
    return t.standard.find((k) => k.key === name) ?? t.extended.find((k) => k.key === name)
  }
  return t.standard.find((k) => k.fields?.includes(target))
}

// fileValues are what the track's file holds for a target now.
export function fileValues(t: LibraryTrack, target: Target): string[] {
  return keyOf(t, target)?.values ?? []
}

// stagedFor finds a target's edit. A fix may stage a raw key, such as
// RELEASEDATE, and the field that key feeds has to show it.
function stagedFor(t: LibraryTrack, target: Target, stage: Stage): Staged | undefined {
  const edits = stage[t.id]
  if (!edits) return undefined
  return edits[target] ?? (target.startsWith('key:') ? undefined : edits[keyTarget(keyOf(t, target)?.key ?? target.toUpperCase())])
}

export function currentValues(t: LibraryTrack, target: Target, stage: Stage): string[] {
  return stagedFor(t, target, stage)?.values ?? fileValues(t, target)
}

export interface FieldView {
  values: string[]
  mixed: boolean
  staged: boolean
  // was is the field before the staged edit, for the line under it.
  was: { values: string[]; mixed: boolean } | null
  readOnly: boolean
}

function summary(lists: string[][]) {
  const first = lists[0] ?? []
  const mixed = lists.some((l) => !same(l, first))
  return { values: mixed ? [] : first, mixed }
}

// fieldView is a field as the editor shows it across the tracks an edit reaches.
export function fieldView(tracks: LibraryTrack[], target: Target, stage: Stage): FieldView {
  const now = summary(tracks.map((t) => currentValues(t, target, stage)))
  const staged = tracks.some((t) => stagedFor(t, target, stage))
  return {
    ...now,
    staged,
    was: staged ? summary(tracks.map((t) => fileValues(t, target))) : null,
    readOnly: tracks.some((t) => keyOf(t, target)?.read_only),
  }
}

// stageEdit sets a target on tracks. A track that already holds the value drops
// its edit, because a Save that changes nothing is refused.
export function stageEdit(stage: Stage, tracks: LibraryTrack[], target: Target, values: string[], anchor?: string): Stage {
  const next = { ...stage }
  for (const t of tracks) {
    const edits = { ...next[t.id] }
    if (same(fileValues(t, target), values)) delete edits[target]
    else edits[target] = anchor ? { values, anchor } : { values }
    put(next, t.id, edits)
  }
  return next
}

function put(stage: Stage, id: number, edits: Record<Target, Staged>) {
  if (Object.keys(edits).length > 0) stage[id] = edits
  else delete stage[id]
}

export function revert(stage: Stage, tracks: LibraryTrack[], target: Target): Stage {
  const next = { ...stage }
  for (const t of tracks) {
    const edits = { ...next[t.id] }
    delete edits[target]
    if (!target.startsWith('key:')) delete edits[keyTarget(keyOf(t, target)?.key ?? target.toUpperCase())]
    put(next, t.id, edits)
  }
  return next
}

// addValue adds a value to a multi-value tag on each track that lacks it, so
// nothing a track holds is lost.
export function addValue(stage: Stage, tracks: LibraryTrack[], target: Target, value: string, anchor?: string): Stage {
  let next = stage
  for (const t of tracks) {
    const now = currentValues(t, target, next)
    if (!now.includes(value)) next = stageEdit(next, [t], target, [...now, value], anchor)
  }
  return next
}

// accept stages a suggestion. A fill gives each track its own value, on every
// track whatever the editor has selected.
export function accept(stage: Stage, all: LibraryTrack[], scope: LibraryTrack[], s: Suggestion, multi: boolean): Stage {
  if (s.tracks) {
    let next = stage
    for (const t of all) {
      const v = s.tracks[String(t.id)]
      if (v) next = stageEdit(next, [t], s.field, v, s.anchor)
    }
    return next
  }
  const values = s.values ?? []
  if (multi && values.length === 1) return addValue(stage, scope, s.field, values[0]!, s.anchor)
  return stageEdit(stage, scope, s.field, values, s.anchor)
}

// changes counts what the Save bar names: one per target, however many tracks it reaches.
export function changes(stage: Stage): number {
  return new Set(Object.values(stage).flatMap((e) => Object.keys(e))).size
}

export interface CoverEdit {
  upload?: string
  delete?: boolean
}

export interface SaveBody {
  tracks: { id: number; revision: string; edits: { field?: string; key?: string; values: string[]; anchor?: string }[] }[]
  confirm: string[]
  cover?: CoverEdit
}

// saveBody builds a Save. A cover edit needs every track with its revision.
export function saveBody(tracks: LibraryTrack[], stage: Stage, confirm: string[] = [], cover?: CoverEdit): SaveBody {
  const out: SaveBody = { tracks: [], confirm }
  for (const t of tracks) {
    const edits = Object.entries(stage[t.id] ?? {}).map(([target, e]) => ({
      ...(target.startsWith('key:') ? { key: target.slice(4) } : { field: target }),
      // An empty Add value row sends no value.
      values: e.values.filter((v) => v !== ''),
      ...(e.anchor ? { anchor: e.anchor } : {}),
    }))
    if (edits.length > 0 || cover) out.tracks.push({ id: t.id, revision: t.revision, edits })
  }
  if (cover) out.cover = cover
  return out
}

// A field's best suggestions, for the tracks an edit reaches. The answer holds
// album suggestions, and each track's own while one is selected.
export function suggestionsFor(s: Suggestions | null, target: Target, track: number | null): Suggestion[] {
  if (!s) return []
  const own = track === null ? [] : (s.tracks.find((t) => t.id === track)?.suggestions ?? [])
  const candidates = [...s.album.filter((x) => x.proposal), ...own, ...s.album.filter((x) => !x.proposal)]
  return candidates.filter((x) => x.field === target).flatMap((x) => {
    if (!x.proposal || track === null) return [x]
    const values = x.tracks?.[String(track)]
    return values ? [{ ...x, values, tracks: { [String(track)]: values } }] : []
  })
}

// offersFor picks what a field shows: its best few, without the value it holds. A
// value at All tracks that belongs to one track can't go on all of them, but a
// fill gives each track its own.
export function offersFor(s: Suggestions | null, target: Target, track: number | null, locked: boolean, held: string[]): Suggestion[] {
  const now = held.join('\u0000')
  let regular = 0
  return suggestionsFor(s, target, track).filter((x) => {
    if (x.proposal) return true
    if (!x.tracks && (locked || (x.values ?? []).join('\u0000') === now)) return false
    return regular++ < 3
  })
}

function common(tracks: LibraryTrack[], target: Target): string[] | null {
  const seen = new Map<string, { values: string[]; n: number }>()
  for (const t of tracks) {
    const v = fileValues(t, target)
    if (v.length === 0) continue
    const k = JSON.stringify(v)
    const e = seen.get(k) ?? { values: v, n: 0 }
    e.n++
    seen.set(k, e)
  }
  return [...seen.values()].sort((a, b) => b.n - a.n)[0]?.values ?? null
}

// stageReleaseDate is the RELEASEDATE fix, which keeps the year Navidrome shows.
// Under the legacy rule it moves DATE to RELEASEDATE and ORIGINALDATE to DATE. A
// key that already feeds the release date, YEAR or M4A's date, gives its value.
function stageReleaseDate(stage: Stage, tracks: LibraryTrack[]): Stage {
  let next = stage
  const release = keyTarget('RELEASEDATE')
  for (const t of tracks) {
    if (fileValues(t, release).length > 0) continue
    const feeds = t.standard.find((k) => k.key !== 'RELEASEDATE' && k.fields?.includes('releasedate'))
    const date = fileValues(t, keyTarget('DATE'))
    const original = fileValues(t, keyTarget('ORIGINALDATE'))
    if (feeds) next = stageEdit(next, [t], release, feeds.values)
    else if (date.length === 0) continue
    else {
      next = stageEdit(next, [t], release, date)
      if (original.length > 0) next = stageEdit(next, [t], keyTarget('DATE'), original)
    }
  }
  return next
}

function rewrite(stage: Stage, tracks: LibraryTrack[], target: Target): Stage {
  let next = stage
  for (const t of tracks) {
    const v = fileValues(t, target)
    if (v.length > 0) next = { ...next, [t.id]: { ...next[t.id], [target]: { values: v } } }
  }
  return next
}

// fix stages what an issue's Fix stages, or answers null when there is nothing
// to stage, and then the issue has no Fix. A reissue year has none, because no
// rule can tell which date the owner wants.
export function fix(issue: LibraryIssue, tracks: LibraryTrack[], s: Suggestions | null, stage: Stage): Stage | null {
  const best = (field: string) => suggestionsFor(s, field, null)[0]
  let next = stage
  switch (issue.kind) {
    case 'no_releasedate':
      next = stageReleaseDate(stage, tracks)
      break
    case 'various_artists':
      next = stageEdit(stage, tracks, 'compilation', ['1'])
      break
    case 'values_differ':
      for (const f of issue.fields ?? []) {
        const v = best(f)?.values ?? common(tracks, f)
        if (v) next = stageEdit(next, tracks, f, v, best(f)?.anchor)
      }
      break
    case 'tags_disagree':
      // A field edit writes every key that feeds the field, so even the value a
      // file's first key holds brings the others in line. A track whose keys
      // agree stays as it is.
      for (const f of issue.fields ?? []) {
        const own = tracks.filter((t) => t.issues.some((i) => i.kind === 'tags_disagree' && i.fields?.includes(f)))
        const b = best(f)
        if (b) next = accept(next, own, own, b, false)
        else next = rewrite(next, own, f)
      }
      break
    case 'missing_tag':
    case 'two_releases':
    case 'artist_split':
      for (const f of issue.fields ?? []) {
        const b = best(f)
        if (b) next = accept(next, tracks, tracks, b, false)
      }
      break
    default:
      return null
  }
  return next === stage ? null : next
}

// Tags that belong to one track, read-only at All tracks.
const trackFields = new Set(['title', 'titlesort', 'track', 'lyrics', 'musicbrainz_trackid', 'musicbrainz_recordingid', 'replaygain_track_gain', 'replaygain_track_peak'])
export const isLong = (values: string[]) => values.some((v) => v.length > longValue)
const multiKeys = new Set(['ARTISTS', 'ALBUMARTISTS', 'GENRE', 'MOOD', 'COMPOSER', 'LYRICIST', 'PRODUCER', 'ENGINEER', 'MIXER', 'REMIXER', 'ARRANGER', 'CONDUCTOR', 'DIRECTOR', 'DJMIXER', 'PERFORMER'])

export interface TagRow {
  key: string
  group: 'standard' | 'extended'
  // target is what the row edits. It is the field when this key is the one a main
  // row shows, so two rows never stage two edits on one tag.
  target: Target
  fields: string[]
  perTrack: boolean
  multi: boolean
  // custom is a key the file doesn't hold yet, staged here.
  custom: boolean
}

function rowOf(key: string, group: TagRow['group'], fields: string[], tracks: LibraryTrack[], custom: boolean): TagRow {
  const field = fields.find((f) => tracks.some((t) => keyOf(t, f)?.key === key))
  return {
    key, group, fields, custom,
    target: field ?? keyTarget(key),
    perTrack: key === 'ISRC' || key.startsWith('LYRICS') || fields.some((f) => trackFields.has(f)),
    multi: multiKeys.has(key) || tracks.some((t) => (keyOf(t, keyTarget(key))?.values.length ?? 0) > 1),
  }
}

// tagRows lists every tag of the tracks for All fields, and the targets only the
// stage or a pending proposal names. known lists the keys Navidrome reads.
export function tagRows(tracks: LibraryTrack[], stage: Stage, known: string[], proposed: Target[] = []): TagRow[] {
  const rows = new Map<string, TagRow>()
  for (const group of ['standard', 'extended'] as const) {
    for (const t of tracks) {
      for (const k of t[group]) {
        if (!rows.has(k.key)) rows.set(k.key, rowOf(k.key, group, k.fields ?? [], tracks, false))
      }
    }
  }
  const targets = [...tracks.flatMap((t) => Object.keys(stage[t.id] ?? {})), ...proposed]
  for (const target of targets) {
    const key = target.startsWith('key:') ? target.slice(4) : null
    if (key) {
      if (!rows.has(key)) rows.set(key, rowOf(key, known.includes(key) ? 'standard' : 'extended', [], tracks, true))
      continue
    }
    // A field no file holds yet, such as Album artists an agent proposes, gets a row of its own, so its tape shows.
    // Its key is the field's usual tag name; Save writes the field and picks the key itself.
    if (mainFields.some((f) => f.field === target) || [...rows.values()].some((r) => r.fields.includes(target))) continue
    const name = target.toUpperCase()
    if (!rows.has(name)) rows.set(name, { ...rowOf(name, known.includes(name) ? 'standard' : 'extended', [target], tracks, true), target })
  }
  return [...rows.values()]
}

const releaseId = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i

// pastedRelease finds the MusicBrainz release in a value typed into the album ID
// field, which may be an ID or a link. Pasting one moves the anchor.
export function pastedRelease(value: string): string | null {
  return releaseId.exec(value)?.[0].toLowerCase() ?? null
}

// A value this long doesn't fit one line, and opens the long-form editor.
export const longValue = 100

// tagKey is a typed key as a file holds it: capitals, no spaces at the ends.
export const tagKey = (typed: string) => typed.trim().toUpperCase()

const words: Record<string, string> = {
  missing_tag: 'Missing required tag',
  split_album: 'Split album',
  values_differ: 'Values differ across tracks',
  tags_disagree: 'Tags disagree',
  reissue_year: 'Navidrome shows a reissue year',
  no_releasedate: 'No RELEASEDATE',
  various_artists: 'Various Artists without the compilation flag',
  artist_split: 'Wrong artist split',
  two_releases: 'IDs from two releases',
  no_cover: 'No cover',
  bad_cover: 'Damaged cover',
  guide_albumartists: 'No ALBUMARTISTS',
  guide_replaygain: 'No ReplayGain',
}

export const issueName = (kind: string) => words[kind] ?? kind
export const isGuide = (kind: string) => kind.startsWith('guide_')

const fieldLabels: Record<string, string> = {
  titlesort: 'Title sort',
  artistsort: 'Artist sort',
  artists: 'Artists',
  artistssort: 'Artists sort',
  composersort: 'Composer sort',
  lyricistsort: 'Lyricist sort',
  djmixer: 'DJ mixer',
  albumartistsort: 'Album artist sort',
  albumartists: 'Album artists',
  albumartistssort: 'Album artists sort',
  albumsort: 'Album sort',
  albumversion: 'Album version',
  tracktotal: 'Track total',
  disctotal: 'Disc total',
  discsubtitle: 'Disc subtitle',
  bpm: 'BPM',
  catalognumber: 'Catalog number',
  releasetype: 'Release type',
  explicitstatus: 'Explicit status',
  asin: 'ASIN',
  encodedby: 'Encoded by',
  encodersettings: 'Encoder settings',
  isrc: 'ISRC',
  movementname: 'Movement name',
  movementtotal: 'Movement total',
  recordlabel: 'Record label',
  releasecountry: 'Release country',
  releasestatus: 'Release status',
  musicbrainz_artistid: 'MusicBrainz artist ID',
  musicbrainz_recordingid: 'MusicBrainz recording ID',
  musicbrainz_trackid: 'MusicBrainz track ID',
  musicbrainz_albumartistid: 'MusicBrainz album artist ID',
  musicbrainz_albumid: 'MusicBrainz album ID',
  musicbrainz_releasegroupid: 'MusicBrainz release group ID',
  musicbrainz_composerid: 'MusicBrainz composer ID',
  musicbrainz_lyricistid: 'MusicBrainz lyricist ID',
  musicbrainz_directorid: 'MusicBrainz director ID',
  musicbrainz_producerid: 'MusicBrainz producer ID',
  musicbrainz_engineerid: 'MusicBrainz engineer ID',
  musicbrainz_mixerid: 'MusicBrainz mixer ID',
  musicbrainz_remixerid: 'MusicBrainz remixer ID',
  musicbrainz_djmixerid: 'MusicBrainz DJ mixer ID',
  musicbrainz_conductorid: 'MusicBrainz conductor ID',
  musicbrainz_arrangerid: 'MusicBrainz arranger ID',
  musicbrainz_performerid: 'MusicBrainz performer ID',
  musicbrainz_discid: 'MusicBrainz disc ID',
  musicbrainz_workid: 'MusicBrainz work ID',
  replaygain_album_gain: 'ReplayGain album gain',
  replaygain_album_peak: 'ReplayGain album peak',
  replaygain_track_gain: 'ReplayGain track gain',
  replaygain_track_peak: 'ReplayGain track peak',
  r128_album_gain: 'R128 album gain',
  r128_track_gain: 'R128 track gain',
}

export function fieldLabel(field: string): string {
  const readable = field.replace(/_/g, ' ')
  return mainFields.find((f) => f.field === field)?.label ?? fieldLabels[field] ?? readable.charAt(0).toUpperCase() + readable.slice(1)
}

// issueDetail is the line under an issue's name.
export function issueDetail(i: LibraryIssue): string {
  const names = (i.fields ?? []).map(fieldLabel).join(', ')
  switch (i.kind) {
    case 'missing_tag':
      return `A track has no ${names}, so Navidrome shows a placeholder.`
    case 'split_album':
      return 'Two albums in this folder share a name, and Navidrome shows them apart.'
    case 'values_differ':
      return `${names} differ across the tracks.`
    case 'tags_disagree':
      return `Two tags feed ${names} with different values.`
    case 'no_releasedate':
      return 'The fix adds the tag and keeps the year Navidrome shows.'
    case 'reissue_year':
      return 'Navidrome shows a year later than the original date. Choose the date you want in the date fields.'
    case 'various_artists':
      return 'The album artist is Various Artists, and a track lacks the flag.'
    case 'artist_split':
      return "The MusicBrainz artist IDs don't match the artists Navidrome splits from the tags."
    case 'two_releases':
      return 'The tracks hold MusicBrainz IDs of two releases.'
    case 'no_cover':
      return 'Open the cover to add one.'
    case 'bad_cover':
      return "ffmpeg can't read it. Repair stages a clean copy."
    default:
      return names
  }
}
