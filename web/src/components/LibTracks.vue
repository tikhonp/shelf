<script setup lang="ts">
import { computed } from 'vue'
import type { LibraryTrack } from '../api/types'
import { duration } from '../lib/format'
import { usePhone } from '../lib/viewport'
import { useEditor } from '../stores/editor'
import { usePlayer } from '../stores/player'
import Icon from './Icon.vue'

const phone = usePhone()
const editor = useEditor()
const player = usePlayer()
const total = computed(() => editor.tracks.reduce((n, t) => n + t.duration, 0))
const isPlaying = (t: LibraryTrack) => player.track?.id === t.id && player.playing
const hasStaged = (t: LibraryTrack) => !!editor.stage[t.id]
</script>

<template>
  <div class="lib-tracks rows" role="grid" aria-label="Tracks">
    <div class="row g-lt all" :class="{ cur: editor.trackId === null }" role="row">
      <span></span><span></span>
      <button type="button" class="lib-open ttl" @click="editor.trackId = null"><span class="ell">All {{ editor.tracks.length }} tracks</span></button>
      <span class="t2 num sm r">{{ duration(total) }}</span>
    </div>
    <div v-for="t in editor.tracks" :key="t.id" class="row g-lt" :class="{ cur: editor.trackId === t.id }" role="row">
      <button type="button" class="key flat icon" :class="{ sm: !phone }" :disabled="!!t.error" :aria-label="`${isPlaying(t) ? 'Pause' : 'Play'} ${t.title}`" @click="player.toggle(t)"><Icon :name="isPlaying(t) ? 'pause' : 'play'" /></button>
      <span class="t3 num sm r">{{ t.track || '' }}</span>
      <button type="button" class="lib-open ttl" :disabled="!!t.error" @click="editor.trackId = t.id"><span class="ell">{{ t.title || t.path.split('/').pop() }}</span></button>
      <span class="t2 num sm r">{{ duration(t.duration) }}</span>
      <span v-if="hasStaged(t)" class="led stg" role="img" aria-label="Staged"></span>
      <span v-else-if="t.error" class="led bad" role="img" :aria-label="t.error" :title="t.error"></span>
      <span v-else-if="t.issues.some((i) => !i.kind.startsWith('guide_'))" class="led iss" role="img" aria-label="Has issues"></span>
    </div>
  </div>
</template>
