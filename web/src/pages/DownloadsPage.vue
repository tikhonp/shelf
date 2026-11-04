<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import Counters from '../components/Counters.vue'
import CurrentDownload from '../components/CurrentDownload.vue'
import DownloadDeck from '../components/DownloadDeck.vue'
import DownloadPanel from '../components/DownloadPanel.vue'
import HistoryList from '../components/HistoryList.vue'
import Icon from '../components/Icon.vue'
import NoticeSlot from '../components/NoticeSlot.vue'
import QueueShelf from '../components/QueueShelf.vue'
import { useNow } from '../lib/clock'
import { isTyping, moved } from '../lib/cursor'
import { ago } from '../lib/format'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'
import { useStatus } from '../stores/status'

const route = useRoute()
const router = useRouter()
const phone = usePhone()
const now = useNow()
const downloads = useDownloads()
const status = useStatus()
const cursor = ref(-1)

const id = computed(() => (route.params.id ? Number(route.params.id) : undefined))
const close = () => void router.push('/downloads')
const scanWords = computed(() => {
  const s = status.scan
  if (!s?.enabled) return ''
  if (s.scanning) return 'Scanning'
  return s.last_scan ? `Last scan ${ago(s.last_scan, now.value)}` : 'No scan yet'
})

function key(e: KeyboardEvent) {
  if (e.key === 'Escape' && id.value) return close()
  if (isTyping(e)) return
  const next = moved(e.key, cursor.value, downloads.history.length)
  if (next !== null) {
    e.preventDefault()
    cursor.value = next
  } else if (e.key === 'Enter' && (e.target as HTMLElement).tagName === 'BODY') {
    const d = downloads.history[cursor.value]
    if (d) void router.push(`/downloads/${d.id}`)
  }
}
onMounted(() => document.addEventListener('keydown', key))
onBeforeUnmount(() => document.removeEventListener('keydown', key))
</script>

<template>
  <div v-if="phone" class="stack">
    <DownloadPanel v-if="id" :id="id" @close="close" />
    <section v-else class="win" aria-label="Downloads">
      <div class="tb">
        <h1>Downloads</h1>
        <span class="stripes"></span>
        <button v-if="status.scan?.enabled" type="button" class="wbtn" :disabled="status.scan.scanning" :aria-label="`Scan the library now. ${scanWords}`" title="Scan now" @click="status.startScan()"><Icon name="scan" /></button>
      </div>
      <div class="vscroll win-scroll">
        <template v-if="downloads.busy"><Counters /></template>
        <DownloadDeck v-for="d in downloads.running" :key="d.id" :download="d" />
        <section v-if="!downloads.busy" class="deck idle">
          <div class="deck-top"><span class="led off"></span><h2 class="px">Nothing downloading</h2><span class="stripes"></span></div>
          <p class="t2">Download an album, a track or a playlist from Search, and it shows here while it runs.</p>
          <RouterLink class="key block" to="/search">Go to Search</RouterLink>
        </section>
        <section v-if="downloads.queued.length" class="col g8">
          <h2 class="px sec">Queue · {{ downloads.queued.length }}</h2>
          <QueueShelf />
        </section>
        <section class="col">
          <h2 class="px sec under">History</h2>
          <HistoryList v-model:cursor="cursor" />
          <button v-if="downloads.historyNext !== null" type="button" class="key block more" @click="downloads.loadMore()">Load more</button>
        </section>
        <p v-if="scanWords" class="t3 sm center">{{ scanWords }}</p>
      </div>
    </section>
    <NoticeSlot />
  </div>
  <div v-else class="page">
    <div class="page-main col dl-main">
      <Counters />
      <CurrentDownload v-for="d in downloads.running" :key="d.id" :download="d" />
      <section v-if="!downloads.busy" class="deck idle">
        <div class="deck-top"><span class="led off"></span><h1 class="px">Nothing downloading</h1><span class="stripes"></span></div>
        <p class="t2">Download an album, a track or a playlist from Search, and it shows here while it runs.</p>
      </section>
      <section class="win" aria-label="History">
        <div class="tb"><h2>History</h2><span class="stripes"></span></div>
        <HistoryList v-model:cursor="cursor" :selected="id" />
        <div v-if="downloads.historyNext !== null" class="more-row"><button type="button" class="key" @click="downloads.loadMore()">Load more</button></div>
      </section>
    </div>
    <aside v-if="id || downloads.queued.length || downloads.deskNotice" class="page-side">
      <NoticeSlot />
      <DownloadPanel v-if="id" :id="id" @close="close" />
      <section v-else-if="downloads.queued.length" class="win" aria-label="Queue">
        <div class="tb"><h2>Queue · {{ downloads.queued.length }}</h2><span class="stripes"></span></div>
        <div class="side"><QueueShelf /></div>
      </section>
    </aside>
  </div>
</template>

<style scoped>
.rs .deck { display: flex; flex-direction: column; gap: 12px; padding: 14px; background: repeating-linear-gradient(90deg, rgba(255, 255, 255, .03) 0 1px, transparent 1px 3px), linear-gradient(160deg, color-mix(in srgb, var(--deck) 80%, #fff), var(--deck) 45%); border: 1px solid var(--ink); border-radius: 7px; box-shadow: inset 1px 1px 0 rgba(255, 255, 255, .14), 2px 4px 10px rgba(0, 0, 0, .3); }
.rs .deck-top { display: flex; align-items: center; gap: 10px; color: var(--text-2); }
.rs .deck-top .px, .rs .deck-top > span:not(.led):not(.stripes) { font-family: var(--label); font-size: 13px; font-weight: 700; letter-spacing: .14em; text-transform: uppercase; }
.rs .deck .stripes { background: repeating-linear-gradient(90deg, rgba(0, 0, 0, .55) 0 12px, transparent 12px 17px); }
.rs .deck-keys { display: flex; flex-wrap: wrap; gap: 12px; }
.rs .sec { font-size: 17px; }
.rs .under { padding-bottom: 6px; border-bottom: 2px solid var(--ink); }
.rs .more { margin-top: 12px; }
.rs .more-row { display: flex; justify-content: flex-end; padding: 14px; }
.rs .dl-main { gap: 20px; }
.rs .side { padding: 14px; }
.rs .idle h1, .rs .idle h2 { font-size: 15px; font-weight: 500; }
</style>
