// The JSON shapes of shelf's API, as docs/architecture.md lists them.

export type Kind = 'track' | 'album' | 'playlist' | 'artist' | 'label'
export type SearchType = 'album' | 'track' | 'artist' | 'playlist'

export interface Ref {
  source: string
  kind: Kind
  id: string
}

export interface Quality {
  codec?: string
  bit_depth?: number
  sample_rate?: number
  hires?: boolean
  atmos?: boolean
  badge?: string
}

export type ItemState = 'in_library' | 'queued' | 'running' | 'partial' | 'failed'

export interface ItemStatus {
  state: ItemState
  download_id?: number
  tracks_done?: number
  tracks_total?: number
}

export interface Track {
  id: string
  position: number
  disc?: number
  title: string
  version?: string
  artist?: string
  explicit?: boolean
  duration?: number
  quality: Quality
  status?: ItemStatus
}

export interface Section {
  id: string
  title: string
  items: Item[]
}

export interface Item {
  ref: Ref
  title: string
  version?: string
  artist?: string
  album?: string
  year?: number
  release_date?: string
  label?: string
  upc?: string
  genre?: string
  explicit?: boolean
  track_count?: number
  duration?: number
  cover_url?: string
  url?: string
  quality: Quality
  tracks?: Track[]
  sections?: Section[]
  status?: ItemStatus
}

export interface Page {
  items: Item[]
  total: number
  limit: number
  offset: number
}

export interface Format {
  id: string
  label: string
  description: string
  default?: boolean
}

export interface Health {
  ok: boolean
  message?: string
  detail?: unknown
}

export interface Source {
  id: string
  name: string
  enabled: boolean
  capabilities: { links: boolean; search: Kind[]; resolve: boolean; download: boolean }
  formats: Format[]
  health: Health
}

export type DownloadStatus = 'queued' | 'running' | 'done' | 'partial' | 'failed' | 'canceled' | 'skipped'

export interface Download {
  author?: string
  author_id?: string | null
  id: number
  parent_id: number | null
  source: string
  kind: Kind
  item_id: string
  url: string
  format: string
  title: string
  artist: string
  cover_url: string
  status: DownloadStatus
  error: string
  tracks_total: number
  tracks_done: number
  tracks_failed: number
  bytes: number
  folder: string
  origin: string
  origin_user: string
  created_at: string
  started_at: string | null
  finished_at: string | null
}

export interface DownloadTrack {
  position: number
  disc: number
  item_id: string
  title: string
  artist: string
  status: 'pending' | 'running' | 'done' | 'skipped' | 'failed'
  path: string
  codec: string
  bit_depth: number
  sample_rate: number
  size: number
  error: string
}

export interface DownloadDetail extends Download {
  tracks: DownloadTrack[]
  children: Download[]
  provenance_path: string
}

export interface DownloadList {
  downloads: Download[]
  next: number | null
}

export interface DownloadEvent {
  author?: string
  author_id?: string | null
  id: number
  parent_id: number | null
  status: DownloadStatus
  tracks_done: number
  tracks_total: number
  tracks_failed: number
  current: string
  bytes: number
  error: string
}

export interface ScanEvent {
  scanning: boolean
  count: number
  last_scan: string
}

export interface ScanState extends ScanEvent {
  enabled: boolean
  ok: boolean
  message: string
  address: string
  latency_ms: number
}

export interface MusicBrainzState {
  ok: boolean
  message: string
  latency_ms: number
  checked_at: string
}

export type WrapperState = 'starting' | 'running' | 'signed_out' | 'signing_in' | 'awaiting_code' | 'error'

export interface Wrapper {
  state: WrapperState
  storefront: string
  since: string
  error: string
  code_deadline: string | null
}

export interface SourceEvent {
  id: string
  ok: boolean
  message: string
  wrapper?: Wrapper | null
}

export interface Telegram {
  enabled: boolean
  connected: boolean
  username: string
  users: number
}

export interface LogLine {
  time: string
  level: string
  step: string
  text: string
}

export interface WrapperLogLine {
  n: number
  time: string
  text: string
}

// The Library, as /api/library answers it.

export interface LibraryIssue {
  kind: string
  fields?: string[]
}

// How shelf knows an album's source: shelf's own download, an older tool's
// history, the folder's shelf.txt, or the source folder the album sits in.
export type LibrarySourceBy = 'download' | 'import' | 'file' | 'folder'

export interface LibrarySource {
  // Empty when nothing tells.
  id: string
  name: string
  by?: LibrarySourceBy
  kind?: string
  item_id?: string
  download_id?: number
  quality?: string
  at?: string
  link?: string
  // The folder the record names, when the album was moved out of it.
  recorded?: string
}

export interface LibraryAlbum {
  id: number
  folder: string
  title: string
  artist: string
  year: number
  tracks: number
  quality: string
  duration: number
  size: number
  added_at: string
  navidrome_id: string
  has_mbid: boolean
  lamp: boolean
  issues: LibraryIssue[]
  source: LibrarySource
}

