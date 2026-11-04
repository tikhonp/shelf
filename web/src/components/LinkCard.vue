<script setup lang="ts">
import { computed } from 'vue'
import type { Item } from '../api/types'
import { count, duration, kindWord } from '../lib/format'
import { tapes } from '../lib/quality'
import { usePhone } from '../lib/viewport'
import { useSources } from '../stores/sources'
import Cover from './Cover.vue'
import ReleaseActions from './ReleaseActions.vue'
import TrackRows from './TrackRows.vue'

const props = defineProps<{ item: Item }>()
const emit = defineEmits<{ open: [Item]; artist: [string] }>()

const phone = usePhone()
const sources = useSources()
const noun = computed(() => kindWord(props.item.ref.kind).toLowerCase())
const article = computed(() => (/^[aeiou]/.test(noun.value) ? 'an' : 'a'))
const group = computed(() => props.item.ref.kind === 'artist' || props.item.ref.kind === 'label')
const list = computed(() => tapes(props.item.quality))
const facts = computed(() => {
  const it = props.item
  const n = it.tracks?.length || it.track_count
  return [it.year ? String(it.year) : '', n ? count(n, 'track') : '', duration(it.duration), it.label].filter(Boolean).join(' · ')
})
</script>

<template>
  <div class="col g14 card">
    <p class="t2 sm line g8">The link points to {{ article }} {{ noun }} on <span class="src">{{ sources.name(item.ref.source) }}</span></p>
    <div class="body">
      <div class="art">
        <Cover :url="item.cover_url" :size="phone ? 188 : 200" :round="group" big />
        <span v-for="(t, i) in list" :key="t.text" class="stk" :class="[t.cls, i === 0 ? 'one' : 'two-tape']">{{ t.text }}</span>
      </div>
      <div class="col g14 info">
        <div class="two g4 words">
          <h2 class="name">{{ item.title }}</h2>
          <button v-if="item.artist" type="button" class="link by" @click="emit('artist', item.artist)">{{ item.artist }}</button>
          <span class="sub num">{{ facts }}</span>
        </div>
        <button v-if="group" type="button" class="key sun block" @click="emit('open', item)">Open the {{ noun }}</button>
        <template v-else>
          <ReleaseActions :item="item" />
          <button v-if="phone && item.tracks?.length" type="button" class="key block" @click="emit('open', item)">Show the tracks</button>
        </template>
      </div>
    </div>
    <TrackRows v-if="!phone && item.tracks?.length" :item="item" />
  </div>
</template>

<style scoped>
.rs .body { display: flex; flex-wrap: wrap; gap: 30px; align-items: flex-start; }
.rs .art { position: relative; flex: none; margin: 4px 12px 0 6px; }
.rs .info { flex: 1 1 340px; }
.rs .name { font-size: 24px; font-weight: 700; line-height: 1.2; }
.rs .by { font-size: 15px; font-weight: 600; text-align: left; }
.rs .stk.one { position: absolute; left: -14px; top: 14px; height: 26px; transform: rotate(-8deg); }
.rs .stk.two-tape { position: absolute; right: -16px; bottom: 18px; height: 26px; transform: rotate(5deg); }
@media (max-width: 719px) {
  .rs .body { flex-direction: column; align-items: stretch; gap: 14px; }
  .rs .art { align-self: center; margin: 8px 0 0; }
  .rs .info { flex: none; }
  .rs .words { align-items: center; text-align: center; }
  .rs .name { font-size: 23px; }
}
</style>
