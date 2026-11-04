<script setup lang="ts">
import { computed } from 'vue'

// The frequency range a format can carry, on the design's log scale: 20 Hz at
// the left edge, 81.5 units per decade.
const props = defineProps<{ top: number; label: string }>()
const x = (hz: number) => 81.5 * Math.log10(hz / 20)
const edge = computed(() => Math.min(296, x(props.top)))
const limit = x(20000)
</script>

<template>
  <div class="eq" role="img" :aria-label="label">
    <svg viewBox="0 0 300 100" aria-hidden="true">
      <path class="grid" d="M0 30 H300 M0 60 H300 M57 0 V100 M138 0 V100 M220 0 V100" />
      <path class="fill" :d="`M0 30 H${edge} L${edge + 3} 90 L0 90 Z`" />
      <path class="curve" :d="`M0 30 H${edge} L${edge + 3} 90`" />
      <line class="limit" :x1="limit" y1="6" :x2="limit" y2="94" />
      <text x="4" y="96">20 Hz</text><text x="122" y="96">1 kHz</text><text x="214" y="16">20 kHz</text>
    </svg>
  </div>
</template>

<style scoped>
.rs .eq { position: relative; overflow: hidden; background: var(--lcd); border: 1px solid var(--ink); border-radius: 5px; box-shadow: inset 1px 2px 6px rgba(0, 0, 0, .85); }
.rs .eq svg { display: block; width: 100%; height: auto; }
.rs .eq .grid { stroke: rgba(125, 247, 210, .14); stroke-width: 1; fill: none; }
.rs .eq .limit { stroke: rgba(255, 225, 90, .7); stroke-width: 1.2; stroke-dasharray: 3 3; fill: none; }
.rs .eq .curve { stroke: var(--lcd-text); stroke-width: 2.2; fill: none; filter: drop-shadow(0 0 3px var(--lcd-glow)); }
.rs .eq .fill { fill: rgba(125, 247, 210, .12); stroke: none; }
.rs .eq text { fill: rgba(125, 247, 210, .8); font: 11px var(--disp); }
.rs .eq { flex: 1 1 150px; }
</style>
