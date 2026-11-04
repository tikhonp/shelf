import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { errorText, get, post } from '../api/client'
import type { Download, DownloadDetail, DownloadEvent, DownloadList, ItemStatus, Ref } from '../api/types'
import { useSettings } from './settings'
import { useSources } from './sources'

const activeLimit = 200
const historyLimit = 50
const day = 24 * 3600 * 1000

// Notice is what the notice slot shows after a download request, or when a
// download ends.
export interface Notice {
  title: string
  text: string
  sub: string
  cover: string
  round: boolean
  // lamp is empty for a notice that only repeats what the row shows, which the
  // desktop leaves out.
  lamp: '' | 'ok' | 'warn' | 'bad'
  // to is where View goes, or null for no View.
  to: string | null
}

const refKey = (source: string, kind: string, id: string) => `${source}:${kind}:${id}`

export const useDownloads = defineStore('downloads', () => {
  // items holds every download this page knows, by ID.
  const items = ref<Record<number, Download>>({})
  // currents holds the track each running download is fetching, which only events carry.
  const currents = ref<Record<number, string>>({})
  const details = ref<Record<number, DownloadDetail>>({})
  const historyNext = ref<number | null>(null)
  // historyFloor is the oldest ID of the pages read so far. Failed downloads
  // older than that are loaded for the counter and stay out of the history.
  const historyFloor = ref(0)
  const loaded = ref(false)
  const notice = ref<Notice | null>(null)
  const pending = new Set<number>()

  function put(list: Download[]) {
    for (const d of list) items.value[d.id] = d
  }

  async function load() {
    const [active, history, failed] = await Promise.all([
      get<DownloadList>(`/api/downloads?status=queued,running&limit=${activeLimit}`),
      get<DownloadList>(`/api/downloads?status=done,partial,failed,canceled,skipped&limit=${historyLimit}`),
      get<DownloadList>(`/api/downloads?status=failed&limit=${activeLimit}`),
    ])
    items.value = {}
    put(active.downloads)
    put(failed.downloads)
    put(history.downloads)
    historyNext.value = history.next
    historyFloor.value = history.next ?? 0
    loaded.value = true
    await Promise.all(Object.keys(details.value).map((id) => loadDetail(Number(id))))
  }

  async function loadMore() {
    if (historyNext.value === null) return
    const page = await get<DownloadList>(
      `/api/downloads?status=done,partial,failed,canceled,skipped&limit=${historyLimit}&before=${historyNext.value}`,
    )
    put(page.downloads)
    historyNext.value = page.next
    historyFloor.value = page.next ?? 0
  }

  async function loadDetail(id: number) {
    const d = await get<DownloadDetail>(`/api/downloads/${id}`)
    details.value[id] = d
    if (d.parent_id === null) items.value[id] = stripDetail(d)
    return d
  }

  function closeDetail(id: number) {
    delete details.value[id]
  }

  async function fetchUnknown(id: number) {
    if (pending.has(id)) return
    pending.add(id)
    try {
      items.value[id] = stripDetail(await get<DownloadDetail>(`/api/downloads/${id}`))
    } catch {
      // The next event or reconnect reads it again.
    } finally {
      pending.delete(id)
    }
  }

  // Track lines change on every event, and one read a second is all a person can follow.
  const refreshing = new Set<number>()
  function refreshDetail(id: number) {
    if (refreshing.has(id)) return
    refreshing.add(id)
    setTimeout(() => {
      refreshing.delete(id)
      if (details.value[id]) loadDetail(id).catch(() => {})
    }, 1000)
  }

  function applyEvent(e: DownloadEvent) {
    if (e.current) currents.value[e.id] = e.current
    else if (e.status !== 'running') delete currents.value[e.id]

    const { current: _current, ...fields } = e
    const known = items.value[e.id]
    const ended = e.status !== 'queued' && e.status !== 'running'
    // Events are live, so a download this page never saw ended just now too.
    const justEnded = ended && e.parent_id === null && !pending.has(e.id) && (!known || known.status === 'queued' || known.status === 'running')
    if (known) {
      items.value[e.id] = {
        ...known,
        ...fields,
        started_at: known.started_at ?? (e.status === 'running' ? new Date().toISOString() : null),
        finished_at: known.finished_at ?? (ended ? new Date().toISOString() : null),
      }
    }
    // A title, cover and final times arrive with the job, not the event.
    if (e.parent_id === null && (ended || !known)) {
      const read = fetchUnknown(e.id)
      if (justEnded) void read.then(() => announce(e.id))
    }

    if (details.value[e.id]) refreshDetail(e.id)
    if (e.parent_id !== null && details.value[e.parent_id]) refreshDetail(e.parent_id)
  }

  // announce shows that a download ended, as far as the setting asks for it.
  function announce(id: number) {
    const d = items.value[id]
    if (!d) return
    const level = useSettings().notices
    const failure = d.status === 'failed' || d.status === 'partial'
    if (level === 'off' || (level === 'failures' && !failure) || (!failure && d.status !== 'done')) return
    const base = { text: d.title || d.url, cover: d.cover_url, round: d.kind === 'artist', to: `/downloads/${d.id}` }
    if (d.status === 'failed') notice.value = { ...base, title: 'Failed', sub: d.error || 'The download failed', lamp: 'bad' }
    else if (d.status === 'partial') notice.value = { ...base, title: 'Done with errors', sub: `${d.tracks_done} of ${d.tracks_total} tracks`, lamp: 'warn' }
    else notice.value = { ...base, title: 'Downloaded', sub: `${kindName(d.kind)} · ${d.artist}`.replace(/ · $/, ''), lamp: 'ok' }
  }

  function ahead(d: Download): number {
    return all.value.filter((x) => x.source === d.source && x.id < d.id && (x.status === 'queued' || x.status === 'running')).length
  }

  async function queue(ref_: Ref, opts: { format?: string; force?: boolean; title?: string; cover?: string } = {}) {
    const sources = useSources()
    const kind = ref_.kind
    try {
      const d = await post<Download>('/api/downloads', { ref: ref_, format: opts.format, force: opts.force })
      items.value[d.id] = d
      const label = sources.formatLabel(d.source, d.format)
      const skipped = d.status === 'skipped'
      const waiting = ahead(d)
      notice.value = {
        title: skipped ? 'Already in the library' : 'Queued',
        text: d.title || opts.title || '',
        sub: skipped ? 'Nothing to download' : [kindName(kind), label, waiting ? `${waiting} ahead of it` : ''].filter(Boolean).join(' · '),
        cover: d.cover_url || opts.cover || '',
        round: kind === 'artist',
        lamp: '',
        to: '/downloads',
      }
      return d
    } catch (err) {
      notice.value = { title: "Couldn't queue", text: opts.title ?? '', sub: errorText(err), cover: opts.cover ?? '', round: false, lamp: 'bad', to: null }
      return null
    }
  }

  async function cancel(id: number) {
    put([await post<Download>(`/api/downloads/${id}/cancel`)])
  }

  async function retry(id: number) {
    put([await post<Download>(`/api/downloads/${id}/retry`)])
    if (details.value[id]) await loadDetail(id)
  }

  const all = computed(() => Object.values(items.value).sort((a, b) => a.id - b.id))
  const running = computed(() => all.value.filter((d) => d.status === 'running'))
  const queued = computed(() => all.value.filter((d) => d.status === 'queued'))
  const busy = computed(() => running.value.length + queued.value.length > 0)
  const history = computed(() =>
    all.value
      .filter((d) => d.status !== 'queued' && d.status !== 'running' && d.id >= historyFloor.value)
      .reverse(),
  )

  function failedSince(now: number): number {
    return all.value.filter((d) => d.status === 'failed' && d.finished_at && now - Date.parse(d.finished_at) < day).length
  }

  // The desktop shows a request's result in the row, so only a notice with a lamp reaches it.
  const deskNotice = computed(() => (notice.value?.lamp ? notice.value : null))

  const byRef = computed(() => {
    const map = new Map<string, Download>()
    for (const d of all.value) map.set(refKey(d.source, d.kind, d.item_id), d)
    return map
  })

  // liveStatus is an item's status now: what this page saw of its downloads
  // beats what the search answered. A canceled download leaves no mark.
  function liveStatus(ref_: Ref, known?: ItemStatus): ItemStatus | undefined {
    const d = byRef.value.get(refKey(ref_.source, ref_.kind, ref_.id))
    if (!d) return known
    const base = { download_id: d.id, tracks_done: d.tracks_done, tracks_total: d.tracks_total }
    switch (d.status) {
      case 'queued':
      case 'running':
      case 'partial':
      case 'failed':
        return { state: d.status, ...base }
      case 'done':
      case 'skipped':
        return { state: 'in_library', ...base }
      case 'canceled':
        return known?.state === 'in_library' ? known : undefined
    }
  }

  return {
    items,
    currents,
    details,
    historyNext,
    loaded,
    notice,
    deskNotice,
    load,
    loadMore,
    loadDetail,
    closeDetail,
    applyEvent,
    queue,
    cancel,
    retry,
    ahead,
    all,
    running,
    queued,
    busy,
    history,
    failedSince,
    liveStatus,
  }
})

function stripDetail(d: DownloadDetail): Download {
  const { tracks: _tracks, children: _children, provenance_path: _path, ...rest } = d
  return rest
}

function kindName(kind: string): string {
  return kind.charAt(0).toUpperCase() + kind.slice(1)
}
