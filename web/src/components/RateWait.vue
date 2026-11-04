<script setup lang="ts">
import { computed, ref } from 'vue'
import { useNow } from '../lib/clock'

// The wait a source asks for has no exact length in the API, and its text says a
// minute, so the meter counts one down. Nothing waits for it.
defineProps<{ message: string }>()
defineEmits<{ retry: [] }>()

const wait = 60
const started = ref(Date.now())
const now = useNow()
const left = computed(() => Math.max(0, wait - Math.floor((now.value - started.value) / 1000)))
const needle = computed(() => `rotate(${(left.value / wait) * 80 - 40} 100 150)`)
</script>

<template>
  <div class="col g12">
    <div class="vu" role="img" :aria-label="`Wait: ${left} seconds left`">
      <div class="vu-face">
        <svg viewBox="0 0 200 120" aria-hidden="true">
          <path class="arc" d="M29.29 65.74 A110 110 0 0 1 170.71 65.74" />
          <path class="ticks" d="M27.36 63.44 A113 113 0 0 1 172.64 63.44" />
          <path class="ticks major" d="M25.76 61.52 A115.5 115.5 0 0 1 174.24 61.52" />
          <text class="vn" x="16.4" y="54.4">0</text><text class="vn" x="55.5" y="31.8">15</text><text class="vn" x="100" y="24">30</text><text class="vn" x="144.5" y="31.8">45</text><text class="vn" x="183.6" y="54.4">60</text>
          <g :transform="needle"><line class="needle-sh" x1="101.5" y1="150" x2="101.5" y2="36" /><line class="needle" x1="100" y1="150" x2="100" y2="34" /></g>
          <path class="hub" d="M78 120 A22 22 0 0 1 122 120 Z" />
        </svg>
      </div>
    </div>
    <p class="center bold">{{ message }}</p>
    <p class="t2 sm center">Downloads already running carry on.</p>
    <button type="button" class="key block" @click="$emit('retry')">Try again</button>
  </div>
</template>

<style scoped>
.rs .vu { display: flex; align-items: center; flex: 1 1 150px; min-width: 130px; padding: 6px; background: #0C0F13; border: 1px solid var(--ink); border-radius: 7px; box-shadow: inset 1px 1px 0 rgba(255, 255, 255, .08), 1px 2px 4px rgba(0, 0, 0, .4); }
.rs .vu-face { position: relative; width: 100%; overflow: hidden; background: radial-gradient(ellipse at 50% 120%, #FFF6D8 0 35%, var(--vu-face) 75%); border-radius: 4px; box-shadow: inset 0 6px 12px rgba(0, 0, 0, .35); }
.rs .vu-face::after { content: ""; position: absolute; inset: 0; pointer-events: none; background: linear-gradient(160deg, rgba(255, 255, 255, .45) 0 18%, rgba(255, 255, 255, 0) 38%); }
.rs .vu svg { display: block; width: 100%; height: auto; }
.rs .vu .arc { fill: none; stroke: var(--vu-ink); stroke-width: 1.4; }
.rs .vu .ticks { fill: none; stroke: var(--vu-ink); stroke-width: 6; stroke-dasharray: 1.2 6.689; stroke-dashoffset: .6; }
.rs .vu .ticks.major { stroke-width: 11; stroke-dasharray: 1.8 38.52; stroke-dashoffset: .9; }
.rs .vu .vn { fill: var(--vu-ink); font: 700 12px var(--label); text-anchor: middle; }
.rs .vu .needle { stroke: #1A1206; stroke-width: 1.6; stroke-linecap: round; }
.rs .vu .needle-sh { stroke: rgba(0, 0, 0, .18); stroke-width: 2; stroke-linecap: round; }
.rs .vu .hub { fill: #2A2416; }
.rs .vu { flex: none; max-width: 320px; align-self: center; width: 100%; }
</style>
