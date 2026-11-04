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
.rs.day .rot-labels .led { --c: #9AA0A9; }
</style>
