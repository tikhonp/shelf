<script setup lang="ts">
import { computed } from 'vue'
import type { SearchType } from '../api/types'
import { typeAngles } from '../lib/formats'
import Knob from './Knob.vue'

const props = defineProps<{ modelValue: SearchType }>()
const emit = defineEmits<{ 'update:modelValue': [SearchType] }>()

const types: { id: SearchType; label: string }[] = [
  { id: 'album', label: 'Albums' },
  { id: 'track', label: 'Tracks' },
  { id: 'artist', label: 'Artists' },
  { id: 'playlist', label: 'Playlists' },
]
const at = computed(() => types.findIndex((t) => t.id === props.modelValue))
const turn = () => emit('update:modelValue', types[(at.value + 1) % types.length]!.id)
</script>

<template>
  <div class="rotary" role="radiogroup" aria-label="Type">
    <div class="rot-labels">
      <button v-for="t in types.slice(0, 2)" :key="t.id" type="button" role="radio" :aria-checked="t.id === modelValue" @click="emit('update:modelValue', t.id)">
        <span class="led"></span>{{ t.label }}
      </button>
      <Knob color="red" :size="48" :angle="typeAngles[at] ?? 0" :label="`Switch type, now ${types[at]?.label}`" @press="turn" />
      <button v-for="t in types.slice(2)" :key="t.id" type="button" role="radio" :aria-checked="t.id === modelValue" @click="emit('update:modelValue', t.id)">
        <span class="led"></span>{{ t.label }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.rs .rotary { display: flex; justify-content: center; padding: 6px 12px; background: var(--win-2); border-top: 1px solid var(--ink); border-bottom: 1px solid var(--ink); box-shadow: inset 0 1px 3px rgba(0, 0, 0, .3); }
.rs .rot-labels { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto minmax(0, 1fr) minmax(0, 1fr); align-items: center; gap: 4px; width: 100%; max-width: 460px; }
.rs .rot-labels button[aria-checked="true"] { color: var(--text); }
.rs .rot-labels .led { --c: #3A4554; box-shadow: none; }
.rs.day .rot-labels .led { --c: #9AA0A9; }
.rs .rot-labels button[aria-checked="true"] .led { --c: var(--sun); box-shadow: 0 0 calc(9px * var(--glow)) var(--sun); }
.rs .rot-labels .knob { margin: 0 4px; }
.rs .rot-labels button:not(.knob) { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 5px; min-height: 48px; padding: 4px 2px; border: 0; border-radius: 5px; background: transparent; color: var(--text-2); font-family: var(--label); font-size: 12px; font-weight: 700; letter-spacing: .06em; text-transform: uppercase; cursor: pointer; }
.rs .rot-labels button:not(.knob):hover { color: var(--text); background: color-mix(in srgb, var(--text) 6%, transparent); }
</style>
