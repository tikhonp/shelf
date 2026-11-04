<script setup lang="ts">
import { computed } from 'vue'
import { useSettings, type Density } from '../stores/settings'
import Icon from './Icon.vue'
import PartCard from './PartCard.vue'

const settings = useSettings()

const levels: { id: Density; label: string; about: string }[] = [
  { id: 'comfortable', label: 'Comfortable', about: 'Taller rows and larger covers in search results, track lists and History.' },
  { id: 'compact', label: 'Compact', about: 'Shorter rows and smaller covers in search results, track lists and History.' },
]
const about = computed(() => levels.find((l) => l.id === settings.density)?.about)

// Status has no table of its own, so these made-up rows show what the choice does.
const sample = [
  { at: '14:32', title: 'Low Tide Hymns', note: 'Northern Static', source: 'Qobuz', pill: 'done', result: 'Done', took: '1m 48s', size: '612 MB', retry: false },
  { at: '13:40', title: 'Paper Lanterns', note: 'The Quiet Arcade · 11 of 12 tracks', source: 'Apple Music', pill: 'partial', result: 'Done with errors', took: '3m 10s', size: '486 MB', retry: true },
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
      <div class="rows hist-rows sample">
        <div class="row thead g-hist">
          <span>Finished</span><span>Title</span><span class="opt">Source</span><span class="opt">Kind</span><span>Result</span><span class="r opt">Took</span><span class="r opt">Size</span><span></span>
        </div>
        <div v-for="s in sample" :key="s.title" class="row g-hist">
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
