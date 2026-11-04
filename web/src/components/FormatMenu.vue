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
.rs .pop { position: absolute; z-index: 20; min-width: 230px; padding: 6px; background: var(--win); border: 1px solid var(--ink); border-radius: 7px; box-shadow: inset 1px 1px 0 rgba(255, 255, 255, .1), 6px 10px 24px rgba(0, 0, 0, .5); }
.rs .pop-h { padding: 6px 10px; font-family: var(--label); font-size: 12px; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; color: var(--text-3); }
.rs .pop button { display: flex; align-items: center; gap: 10px; width: 100%; min-height: 40px; padding: 0 10px; border: 0; border-radius: 5px; background: transparent; font-size: 14px; text-align: left; cursor: pointer; color: var(--text); }
.rs .pop button:hover, .rs .pop button.hl { background: var(--lamp); color: #1A1206; }
.rs .pop .ck { display: inline-flex; flex: none; width: 18px; }
.rs .pop .hint { margin-left: auto; font-size: 12px; color: var(--text-3); }
.rs .pop button.hl .hint, .rs .pop button:hover .hint { color: inherit; }
@media (max-width: 719px) {
  .rs .pop button { min-height: 56px; font-size: 16px; }
}
.rs .pop { top: calc(100% + 6px); right: 0; }
.rs .about { font-size: 13px; opacity: .85; }
@media (max-width: 719px) {
  .rs .pop { left: 0; right: 0; }
}
</style>
