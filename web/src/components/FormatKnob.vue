<script setup lang="ts">
import { computed } from 'vue'
import type { Format } from '../api/types'
import { knobAngles, menuFormats } from '../lib/formats'
import Knob from './Knob.vue'

const props = defineProps<{ formats: Format[]; modelValue: string; size?: number }>()
const emit = defineEmits<{ 'update:modelValue': [string] }>()

const list = computed(() => menuFormats(props.formats))
const at = computed(() => Math.max(0, list.value.findIndex((f) => f.id === props.modelValue)))
const angle = computed(() => knobAngles(list.value.length)[at.value] ?? 45)
const turn = () => emit('update:modelValue', list.value[(at.value + 1) % list.value.length]!.id)
</script>

<template>
  <Knob color="blue" :size="size ?? 64" :angle="angle" :label="`Switch format, now ${list[at]?.label}`" @press="turn" />
  <div class="fmt-list" role="radiogroup" aria-label="Format">
    <button v-for="f in list" :key="f.id" type="button" role="radio" :aria-checked="f.id === list[at]?.id" @click="emit('update:modelValue', f.id)">
      <span class="led"></span>{{ f.label }}<span v-if="f.default" class="hint">default</span>
    </button>
  </div>
</template>

<style scoped>
.rs .fmt-list { display: flex; flex-direction: column; gap: 2px; }
.rs .fmt-list button { display: flex; align-items: center; gap: 10px; min-height: 40px; padding: 0 10px; border: 0; border-radius: 5px; background: transparent; color: var(--text-2); font-family: var(--label); font-size: 14px; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; text-align: left; cursor: pointer; }
.rs .fmt-list button:hover { color: var(--text); background: color-mix(in srgb, var(--text) 6%, transparent); }
.rs .fmt-list button[aria-checked="true"] { color: var(--text); }
.rs .fmt-list .led { --c: #3A4554; box-shadow: none; }
.rs.day .fmt-list .led { --c: #9AA0A9; }
.rs .fmt-list button[aria-checked="true"] .led { --c: var(--sun); box-shadow: 0 0 calc(9px * var(--glow)) var(--sun); }
.rs .fmt-list .hint { font-family: var(--font); font-size: 12px; font-weight: 500; letter-spacing: 0; text-transform: none; color: var(--text-3); }
</style>
