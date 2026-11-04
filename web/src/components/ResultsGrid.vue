<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { Item, SearchType } from '../api/types'
import { duration } from '../lib/format'
import { downloadable, itemKey, rowLine } from '../lib/rows'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import ItemAction from './ItemAction.vue'
import ItemTitle from './ItemTitle.vue'
import StateLine from './StateLine.vue'
import Tapes from './Tapes.vue'

const props = defineProps<{ items: Item[]; type: SearchType; label: string; narrow: boolean; cursor: number }>()
const emit = defineEmits<{ open: [Item]; 'update:cursor': [number] }>()

const sources = useSources()
const downloads = useDownloads()
const root = ref<HTMLElement>()
// The full grid needs this much room, which a tablet or an open panel may not leave.
const fullWidth = 900
const tight = ref(false)
const compact = computed(() => props.narrow || tight.value)
const grid = computed(() => (compact.value ? 'g-res-n' : 'g-res'))
let watcher: ResizeObserver | undefined
onMounted(() => {
  if (!root.value || typeof ResizeObserver === 'undefined') return
  watcher = new ResizeObserver(([e]) => (tight.value = (e?.contentRect.width ?? fullWidth) < fullWidth))
  watcher.observe(root.value)
})
onBeforeUnmount(() => watcher?.disconnect())
const heading = computed(() => ({ album: 'Album', track: 'Track', artist: 'Artist', playlist: 'Playlist' })[props.type])

watch(
  () => props.cursor,
  () => void nextTick(() => root.value?.querySelector('.row.cur')?.scrollIntoView?.({ block: 'nearest' })),
)
</script>

<template>
  <div ref="root" class="rows" role="grid" :aria-label="label">
    <div class="row thead" :class="grid" role="row">
      <span role="columnheader"><span class="sr">Cover</span></span>
      <span role="columnheader">{{ heading }}</span>
      <span role="columnheader" class="r w">{{ type === 'artist' ? '' : type === 'track' ? '' : 'Tracks' }}</span>
      <span role="columnheader" class="r w">{{ type === 'artist' ? '' : 'Length' }}</span>
      <span role="columnheader">{{ type === 'artist' ? '' : 'Quality' }}</span>
      <span role="columnheader">Status</span>
      <span role="columnheader"><span class="sr">Download</span></span>
      <span role="columnheader" class="w"><span class="sr">Link</span></span>
    </div>
    <div v-for="(it, i) in items" :key="itemKey(it)" class="row" :class="[grid, { cur: i === cursor }]" role="row" :aria-selected="i === cursor" @click="emit('update:cursor', i)">
      <span role="gridcell"><Cover :url="it.cover_url" :size="46" :round="it.ref.kind === 'artist'" /></span>
      <span role="gridcell" class="two">
        <button type="button" class="ttl ttl-btn" @click="emit('open', it)"><ItemTitle :title="it.title" :version="it.version" :explicit="it.explicit" /></button>
        <span class="sub line g10"><span class="ell">{{ rowLine(it, true) }}</span><span class="src">{{ sources.name(it.ref.source) }}</span></span>
      </span>
      <span role="gridcell" class="r num t2 w">{{ it.ref.kind === 'album' || it.ref.kind === 'playlist' ? it.track_count : '' }}</span>
      <span role="gridcell" class="r num t2 w">{{ duration(it.duration) }}</span>
      <span role="gridcell"><Tapes :quality="it.quality" /></span>
      <span role="gridcell"><StateLine :status="downloads.liveStatus(it.ref, it.status)" /></span>
      <span role="gridcell">
        <ItemAction v-if="downloadable(it)" :item-ref="it.ref" :title="it.title" :status="it.status" :cover="it.cover_url" :layout="compact ? 'narrow' : 'wide'" />
        <button v-else type="button" class="key sm" :aria-label="`Open ${it.title}`" @click="emit('open', it)">Open</button>
      </span>
      <span role="gridcell" class="w"><a v-if="it.url" class="key sm icon flat" :href="it.url" target="_blank" rel="noreferrer" :aria-label="`Open on ${sources.name(it.ref.source)}`"><Icon name="open" size="s" /></a></span>
    </div>
  </div>
</template>

<style scoped>
.rs .rows { overflow-x: auto; }
.rs .g-res-n { grid-template-columns: 46px minmax(140px, 1fr) 130px 124px 46px; }
.rs.compact .g-res-n { grid-template-columns: 30px minmax(140px, 1fr) 130px 124px 46px; }
</style>
