<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { Format } from '../api/types'
import { menuFormats } from '../lib/formats'
import Icon from './Icon.vue'

const props = defineProps<{ formats: Format[]; source: string; chosen?: string }>()
const emit = defineEmits<{ pick: [Format]; close: [] }>()

const list = computed(() => menuFormats(props.formats))
const current = computed(() => props.chosen ?? list.value[0]?.id)
const root = ref<HTMLElement>()

function outside(e: Event) {
  if (root.value && !root.value.parentElement?.contains(e.target as Node)) emit('close')
}
function key(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.stopPropagation()
    emit('close')
  }
}
onMounted(() => {
  document.addEventListener('pointerdown', outside)
  document.addEventListener('keydown', key, true)
  root.value?.querySelector('button')?.focus()
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('keydown', key, true)
})
</script>

<template>
  <div ref="root" class="pop" role="menu" :aria-label="`${source} format`">
    <div class="pop-h">{{ source }} format</div>
    <button v-for="f in list" :key="f.id" type="button" role="menuitemradio" :aria-checked="f.id === current" :class="{ hl: f.id === current }" @click="emit('pick', f)">
      <span class="ck"><Icon v-if="f.id === current" name="check" /></span>
      <span class="two">
        <span class="bold">{{ f.label }}</span>
        <span class="about">{{ f.description }}</span>
      </span>
      <span class="hint">{{ f.default ? 'Default' : '' }}</span>
    </button>
  </div>
</template>

<style scoped>
.rs .pop { top: calc(100% + 6px); right: 0; }
.rs .about { font-size: 13px; opacity: .85; }
@media (max-width: 719px) {
  .rs .pop { left: 0; right: 0; }
}
</style>
