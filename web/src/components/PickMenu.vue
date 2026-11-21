<script setup lang="ts" generic="T extends string | number">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import Icon from './Icon.vue'

// A cap that opens a menu of choices, like the Library's filter menu, in place of a browser select.
const props = defineProps<{ label: string; options: { id: T; label: string }[]; value: T }>()
const emit = defineEmits<{ pick: [id: T] }>()
const open = ref(false)
const activeIndex = ref(0)
const root = ref<HTMLElement>()
const trigger = ref<HTMLButtonElement>()
const current = computed(() => props.options.find((o) => o.id === props.value)?.label ?? props.label)

function focusItem(index: number) {
  const buttons = root.value?.querySelectorAll<HTMLElement>('.pop [role="menuitemradio"]')
  if (!buttons?.length) return
  const clamped = Math.max(0, Math.min(index, buttons.length - 1))
  activeIndex.value = clamped
  buttons[clamped]?.focus()
}
function openMenu(where: 'selected' | 'first' | 'last' = 'selected') {
  const n = props.options.length
  if (n === 0) return
  if (where === 'last') activeIndex.value = n - 1
  else if (where === 'first') activeIndex.value = 0
  else activeIndex.value = Math.max(0, props.options.findIndex((o) => o.id === props.value))
  open.value = true
}
function closeMenu(restoreFocus = false) {
  open.value = false
  if (restoreFocus) void nextTick(() => trigger.value?.focus())
}
function toggle() {
  if (open.value) closeMenu(true)
  else openMenu('selected')
}
function pick(id: T) {
  emit('pick', id)
  closeMenu(true)
}
function triggerKey(e: KeyboardEvent) {
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    openMenu('selected')
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    openMenu('last')
  }
}
function menuKey(e: KeyboardEvent, index: number) {
  const n = props.options.length
  if (!n) return
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    focusItem((index + 1) % n)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    focusItem((index + n - 1) % n)
  } else if (e.key === 'Home') {
    e.preventDefault()
    focusItem(0)
  } else if (e.key === 'End') {
    e.preventDefault()
    focusItem(n - 1)
  } else if (e.key === 'Tab') {
    closeMenu(false)
  }
}
function outside(e: Event) {
  if (root.value && !root.value.contains(e.target as Node)) closeMenu(false)
}
function key(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    closeMenu(true)
  }
}
watch(open, (on) => {
  if (on) {
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', key, true)
    void nextTick(() => focusItem(activeIndex.value))
  } else {
    document.removeEventListener('pointerdown', outside)
    document.removeEventListener('keydown', key, true)
  }
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('keydown', key, true)
})
</script>

<template>
  <span ref="root" class="pick">
    <button ref="trigger" type="button" class="wbtn txt pick-cap" aria-haspopup="menu" :aria-expanded="open" :aria-label="`${label}: ${current}`" @click="toggle" @keydown="triggerKey"><span class="ell">{{ current }}</span><Icon name="chevron" /></button>
    <div v-if="open" class="pop" role="menu" :aria-label="label">
      <button v-for="(o, i) in options" :key="o.id" type="button" role="menuitemradio" :aria-checked="o.id === value" :tabindex="i === activeIndex ? 0 : -1" :class="{ hl: o.id === value }" @click="pick(o.id)" @keydown="menuKey($event, i)">
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
