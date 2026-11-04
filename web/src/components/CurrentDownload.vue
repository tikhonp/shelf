<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { Download } from '../api/types'
import { useNow } from '../lib/clock'
import { elapsed, originWord } from '../lib/format'
import { percent } from '../lib/lamps'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import Icon from './Icon.vue'
import Lathe from './Lathe.vue'

const props = defineProps<{ download: Download }>()
const downloads = useDownloads()
const sources = useSources()
const now = useNow()

const d = computed(() => props.download)
const two = (n: number) => String(n).padStart(2, '0')
const current = computed(() => downloads.currents[d.value.id] ?? '')
const seconds = computed(() => (d.value.started_at ? (now.value - Date.parse(d.value.started_at)) / 1000 : 0))
const progress = computed(() => percent(d.value && { tracks_done: d.value.tracks_done, tracks_total: d.value.tracks_total }) / 100)
</script>

<template>
  <section class="win">
    <div class="tb"><h1>Current download</h1><span class="stripes"></span><span class="led rec blink" title="Downloading"></span></div>
    <div class="body">
      <div class="disc"><Lathe :size="170" :progress="progress" :cover="d.cover_url" spin /></div>
      <div class="col g14 facts">
        <div class="two g4">
          <span class="ttl big"><span class="ell">{{ d.title || 'Reading the item' }}</span></span>
          <span class="sub line g12 wrap">{{ d.artist }} <span class="src">{{ sources.name(d.source) }}</span> {{ sources.formatLabel(d.source, d.format) }} · asked by {{ originWord(d.origin, d.origin_user) }}</span>
        </div>
        <div class="lcd tight" role="img" :aria-label="`${d.tracks_done} of ${d.tracks_total} tracks done, ${elapsed(seconds)} elapsed`">
          <div class="readout"><span class="lcd-big num">{{ two(d.tracks_done) }}/{{ two(d.tracks_total) }}</span><span class="lcd-l">TRACKS DONE</span><span class="lcd-l num time">{{ elapsed(seconds) }} ELAPSED</span></div>
          <div v-if="current" class="lcd-l ell">NOW: {{ current.toUpperCase() }}</div>
        </div>
        <div class="line g12">
          <RouterLink class="key" :to="`/downloads/${d.id}`">Details</RouterLink>
          <button type="button" class="key" @click="downloads.cancel(d.id)"><Icon name="close" />Cancel</button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.rs .body { display: flex; flex-wrap: wrap; gap: 18px; padding: 16px; }
.rs .disc { padding: 8px 0 8px 8px; }
.rs .facts { flex: 1 1 340px; justify-content: center; }
.rs .big { font-size: 20px; }
.rs .wrap { flex-wrap: wrap; }
.rs .readout { display: flex; align-items: baseline; gap: 10px; }
.rs .time { margin-left: auto; }
</style>
