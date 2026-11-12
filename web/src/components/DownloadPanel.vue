<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { errorText, get } from '../api/client'
import type { DownloadStatus, DownloadTrack, LibraryPage } from '../api/types'
import { useNow } from '../lib/clock'
import { clockSeconds, elapsed, originWord, resultWord, size, took } from '../lib/format'
import { percent } from '../lib/lamps'
import { short } from '../lib/quality'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import Lathe from './Lathe.vue'
import LogView from './LogView.vue'

const props = defineProps<{ id: number }>()
const emit = defineEmits<{ close: [] }>()

const phone = usePhone()
const now = useNow()
const downloads = useDownloads()
const sources = useSources()
const error = ref('')
const router = useRouter()

// The index reads a download's folder at its next walk, so a fresh one may not
// be there yet.
async function openInLibrary(folder: string) {
  error.value = ''
  try {
    const page = await get<LibraryPage>(`/api/library/albums?limit=1&folder=${encodeURIComponent(folder)}`)
    const album = page.albums[0]
    if (album) await router.push({ name: 'album', params: { id: album.id } })
    else error.value = "The Library hasn't read this folder yet."
  } catch (err) {
    error.value = errorText(err)
  }
}

async function load(id: number) {
  error.value = ''
  try {
    await downloads.loadDetail(id)
  } catch (err) {
    error.value = errorText(err)
  }
}
watch(
  () => props.id,
  (id, old) => {
    if (old) downloads.closeDetail(old)
    void load(id)
  },
  { immediate: true },
)
onBeforeUnmount(() => downloads.closeDetail(props.id))

const detail = computed(() => downloads.details[props.id])
// The list has the newest counts between two reads of the detail.
const d = computed(() => (detail.value ? { ...detail.value, ...downloads.items[props.id] } : undefined))
const active = computed(() => d.value?.status === 'running' || d.value?.status === 'queued')
const retryable = computed(() => d.value?.status === 'failed' || d.value?.status === 'partial' || d.value?.status === 'canceled')
const seconds = computed(() => (d.value?.started_at ? (now.value - Date.parse(d.value.started_at)) / 1000 : 0))
const two = (n: number) => String(n).padStart(2, '0')
const progress = computed(() => percent(d.value) / 100)
const delivered = computed(() => {
  const t = detail.value?.tracks.find((x) => x.codec)
  return t ? short(t.codec, t.bit_depth, t.sample_rate) : '—'
})

const pills: Record<DownloadStatus, string> = { queued: 'queued', running: 'run', done: 'done', partial: 'partial', failed: 'failed', canceled: 'canceled', skipped: 'skipped' }

const pillText = computed(() => {
  const v = d.value
  if (!v) return ''
  return v.status === 'partial' ? `${resultWord(v.status)} · ${v.tracks_done} of ${v.tracks_total}` : resultWord(v.status)
})

const fileName = (t: DownloadTrack) => t.path.split('/').pop() || t.title

// A pending track of a download that ended was never fetched, which isn't a failure.
function trackState(t: DownloadTrack): { led: string; text: string } {
  switch (t.status) {
    case 'done':
      return { led: 'ok', text: 'Done' }
    case 'skipped':
      return { led: 'ok', text: 'Skipped' }
    case 'failed':
      return { led: 'bad', text: 'Failed' }
    case 'running':
      return { led: 'run blink', text: 'Downloading' }
    default:
      return { led: 'off', text: active.value ? 'Queued' : 'Not fetched' }
  }
}
</script>

