import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { errorText, get } from '../api/client'
import type { LibraryAlbum, LibraryFilters, LibraryIndex, LibraryPage } from '../api/types'

export type LibrarySort = 'album' | 'artist' | 'year' | 'added'

const pageSize = 100
const searchWait = 250

// The album list and what the filter menu offers.
export const useLibrary = defineStore('library', () => {
  const albums = ref<LibraryAlbum[]>([])
  const total = ref(0)
  const loading = ref(false)
  const error = ref('')
  const query = ref('')
  const filter = ref('all')
  const folder = ref('')
  const sort = ref<LibrarySort>('album')
  const filters = ref<LibraryFilters | null>(null)
  const index = ref<LibraryIndex | null>(null)
  let controller: AbortController | null = null
  let timer: ReturnType<typeof setTimeout> | undefined

  function address(offset: number) {
    const p = new URLSearchParams({ sort: sort.value, limit: String(pageSize), offset: String(offset) })
    // Newest first is what a year or a date is sorted for.
    if (sort.value === 'year' || sort.value === 'added') p.set('order', 'desc')
    if (query.value.trim()) p.set('q', query.value.trim())
    if (filter.value !== 'all') p.set('filter', filter.value)
    if (folder.value) p.set('folder', folder.value)
    return `/api/library/albums?${p}`
  }

  async function fetchPage(offset: number) {
    controller?.abort()
    const mine = new AbortController()
    controller = mine
    loading.value = true
    error.value = ''
    try {
      const page = await get<LibraryPage>(address(offset), mine.signal)
      albums.value = offset === 0 ? page.albums : [...albums.value, ...page.albums]
      total.value = page.total
    } catch (err) {
      if (!mine.signal.aborted) error.value = errorText(err)
    } finally {
      if (controller === mine) loading.value = false
    }
  }

  const load = () => fetchPage(0)
  const more = () => fetchPage(albums.value.length)
  const hasMore = computed(() => albums.value.length < total.value)

  function search(text: string) {
    query.value = text
    clearTimeout(timer)
    timer = setTimeout(() => void load(), searchWait)
  }

  function setSort(s: LibrarySort) {
    sort.value = s
    void load()
  }

  function setFilter(f: string, inFolder = '') {
    filter.value = f
    folder.value = inFolder
    void load()
  }

  function clear() {
    query.value = ''
    setFilter('all')
  }

  async function loadFilters() {
    filters.value = await get<LibraryFilters>('/api/library/filters')
  }

  async function loadIndex() {
    index.value = await get<LibraryIndex>('/api/library/index')
  }

  // A walk that ends changes what the list holds, and one running on an empty
  // index fills it as it goes.
  function applyIndex(next: LibraryIndex) {
    // The list isn't always open, so with no state yet a finished walk counts.
    const ended = !next.indexing && (index.value?.indexing ?? true)
    index.value = next
    if (ended) void Promise.allSettled([load(), loadFilters()])
    return ended
  }

  const filtering = computed(() => filter.value !== 'all' || folder.value !== '')

  return { albums, total, loading, error, query, filter, folder, sort, filters, index, hasMore, filtering, load, more, search, setSort, setFilter, clear, loadFilters, loadIndex, applyIndex }
})
