<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { Download } from '../api/types'
import { useNow } from '../lib/clock'
import { elapsed } from '../lib/format'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import Cover from './Cover.vue'
import Icon from './Icon.vue'
import Meter from './Meter.vue'

const props = defineProps<{ download: Download }>()
const downloads = useDownloads()
const sources = useSources()
const now = useNow()

const d = computed(() => props.download)
const current = computed(() => downloads.currents[d.value.id] ?? '')
const seconds = computed(() => (d.value.started_at ? (now.value - Date.parse(d.value.started_at)) / 1000 : 0))
const two = (n: number) => String(n).padStart(2, '0')
const summary = computed(() => `${d.value.tracks_done} of ${d.value.tracks_total} tracks done`)
</script>

<template>
  <section class="deck" aria-label="Downloading now">
    <div class="deck-top"><span class="led rec blink"></span><h2 class="px">Downloading now</h2><span class="stripes"></span></div>
    <RouterLink class="line g12 plain" :to="`/downloads/${d.id}`">
      <Cover :url="d.cover_url" :size="56" />
      <span class="two grow">
        <span class="ttl"><span class="ell">{{ d.title || 'Reading the item' }}</span></span>
        <span class="sub line g8"><span class="ell">{{ d.artist }}</span><span class="src">{{ sources.name(d.source) }}</span></span>
      </span>
      <Icon name="next" />
    </RouterLink>
    <div class="lcd" role="img" :aria-label="`${summary}, ${current ? `now ${current}, ` : ''}${elapsed(seconds)} elapsed`">
      <div class="readout">
        <span class="lcd-big num">{{ two(d.tracks_done) }}/{{ two(d.tracks_total) }}</span>
        <span class="lcd-l">TRACKS DONE</span>
        <span class="lcd-l num time">{{ elapsed(seconds) }}</span>
      </div>
      <div v-if="current" class="lcd-l ell">NOW: {{ current.toUpperCase() }}</div>
    </div>
    <div v-if="d.tracks_total" class="line g10">
      <span class="px t2 tiny">Tracks</span>
      <Meter class="grow" :done="d.tracks_done" :failed="d.tracks_failed" :total="d.tracks_total" running :label="summary" />
    </div>
    <div class="deck-keys">
      <RouterLink class="key grow" :to="`/downloads/${d.id}`">Details</RouterLink>
      <button type="button" class="key grow" @click="downloads.cancel(d.id)"><Icon name="close" />Cancel</button>
    </div>
  </section>
</template>

<style scoped>
.rs .deck { display: flex; flex-direction: column; gap: 12px; padding: 14px; background: repeating-linear-gradient(90deg, rgba(255, 255, 255, .03) 0 1px, transparent 1px 3px), linear-gradient(160deg, color-mix(in srgb, var(--deck) 80%, #fff), var(--deck) 45%); border: 1px solid var(--ink); border-radius: 7px; box-shadow: inset 1px 1px 0 rgba(255, 255, 255, .14), 2px 4px 10px rgba(0, 0, 0, .3); }
.rs .deck-top { display: flex; align-items: center; gap: 10px; color: var(--text-2); }
.rs .deck-top .px, .rs .deck-top > span:not(.led):not(.stripes) { font-family: var(--label); font-size: 13px; font-weight: 700; letter-spacing: .14em; text-transform: uppercase; }
.rs .deck .stripes { background: repeating-linear-gradient(90deg, rgba(0, 0, 0, .55) 0 12px, transparent 12px 17px); }
.rs .deck-keys { display: flex; flex-wrap: wrap; gap: 12px; }
.rs .deck-top h2 { font-size: 13px; }
.rs .lcd { flex: none; }
.rs .readout { display: flex; align-items: baseline; gap: 10px; }
.rs .readout .lcd-big { font-size: 26px; }
.rs .time { margin-left: auto; }
.rs .tiny { font-size: 11px; }
</style>
