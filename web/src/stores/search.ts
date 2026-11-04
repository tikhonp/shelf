import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { ApiError, errorText, get, post } from '../api/client'
import type { Item, Page, SearchType } from '../api/types'
import { linkKind } from '../lib/links'
import { load as loadSetting, save as saveSetting } from '../lib/storage'
import { useSources } from './sources'

const pageSize = 20
const offKey = 'shelf.sources.off'

// SourceResults is what one source answered for the current query.
export interface SourceResults {
  id: string
  items: Item[]
  total: number
  loading: boolean
  error: string
  rateLimited: boolean
  // ended is set when the source answered an empty page, whatever its total says.
  ended: boolean
}

export interface LinkResult {
  loading: boolean
  item: Item | null
  error: string
}

export const typeWords: Record<SearchType, { one: string; many: string }> = {
  album: { one: 'album', many: 'albums' },
  track: { one: 'track', many: 'tracks' },
  artist: { one: 'artist', many: 'artists' },
  playlist: { one: 'playlist', many: 'playlists' },
}

// mergeByRank takes each source's first item, then each source's second, and so on.
export function mergeByRank(lists: Item[][]): Item[] {
  const out: Item[] = []
  const longest = Math.max(0, ...lists.map((l) => l.length))
  for (let i = 0; i < longest; i++) {
    for (const l of lists) {
      const item = l[i]
      if (item) out.push(item)
    }
  }
  return out
}

function readOff(): string[] {
  try {
    const v = JSON.parse(loadSetting(offKey) ?? '[]') as unknown
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
  } catch {
    return []
  }
}

export const useSearch = defineStore('search', () => {
  const query = ref('')
  const type = ref<SearchType>('album')
  const off = ref<string[]>(readOff())
  // ran is the text of the search on show, which the field may have left behind.
  const ran = ref('')
  const ranType = ref<SearchType>('album')
  const results = ref<SourceResults[]>([])
  const link = ref<LinkResult | null>(null)
  let run = 0

  const on = computed(() => useSources().enabled.filter((s) => !off.value.includes(s.id)))
  // searched are the sources that can search the chosen type.
  const searched = computed(() => on.value.filter((s) => s.capabilities.search.includes(type.value)))

  const items = computed(() => mergeByRank(results.value.map((r) => r.items)))
  const total = computed(() => results.value.reduce((n, r) => n + r.total, 0))
  const loading = computed(() => results.value.some((r) => r.loading) || !!link.value?.loading)
  const failed = computed(() => results.value.filter((r) => r.error))
  const hasMore = computed(() => results.value.some((r) => canGrow(r)))
  const noSources = computed(() => useSources().loaded && on.value.length === 0)

  function canGrow(r: SourceResults) {
    return !r.error && !r.ended && r.items.length < r.total
  }

  async function fetchPage(r: SourceResults, text: string, mine: number) {
    r.loading = true
    r.error = ''
    r.rateLimited = false
    const q = new URLSearchParams({ source: r.id, type: type.value, q: text, limit: String(pageSize), offset: String(r.items.length) })
    try {
      const page = await get<Page>(`/api/search?${q}`)
      if (mine !== run) return
      r.items = [...r.items, ...page.items]
      r.total = page.total
      r.ended = page.items.length === 0
    } catch (err) {
      if (mine !== run) return
      r.error = errorText(err)
      r.rateLimited = err instanceof ApiError && err.status === 429
    } finally {
      if (mine === run) r.loading = false
    }
  }

  async function resolve(url: string, mine: number) {
    link.value = { loading: true, item: null, error: '' }
    try {
      const item = await post<Item>('/api/resolve', { url })
      if (mine === run) link.value = { loading: false, item, error: '' }
    } catch (err) {
      if (mine === run) link.value = { loading: false, item: null, error: errorText(err) }
    }
  }

  // submit runs what the field holds: a link resolves, and text searches every
  // source that is on.
  async function submit() {
    const text = query.value.trim()
    const mine = ++run
    ran.value = text
    ranType.value = type.value
    results.value = []
    link.value = null
    if (!text) return
    if (linkKind(text)) return resolve(text, mine)
    results.value = searched.value.map((s) => ({ id: s.id, items: [], total: 0, loading: true, error: '', rateLimited: false, ended: false }))
    await Promise.all(results.value.map((r) => fetchPage(r, text, mine)))
  }

  async function loadMore() {
    const mine = run
    await Promise.all(results.value.filter(canGrow).map((r) => fetchPage(r, ran.value, mine)))
  }

  // fromRoute runs what the address asks for, as the bot's links open it. The
  // sources must be known first, to tell which ones to ask.
  async function fromRoute(q: string, t?: string) {
    const sources = useSources()
    if (!sources.loaded) await sources.load().catch(() => {})
    if (t && t in typeWords) type.value = t as SearchType
    if (q === ran.value && type.value === ranType.value) return
    query.value = q
    await submit()
  }

  function setType(t: SearchType) {
    if (type.value === t) return
    type.value = t
    if (ran.value && !link.value) void submit()
  }

  function toggle(id: string) {
    off.value = off.value.includes(id) ? off.value.filter((x) => x !== id) : [...off.value, id]
    saveSetting(offKey, JSON.stringify(off.value))
    if (ran.value && !link.value) void submit()
  }

  function clear() {
    run++
    query.value = ''
    ran.value = ''
    results.value = []
    link.value = null
  }

  return { query, type, off, ran, results, link, on, searched, items, total, loading, failed, hasMore, noSources, submit, fromRoute, loadMore, setType, toggle, clear }
})
