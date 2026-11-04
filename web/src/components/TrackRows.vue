<script setup lang="ts">
import type { Item, Ref, Track } from '../api/types'
import { duration } from '../lib/format'
import { usePhone } from '../lib/viewport'
import ItemAction from './ItemAction.vue'
import ItemTitle from './ItemTitle.vue'
import Tapes from './Tapes.vue'

const props = defineProps<{ item: Item }>()
const phone = usePhone()
const trackRef = (t: Track): Ref => ({ source: props.item.ref.source, kind: 'track', id: t.id })
const playlist = () => props.item.ref.kind === 'playlist'
</script>

<template>
  <div class="rows" :class="{ boxed: !phone }" role="table" :aria-label="`Tracks of ${item.title}`">
    <div v-for="t in item.tracks ?? []" :key="t.id" class="row" :class="phone ? 'g-trk-ph' : 'g-trk'" role="row">
      <span role="cell" class="t3 num r sm">{{ t.position }}</span>
      <span role="cell" class="two">
        <span class="line g8 name"><ItemTitle :title="t.title" :version="t.version" :explicit="t.explicit" /></span>
        <span v-if="playlist() && t.artist" class="sub ell">{{ t.artist }}</span>
      </span>
      <span role="cell" class="t2 num r sm">{{ duration(t.duration) }}</span>
      <span v-if="!phone" role="cell"><Tapes :quality="t.quality" /></span>
      <span role="cell"><ItemAction :item-ref="trackRef(t)" :title="t.title" :status="t.status" :cover="item.cover_url" :layout="phone ? 'narrow' : 'track'" /></span>
    </div>
  </div>
</template>

<style scoped>
.rs .boxed { overflow: hidden; border: 2px solid var(--ink); border-radius: 10px; }
.rs .name { font-size: 15px; }
.rs .g-trk-ph { grid-template-columns: 24px minmax(0, 1fr) 44px 46px; min-height: 56px; padding: 4px 0 4px 2px; }
</style>
