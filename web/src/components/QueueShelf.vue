<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { useDownloads } from '../stores/downloads'
import { useSources } from '../stores/sources'
import Cover from './Cover.vue'
import Icon from './Icon.vue'

const downloads = useDownloads()
const sources = useSources()
const two = (n: number) => String(n).padStart(2, '0')
</script>

<template>
  <div class="rack">
    <div v-for="(d, i) in downloads.queued" :key="d.id" class="spine">
      <Cover class="band" :url="d.cover_url" :size="56" />
      <span class="no">{{ two(i + 1) }}</span>
      <RouterLink class="two plain" :to="`/downloads/${d.id}`">
        <span class="ttl"><span class="ell">{{ d.title || 'Reading the item' }}</span></span>
        <span class="sub ell">{{ [d.artist, sources.name(d.source), sources.formatLabel(d.source, d.format)].filter(Boolean).join(' · ') }}</span>
      </RouterLink>
      <button type="button" class="key icon sm" :aria-label="`Cancel ${d.title}`" @click="downloads.cancel(d.id)"><Icon name="close" /></button>
    </div>
  </div>
</template>

<style scoped>
.rs .rack { display: flex; flex-direction: column; gap: 6px; }
.rs .spine { display: grid; grid-template-columns: 14px 30px minmax(0, 1fr) auto; align-items: center; column-gap: 10px; min-height: 56px; padding-right: 8px; overflow: hidden; background: repeating-linear-gradient(90deg, rgba(0, 0, 0, .03) 0 2px, transparent 2px 7px), linear-gradient(180deg, #D3B485, var(--box)); border: 1px solid var(--ink); border-radius: 3px; box-shadow: inset 1px 1px 0 rgba(255, 255, 255, .35), 2px 3px 5px rgba(0, 0, 0, .35); color: var(--box-ink); }
.rs .spine .band { align-self: stretch; width: 14px; height: auto; border: 0; border-right: 1px solid var(--ink); border-radius: 0; box-shadow: none; }
.rs .spine .no { font: 400 18px var(--disp); color: var(--box-ink); text-align: center; }
.rs .spine .two { padding: 4px 10px; background: var(--tape); clip-path: polygon(0 6%, 4% 0, 60% 4%, 100% 0, 98% 50%, 100% 100%, 40% 96%, 3% 100%, 1% 50%); }
.rs .spine .ttl, .rs .spine .sub { color: var(--box-ink); }
.rs .spine .key { --key-hl: rgba(255, 255, 255, .3); }
.rs .spine .ttl { font-size: 14px; }
.rs .spine .key.sm { width: 44px; height: 44px; }
</style>
