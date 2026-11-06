<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import type { Download, DownloadStatus } from '../api/types'
import { useNow } from '../lib/clock'
import { finishedAt, kindWord, originWord, resultWord, size, took } from '../lib/format'
import { usePhone } from '../lib/viewport'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import Cover from './Cover.vue'
import Icon from './Icon.vue'

const props = defineProps<{ selected?: number; cursor: number }>()
const emit = defineEmits<{ 'update:cursor': [number] }>()

const phone = usePhone()
const now = useNow()
const router = useRouter()
const downloads = useDownloads()
const sources = useSources()
const root = ref<HTMLElement>()
const expanded = ref<number[]>([])

const pills: Record<DownloadStatus, string> = { queued: 'queued', running: 'run', done: 'done', partial: 'partial', failed: 'failed', canceled: 'canceled', skipped: 'skipped' }
const retryable = (d: Download) => d.status === 'failed' || d.status === 'partial' || d.status === 'canceled'
const isGroup = (d: Download) => d.kind === 'artist' || d.kind === 'label'
const open = (d: Download) => void router.push(`/downloads/${d.id}`)

// note is the second line: why it failed, how far it got, or who made it.
function note(d: Download): string {
  if (d.status === 'partial') return [d.artist, `${d.tracks_done} of ${d.tracks_total} tracks`].filter(Boolean).join(' · ')
  if (d.status === 'skipped') return 'Already downloaded'
  return d.error || d.artist
}

async function toggle(d: Download) {
  if (expanded.value.includes(d.id)) {
    expanded.value = expanded.value.filter((x) => x !== d.id)
    downloads.closeDetail(d.id)
    return
  }
  expanded.value = [...expanded.value, d.id]
  await downloads.loadDetail(d.id)
}

const rows = computed(() => downloads.history)
watch(
  () => props.cursor,
  () => void nextTick(() => root.value?.querySelector('.row.cur')?.scrollIntoView?.({ block: 'nearest' })),
)
</script>

<template>
  <div v-if="phone" ref="root" class="col">
    <div v-for="d in rows" :key="d.id" class="row ph-row">
      <Cover :url="d.cover_url" :size="44" />
      <RouterLink class="two plain" :to="`/downloads/${d.id}`">
        <span class="ttl small"><span class="ell">{{ d.title || d.url }}</span></span>
        <span class="sub ell">{{ [d.artist, sources.name(d.source), finishedAt(d.finished_at, now), `By ${d.author || 'Unknown'}`].filter(Boolean).join(' · ') }}</span>
        <span class="line g8 extra">
          <span class="pill" :class="pills[d.status]">{{ resultWord(d.status) }}</span>
          <span class="xs t2 ell">{{ d.status === 'failed' ? d.error : d.status === 'partial' ? `${d.tracks_done} of ${d.tracks_total}` : originWord(d.origin, d.origin_user) }}</span>
        </span>
      </RouterLink>
      <button v-if="retryable(d)" type="button" class="key icon" :aria-label="`Retry ${d.title}`" @click="downloads.retry(d.id)"><Icon name="retry" /></button>
    </div>
  </div>

  <div v-else ref="root" class="rows hist-rows" role="table" aria-label="History">
    <div class="row thead g-hist" role="row">
      <span role="columnheader">Finished</span><span role="columnheader">Title</span><span role="columnheader" class="opt">Source</span><span role="columnheader" class="opt">Kind</span>
      <span role="columnheader">Result</span><span role="columnheader" class="r opt">Took</span><span role="columnheader" class="r opt">Size</span><span role="columnheader"><span class="sr">Actions</span></span>
    </div>
    <template v-for="(d, i) in rows" :key="d.id">
      <div class="row g-hist click" :class="{ cur: i === cursor || d.id === selected }" role="row" @click="emit('update:cursor', i); open(d)">
        <span role="cell" class="t2 num sm">{{ finishedAt(d.finished_at, now) }}</span>
        <span role="cell" class="two">
          <span class="ttl small"><span class="ell">{{ d.title || d.url }}</span></span>
          <span class="sub ell" :class="{ 'bad-text': d.status === 'failed' }">{{ note(d) }}</span>
        </span>
        <span role="cell" class="two opt"><span class="src">{{ sources.name(d.source) }}</span><span class="xs t3">{{ `${originWord(d.origin, d.origin_user)} · By ${d.author || 'Unknown'}` }}</span></span>
        <span role="cell" class="t2 sm opt">{{ kindWord(d.kind) }}</span>
        <span role="cell"><span class="pill" :class="pills[d.status]">{{ resultWord(d.status) }}</span></span>
        <span role="cell" class="r num t2 sm opt">{{ took(d.started_at, d.finished_at) }}</span>
        <span role="cell" class="r num t2 sm opt">{{ size(d.bytes) }}</span>
        <span role="cell" class="r line g4 acts">
          <button v-if="isGroup(d)" type="button" class="key sm icon flat" :aria-expanded="expanded.includes(d.id)" :aria-label="`Albums of ${d.title}`" @click.stop="toggle(d)"><Icon name="chevron" size="s" /></button>
          <button v-if="retryable(d)" type="button" class="key sm" @click.stop="downloads.retry(d.id)"><Icon name="retry" size="s" />Retry</button>
        </span>
      </div>
      <template v-if="expanded.includes(d.id)">
        <RouterLink v-for="c in downloads.details[d.id]?.children ?? []" :key="c.id" class="row child plain" :to="`/downloads/${c.id}`">
          <span class="ell">{{ c.title || c.url }}</span>
          <span class="pill" :class="pills[c.status]">{{ resultWord(c.status) }}</span>
        </RouterLink>
      </template>
    </template>
  </div>
</template>

<style scoped>
.rs .click { cursor: pointer; }
.rs .small { font-size: 14px; }
.rs .ph-row { grid-template-columns: 44px minmax(0, 1fr) auto; align-items: start; padding: 10px 0; }
.rs .extra { margin-top: 3px; }
.rs .acts { justify-content: flex-end; }
.rs .child { display: flex; justify-content: space-between; gap: 12px; min-height: 44px; padding-left: 84px; }
</style>