<template>
  <section class="win">
    <div class="tb">
      <button v-if="phone" type="button" class="wbtn" aria-label="Back to Downloads" @click="emit('close')"><Icon name="back" /></button>
      <component :is="phone ? 'h1' : 'h2'" class="num">Download {{ id }}</component>
      <span class="stripes"></span>
      <a v-if="d?.url" class="wbtn" :href="d.url" target="_blank" rel="noreferrer" :aria-label="`Open on ${sources.name(d.source)}`"><Icon name="open" /></a>
      <button v-if="!phone" type="button" class="wbtn" aria-label="Close" @click="emit('close')"><Icon name="close" /></button>
    </div>
    <div class="vscroll body">
      <div v-if="error" class="note"><span class="led bad"></span><p>{{ error }}</p></div>
      <template v-else-if="d">
        <div class="line g14">
          <Cover :url="d.cover_url" :size="64" />
          <div class="two g4 grow">
            <span class="ttl big"><span class="ell">{{ d.title || 'Reading the item' }}</span></span>
            <span class="sub">{{ d.artist }}</span>
            <span><span class="pill" :class="pills[d.status]">{{ pillText }}</span></span>
          </div>
          <button v-if="retryable" type="button" class="key sun" @click="downloads.retry(d.id)"><Icon name="retry" />Retry</button>
        </div>

        <section v-if="active" class="deck" aria-label="Progress">
          <div class="deck-top"><span class="led rec blink"></span><span class="px">{{ resultWord(d.status) }}</span><span class="stripes"></span></div>
          <div class="line g14 spin-row">
            <Lathe :size="phone ? 150 : 170" :progress="progress" :cover="d.cover_url" :spin="d.status === 'running'" />
            <div class="lcd read" role="img" :aria-label="`${d.tracks_done} of ${d.tracks_total} tracks done, ${elapsed(seconds)} elapsed`">
              <div class="lcd-big num">{{ two(d.tracks_done) }}/{{ two(d.tracks_total) }}</div>
              <div class="lcd-l">TRACKS DONE</div>
              <div class="lcd-big num time">{{ elapsed(seconds) }}</div>
              <div class="lcd-l">ELAPSED</div>
            </div>
          </div>
          <button type="button" class="key block" @click="downloads.cancel(d.id)"><Icon name="close" />Cancel download</button>
        </section>

        <dl class="meta panel" :class="{ wide: !phone }">
          <dt>Source</dt><dd><span class="src">{{ sources.name(d.source) }}</span></dd>
          <dt>Asked by</dt><dd>{{ d.author || 'Unknown' }} · {{ originWord(d.origin, d.origin_user) }}</dd>
          <dt>Format</dt><dd>{{ sources.formatLabel(d.source, d.format) }}</dd>
          <dt>Started</dt><dd class="num">{{ clockSeconds(d.started_at) }}</dd>
          <dt>Delivered</dt><dd class="num">{{ delivered }}</dd>
          <dt>Finished</dt><dd class="num">{{ clockSeconds(d.finished_at) }}</dd>
          <dt>Size</dt><dd class="num">{{ size(d.bytes) }}</dd>
          <dt>Took</dt><dd class="num">{{ took(d.started_at, d.finished_at) }}</dd>
        </dl>

        <div v-if="d.error" class="note" role="status"><span class="led bad"></span><p>{{ d.error }}</p></div>

        <div v-if="d.folder || d.provenance_path" class="panel col g8">
          <template v-if="d.folder"><span class="px t3 small-h">Release folder</span><code class="mono path">{{ d.folder }}</code></template>
          <template v-if="d.provenance_path"><span class="px t3 small-h">Provenance file</span><code class="mono path">{{ d.provenance_path }}</code></template>
          <button v-if="d.folder && (d.status === 'done' || d.status === 'partial')" type="button" class="key sm" @click="openInLibrary(d.folder)">Open in Library</button>
        </div>

        <section v-if="d.children.length" class="col">
          <div class="sec-h"><h3 class="px">Albums</h3><span class="t3 sm num">{{ d.children.length }}</span></div>
          <RouterLink v-for="c in d.children" :key="c.id" class="row kid plain" :to="`/downloads/${c.id}`">
            <Cover :url="c.cover_url" :size="36" />
            <span class="ell">{{ c.title || c.url }}</span>
            <span class="pill" :class="pills[c.status]">{{ resultWord(c.status) }}</span>
          </RouterLink>
        </section>

        <section v-if="d.tracks.length" class="col">
          <div class="sec-h"><h3 class="px">Tracks</h3><span class="t3 sm num">{{ d.tracks.length }}</span></div>
          <div class="rows" role="table" aria-label="Tracks">
            <div v-for="t in d.tracks" :key="t.position" class="row trk" role="row">
              <span role="cell" class="t3 num r xs">{{ t.position }}</span>
              <span role="cell" class="ell sm" :title="t.path">{{ fileName(t) }}</span>
              <span role="cell" class="st"><span class="led" :class="trackState(t).led"></span>{{ trackState(t).text }}</span>
              <span v-if="t.error" class="xs bad-text why">{{ t.error }}</span>
            </div>
          </div>
        </section>

        <LogView :download-id="id" />
      </template>
      <div v-else class="col g12" aria-busy="true"><span class="sr">Loading</span><span class="sk"></span><span class="sk"></span></div>
    </div>
  </section>
</template>

<style scoped>
.rs .deck { display: flex; flex-direction: column; gap: 12px; padding: 14px; background: repeating-linear-gradient(90deg, rgba(255, 255, 255, .03) 0 1px, transparent 1px 3px), linear-gradient(160deg, color-mix(in srgb, var(--deck) 80%, #fff), var(--deck) 45%); border: 1px solid var(--ink); border-radius: 7px; box-shadow: inset 1px 1px 0 rgba(255, 255, 255, .14), 2px 4px 10px rgba(0, 0, 0, .3); }
.rs .deck-top { display: flex; align-items: center; gap: 10px; color: var(--text-2); }
.rs .deck-top .px, .rs .deck-top > span:not(.led):not(.stripes) { font-family: var(--label); font-size: 13px; font-weight: 700; letter-spacing: .14em; text-transform: uppercase; }
.rs .deck .stripes { background: repeating-linear-gradient(90deg, rgba(0, 0, 0, .55) 0 12px, transparent 12px 17px); }
.rs .deck-keys { display: flex; flex-wrap: wrap; gap: 12px; }
.rs .body { gap: 14px; padding: 14px 12px; }
.rs .big { font-size: 17px; }
.rs .spin-row { padding: 6px 0 4px 4px; }
.rs .read { align-self: stretch; justify-content: center; gap: 2px; }
.rs .read .lcd-big { font-size: 32px; }
.rs .read .time { margin-top: 10px; font-size: 22px; }
.rs .meta.wide { grid-template-columns: max-content minmax(0, 1fr) max-content minmax(0, 1fr); }
.rs .path { word-break: break-all; }
.rs .note > .led { margin-top: 4px; }
.rs .trk { grid-template-columns: 22px minmax(0, 1fr) auto; min-height: 38px; padding: 4px 10px; row-gap: 2px; }
.rs.compact .row.trk { min-height: 36px; padding-top: 2px; padding-bottom: 2px; }
.rs .why { grid-column: 2 / -1; }
.rs .kid { grid-template-columns: 36px minmax(0, 1fr) auto; min-height: 48px; padding: 4px 2px; }
.rs.compact .kid { grid-template-columns: 30px minmax(0, 1fr) auto; }
</style>