export interface LibraryPage {
  albums: LibraryAlbum[]
  total: number
  limit: number
  offset: number
}

export interface LibraryCount {
  name: string
  albums: number
}

export interface LibraryFilters {
  albums: number
  issues: number
  no_mbid: number
  kinds: LibraryCount[]
  folders: LibraryCount[]
  sources: { id: string; name: string; albums: number }[]
  no_source: number
}

export interface LibraryIndex {
  indexing: boolean
  ready: boolean
  albums: number
  tracks: number
  read: number
  error: string
}

export interface TagKey {
  key: string
  fields?: string[]
  values: string[]
  read_only?: boolean
}

export interface LibraryTrack {
  id: number
  path: string
  disc: number
  track: number
  title: string
  artist: string
  duration: number
  quality: string
  format: string
  size: number
  revision: string
  error: string
  issues: LibraryIssue[]
  standard: TagKey[]
  extended: TagKey[]
  pictures: { type: string; mime: string; description: string }[]
}

export interface LibraryAlbumDetail extends Omit<LibraryAlbum, 'tracks'> {
  tracks: LibraryTrack[]
  stale: boolean
}

export interface Suggestion {
  field: string
  values?: string[]
  tracks?: Record<string, string[]>
  source: string
  sources: string[]
  confidence: number
  anchor?: string
}

export interface CoverSuggestion {
  image: string
  source: string
  confidence: number
  anchor?: string
}

export interface LibraryAnchor {
  id: string
  by: string
  title: string
  artist: string
  date: string
  country: string
  tracks: number
  url: string
  matched: boolean
  reason?: string
}

export interface Suggestions {
  anchor: LibraryAnchor | null
  album: Suggestion[]
  tracks: { id: number; suggestions: Suggestion[] }[]
  covers: CoverSuggestion[]
  notes: { track?: number; field: string; message: string }[]
  unavailable: { source: string; message: string }[]
  // fingerprinting says a job is identifying the album by its audio. A
  // suggestions event follows.
  fingerprinting: boolean
}

export interface CoverUpload {
  id: string
  mime: string
  width: number
  height: number
  size: number
}

export type ChangeState = 'queued' | 'writing' | 'done' | 'partial' | 'failed'

export interface ChangeConflict {
  path: string
  key: string
}

export interface ChangeFile {
  track_id: number
  path: string
  kind: 'audio' | 'cover'
  written: boolean
  revision: string
  keys: { key: string; before: string[]; after: string[] }[]
  cover_before: string
  cover_after: string
}

// DeletedFolder is what a delete removed, and how Navidrome took it.
export interface DeletedFolder {
  files: number
  bytes: number
  // albums are the other albums the folder held, which went with it.
  albums: { title: string; artist: string }[]
  // forgot counts the downloaded items that download again now.
  forgot: number
  navidrome: '' | 'waiting' | 'removed' | 'purges' | 'none' | 'failed' | 'off'
  navidrome_tracks: number
}

export interface LibraryChange {
  author?: string
  author_id?: string | null
  id: number
  kind: 'save' | 'undo' | 'redo' | 'delete'
  target: number | null
  album_id: number
  folder: string
  title: string
  artist: string
  state: ChangeState
  reason: string
  error: string
  undone: boolean
  tags: number
  cover: boolean
  conflicts: ChangeConflict[]
  job_id: number | null
  created_at: string
  finished_at: string | null
  files?: ChangeFile[]
  // run says this row is a bulk run, and its tags count all its albums.
  run?: RunSummary
  deleted?: DeletedFolder
  // origin names the proposal acceptance a Save came from: "proposal:<p>/acceptance:<a>".
  origin?: string | null
}

export interface RunSummary {
  fixes: string[]
  albums: number
  files: number
  skipped: number
  failed: number
}

export interface RunSelection {
  albums?: number[]
  all?: boolean
  q?: string
  filter?: string
  folder?: string
  source?: string
  except?: number[]
}

export interface RunSample {
  album: string
  key: string
  before: string
  after: string
}

export interface RunPreview {
  selected: number
  fixes: { fix: string; albums: number }[]
  albums: number
  files: number
  bytes: number
  samples: RunSample[]
}

export interface RunLine {
  at: string
  album: string
  state: 'done' | 'skipped' | 'failed'
  fixes: number
  files: number
  message: string
}

export interface RunProgress {
  id: number
  kind: 'save' | 'undo' | 'redo'
  fixes: string[]
  state: ChangeState
  reason: string
  error: string
  cancelling: boolean
  total: number
  done: number
  skipped: number
  failed: number
  files: number
  now: string
  now_state: '' | 'writing' | 'analyzing'
  log: RunLine[]
  created_at: string
  finished_at: string | null
}

export interface SaveWarning {
  kind: string
  message: string
}

// SaveRefusal is the body of a Save shelf won't run.
export interface SaveRefusal {
  error: string
  reason: string
  track?: number
  confirm?: SaveWarning[]
}
