<script setup lang="ts" generic="T extends string | number">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import Icon from './Icon.vue'

// A cap that opens a menu of choices, like the Library's filter menu, in place of a browser select.
const props = defineProps<{ label: string; options: { id: T; label: string }[]; value: T }>()
const emit = defineEmits<{ pick: [id: T] }>()
const open = ref(false)
const root = ref<HTMLElement>()
const current = computed(() => props.options.find((o) => o.id === props.value)?.label ?? props.label)

function pick(id: T) {
  open.value = false
  emit('pick', id)
}
function outside(e: Event) {
  if (root.value && !root.value.contains(e.target as Node)) open.value = false
}
function key(e: KeyboardEvent) {
  if (e.key === 'Escape') { e.stopPropagation(); open.value = false; root.value?.querySelector<HTMLElement>('.wbtn')?.focus() }
}
watch(open, (on) => {
  if (on) {
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', key, true)
    requestAnimationFrame(() => root.value?.querySelector<HTMLElement>('.pop button')?.focus())
  } else {
    document.removeEventListener('pointerdown', outside)
    document.removeEventListener('keydown', key, true)
  }
})
onBeforeUnmount(() => (open.value = false))
</script>

<template>
  <span ref="root" class="pick">
    <button type="button" class="wbtn txt pick-cap" aria-haspopup="menu" :aria-expanded="open" :aria-label="`${label}: ${current}`" @click="open = !open"><span class="ell">{{ current }}</span><Icon name="chevron" /></button>
    <div v-if="open" class="pop" role="menu" :aria-label="label">
      <button v-for="o in options" :key="o.id" type="button" role="menuitemradio" :aria-checked="o.id === value" :class="{ hl: o.id === value }" @click="pick(o.id)">
        <span class="ck"><Icon v-if="o.id === value" name="check" /></span><span class="ell">{{ o.label }}</span>
      </button>
    </div>
  </span>
</template>

<style scoped>
.rs .pick { position: relative; display: inline-flex; min-width: 0; }
.rs .pick-cap { max-width: 220px; height: 32px; }
.rs .pop { top: calc(100% + 6px); left: 0; max-height: min(420px, calc(100dvh - 200px)); overflow-y: auto; }
@media (max-width: 719px) {
  .rs .pick-cap { height: 40px; }
}
</style>
