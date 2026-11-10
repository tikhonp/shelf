<script setup lang="ts">
import { computed, useId } from 'vue'

// A picture of a key switch. The control beside it, such as the Passkey slide, is the real one.
// With press, a click on the picture does what that control does, for the pointer only.
const props = withDefaults(defineProps<{ on?: boolean; width?: number; press?: boolean }>(), { on: false, width: 150, press: false })
const emit = defineEmits<{ press: [] }>()
const id = useId()
// Below 150px the legends grow, so OFF and ON stay readable at 120px.
const small = computed(() => props.width < 150)
</script>

<template>
  <svg class="keysw" :data-press="press || undefined" :width="width" :height="Math.round(width * 124 / 150)" viewBox="0 0 150 124" aria-hidden="true" @click="press && emit('press')">
    <defs>
      <radialGradient :id="`${id}-chrome`" cx="38%" cy="28%" r="80%"><stop offset="0" stop-color="#F4F6F8" /><stop offset=".5" stop-color="#A3AAB4" /><stop offset="1" stop-color="#3F454E" /></radialGradient>
      <linearGradient :id="`${id}-key`" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#F7F3EA" /><stop offset="1" stop-color="#BDB29A" /></linearGradient>
    </defs>
    <text :x="small ? 14 : 20" y="22" fill="currentColor" class="keysw-t" :class="{ small }">OFF</text>
    <text :x="small ? 108 : 112" y="22" fill="currentColor" class="keysw-t" :class="{ small }">ON</text>
    <line x1="43" y1="31" x2="49" y2="38" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    <line x1="107" y1="31" x2="101" y2="38" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
    <circle cx="75" cy="72" r="44" :fill="`url(#${id}-chrome)`" stroke="#0A0C10" stroke-width="1.5" />
    <circle cx="75" cy="72" r="32" fill="#1B1F26" stroke="#0A0C10" />
    <circle cx="75" cy="72" r="32" fill="none" stroke="rgba(255,255,255,.12)" stroke-width="2" stroke-dasharray="40 160" />
    <g class="keysw-bow" :class="{ on }">
      <rect x="71" y="44" width="8" height="56" rx="2" fill="#07090C" />
      <ellipse cx="75" cy="72" rx="13" ry="27" :fill="`url(#${id}-key)`" stroke="#0A0C10" stroke-width="1.5" />
      <circle cx="75" cy="55" r="4" fill="#1B1F26" />
      <line x1="75" y1="66" x2="75" y2="92" stroke="rgba(0,0,0,.25)" stroke-width="2" stroke-linecap="round" />
    </g>
  </svg>
</template>

<style scoped>
.keysw { flex: none; color: var(--text-2); }
.keysw-t { font: 700 12px var(--label); letter-spacing: .12em; }
.keysw-t.small { font-size: 15px; letter-spacing: .1em; }
/* The turn overshoots a little, like a real key. */
.keysw-bow { transform: rotate(-40deg); transform-origin: 75px 72px; transition: transform .5s cubic-bezier(.3, 1.5, .5, 1); }
.keysw-bow.on { transform: rotate(40deg); }
@media (prefers-reduced-motion: reduce) {
  .keysw-bow { transition: none; }
}
</style>
