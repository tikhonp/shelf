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
