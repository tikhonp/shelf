<script setup lang="ts">
import { computed } from 'vue'
import { useDownloads } from '../stores/downloads'
import { useSettings, type Density } from '../stores/settings'
import Icon from './Icon.vue'
import PartCard from './PartCard.vue'

const settings = useSettings()
const downloads = useDownloads()

const levels = computed<{ id: Density; label: string; about: string }[]>(() => {
  const lists = downloads.enabled ? 'search results, track lists and History' : 'the album list, track lists and History'
  return [
    { id: 'comfortable', label: 'Comfortable', about: `Taller rows and larger covers in ${lists}.` },
    { id: 'compact', label: 'Compact', about: `Shorter rows and smaller covers in ${lists}.` },
  ]
})
const about = computed(() => levels.value.find((l) => l.id === settings.density)?.about)

// Status has no table of its own, so these made-up rows show what the choice does.
const downloadSample = [
  { at: '14:32', title: 'Low Tide Hymns', note: 'Northern Static', source: 'Qobuz', pill: 'done', result: 'Done', took: '1m 48s', size: '612 MB', retry: false },
  { at: '13:40', title: 'Paper Lanterns', note: 'The Quiet Arcade · 11 of 12 tracks', source: 'Apple Music', pill: 'partial', result: 'Done with errors', took: '3m 10s', size: '486 MB', retry: true },
]
const librarySample = [
  { at: '12m ago', title: 'Save', note: 'By Alice · 10 tracks', tags: '4', pill: 'done', state: 'Saved', undo: true },
  { at: '2h ago', title: 'Save: cover', note: 'By Alice', tags: 'Cover', pill: 'done', state: 'Saved', undo: false },
]
</script>

<template>
  <PartCard name="Density" note="In this browser" :lines="[]">
    <div class="col g8 choice">
      <div class="segk block" role="radiogroup" aria-label="Density">
        <button v-for="l in levels" :key="l.id" type="button" role="radio" :aria-checked="settings.density === l.id" @click="settings.density = l.id">{{ l.label }}</button>
      </div>
      <p class="t2 sm">{{ about }}</p>
    </div>
    <div class="col g4 preview" inert>
      <span class="px t3 caption">Preview</span>
      <div v-if="downloads.enabled" class="rows hist-rows sample">
        <div class="row thead g-hist">
          <span>Finished</span><span>Title</span><span class="opt">Source</span><span class="opt">Kind</span><span>Result</span><span class="r opt">Took</span><span class="r opt">Size</span><span></span>
        </div>
        <div v-for="s in downloadSample" :key="s.title" class="row g-hist">
          <span class="t2 num sm">{{ s.at }}</span>
          <span class="two"><span class="ttl small"><span class="ell">{{ s.title }}</span></span><span class="sub ell">{{ s.note }}</span></span>
          <span class="two opt"><span class="src">{{ s.source }}</span><span class="xs t3">Web</span></span>
          <span class="t2 sm opt">Album</span>
          <span><span class="pill" :class="s.pill">{{ s.result }}</span></span>
          <span class="r num t2 sm opt">{{ s.took }}</span>
          <span class="r num t2 sm opt">{{ s.size }}</span>
          <span class="r"><span v-if="s.retry" class="key sm"><Icon name="retry" size="s" />Retry</span></span>
        </div>
      </div>
      <div v-else class="rows sample">
        <div class="row thead g-ah">
          <span>When</span><span>Change</span><span class="r">Tags</span><span>State</span><span></span>
        </div>
        <div v-for="s in librarySample" :key="s.title" class="row g-ah">
          <span class="t2 num sm">{{ s.at }}</span>
          <span class="two"><span class="ttl small"><span class="ell">{{ s.title }}</span></span><span class="sub ell">{{ s.note }}</span></span>
          <span class="num sm r">{{ s.tags }}</span>
          <span><span class="pill" :class="s.pill">{{ s.state }}</span></span>
          <span class="r"><span v-if="s.undo" class="key sm">Undo</span></span>
        </div>
      </div>
    </div>
  </PartCard>
</template>

<style scoped>
.rs .choice { flex: 1 1 320px; }
.rs .preview { flex: 1 1 100%; }
.rs .caption { font-size: 12px; }
.rs .sample { border: 1px solid var(--ink); border-radius: 6px; }
.rs .small { font-size: 14px; }
</style>
