<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import type { Item, Kind, Ref, SearchType } from '../api/types'
import DownloadDeck from '../components/DownloadDeck.vue'
import Icon from '../components/Icon.vue'
import ItemPanel from '../components/ItemPanel.vue'
import NoticeSlot from '../components/NoticeSlot.vue'
import QueueShelf from '../components/QueueShelf.vue'
import SearchWindow from '../components/SearchWindow.vue'
import { isTyping, moved } from '../lib/cursor'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'
import { useSearch } from '../stores/search'
import { useStatus } from '../stores/status'

const route = useRoute()
const router = useRouter()
const phone = usePhone()
const search = useSearch()
const downloads = useDownloads()
const status = useStatus()
const cursor = ref(0)
const win = ref<InstanceType<typeof SearchWindow>>()

const q = computed(() => (typeof route.query.q === 'string' ? route.query.q : ''))
const type = computed(() => (typeof route.query.type === 'string' ? route.query.type : undefined))

// With downloads off the catalog search doesn't exist, so its addresses lead back to the library.
watch(
  () => downloads.enabled,
  (on) => {
    if (on === false) void router.replace('/library')
  },
  { immediate: true },
)

async function retryDetect() {
  await downloads.detect()
  if (downloads.enabled) void status.check()
}

watch(
  [() => downloads.enabled, q, type],
  ([on, text, t]) => {
    if (on !== true) return
    // Coming back to the tab keeps the search on show, and puts it in the address.
    if (!text) {
      if (search.ran) void router.replace({ query: { ...route.query, q: search.ran, type: search.type } })
      return
    }
    cursor.value = 0
    void search.fromRoute(text, t)
  },
  { immediate: true },
)

// The open panel lives in the address, so a phone's back button closes it.
const openRef = computed<Ref | null>(() => {
  const v = route.query.open
  if (typeof v !== 'string') return null
  const m = v.match(/^([^:]+):([^:]+):(.+)$/)
  return m ? { source: m[1]!, kind: m[2] as Kind, id: m[3]! } : null
})

function go(text: string) {
  const { q: _q, open: _open, ...rest } = route.query
  void router.replace({ query: text ? { ...rest, q: text, type: search.type } : rest })
}
function open(it: Item) {
  void router.push({ query: { ...route.query, open: `${it.ref.source}:${it.ref.kind}:${it.ref.id}` } })
}
function closePanel() {
  const { open: _open, ...rest } = route.query
  void router.replace({ query: rest })
}
function setType(t: SearchType) {
  search.setType(t)
  cursor.value = 0
  if (q.value) void router.replace({ query: { ...route.query, type: t } })
}
function findArtist(name: string) {
  void router.push({ path: '/search', query: { q: name, type: 'artist' } })
}

function key(e: KeyboardEvent) {
  if (downloads.enabled !== true) return
  if (e.key === 'Escape' && openRef.value) return closePanel()
  if (isTyping(e) || e.metaKey || e.ctrlKey || e.altKey) return
  if (e.key === '/') {
    e.preventDefault()
    win.value?.focus()
    return
  }
  const next = moved(e.key, cursor.value, search.items.length)
  if (next !== null) {
    e.preventDefault()
    cursor.value = next
  } else if (e.key === 'Enter' && (e.target as HTMLElement).tagName !== 'BUTTON' && (e.target as HTMLElement).tagName !== 'A') {
    const it = search.items[cursor.value]
    if (it) open(it)
  }
}
onMounted(() => {
  document.addEventListener('keydown', key)
  if (downloads.enabled === null) void downloads.detect()
})
onBeforeUnmount(() => document.removeEventListener('keydown', key))
</script>

<template>
  <div v-if="downloads.enabled === null && downloads.detectError" :class="phone ? 'stack' : 'page'">
    <section class="win sr-detect-err" aria-labelledby="sr-err-h">
      <div class="tb">
        <RouterLink v-if="phone" class="wbtn" to="/library" aria-label="Back to the library" title="Library"><Icon name="back" /></RouterLink>
        <h1 id="sr-err-h">Search</h1>
        <span class="stripes"></span>
      </div>
      <div class="pad col g8" role="alert">
        <p class="bad-text">{{ downloads.detectError }}</p>
        <div class="line g8">
          <button type="button" class="key sm" :disabled="downloads.detecting" @click="retryDetect">Retry</button>
        </div>
      </div>
    </section>
  </div>
  <template v-else-if="downloads.enabled">
  <div v-if="phone" class="stack">
    <ItemPanel v-if="openRef" :item-ref="openRef" @close="closePanel" @open="open" />
    <SearchWindow v-else ref="win" v-model:cursor="cursor" :narrow="false" @go="go" @open="open" @artist="findArtist" @type="setType" />
    <NoticeSlot />
  </div>
  <div v-else class="page">
    <SearchWindow ref="win" v-model:cursor="cursor" :narrow="!!openRef" @go="go" @open="open" @artist="findArtist" @type="setType" />
    <aside v-if="openRef || downloads.busy || downloads.deskNotice" class="page-side">
      <NoticeSlot />
      <ItemPanel v-if="openRef" :item-ref="openRef" @close="closePanel" @open="open" />
      <template v-else>
        <section v-if="downloads.running.length" class="win" aria-label="Current download">
          <div class="tb"><h2>Current download</h2><span class="stripes"></span></div>
          <div class="col g12 side"><DownloadDeck v-for="d in downloads.running" :key="d.id" :download="d" /></div>
        </section>
        <section v-if="downloads.queued.length" class="win" aria-label="Queue">
          <div class="tb"><h2>Queue · {{ downloads.queued.length }}</h2><span class="stripes"></span></div>
          <div class="side"><QueueShelf /></div>
        </section>
      </template>
    </aside>
  </div>
  </template>
</template>

<style scoped>
.rs .page > .sr-detect-err { flex: 1; max-width: 640px; }
.rs .sr-detect-err .pad { padding: 14px; }
.rs .side { padding: 14px; }
</style>
