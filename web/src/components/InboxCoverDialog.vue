<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from './Icon.vue'

defineProps<{ url: string; label: string }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLElement>()
const closeBtn = ref<HTMLButtonElement>()

const focusableSelector = 'button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'

function onKey(e: KeyboardEvent) {
  const modals = [...document.querySelectorAll<HTMLElement>('[aria-modal="true"]')]
  if (dialog.value && modals.at(-1) !== dialog.value) return
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    e.stopImmediatePropagation()
    emit('close')
    return
  }
  if (e.key === 'Tab') {
    const nodes = [...(dialog.value?.querySelectorAll<HTMLElement>(focusableSelector) ?? [])]
    if (nodes.length === 0) {
      e.preventDefault()
      e.stopImmediatePropagation()
      closeBtn.value?.focus()
      return
    }
    const first = nodes[0]!
    const last = nodes[nodes.length - 1]!
    const active = document.activeElement
    if (e.shiftKey) {
      if (active === first || !dialog.value?.contains(active)) {
        e.preventDefault()
        e.stopImmediatePropagation()
        last.focus()
      }
    } else if (active === last || !dialog.value?.contains(active)) {
      e.preventDefault()
      e.stopImmediatePropagation()
      first.focus()
    }
  }
}

onMounted(() => {
  document.addEventListener('keydown', onKey, true)
  closeBtn.value?.focus()
})
onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKey, true)
})
</script>

<template>
  <div class="scrim" @click.self="emit('close')">
    <section ref="dialog" class="win ib-cover-dlg" role="dialog" aria-modal="true" :aria-label="label">
      <div class="tb">
        <h2 class="ell">{{ label }}</h2>
        <span class="stripes"></span>
        <button ref="closeBtn" type="button" class="wbtn" aria-label="Close artwork preview" @click="emit('close')"><Icon name="close" /></button>
      </div>
      <div class="ib-cover-stage">
        <img :src="url" :alt="label" />
      </div>
    </section>
  </div>
</template>

<style scoped>
.rs .scrim { position: fixed; inset: 0; z-index: 40; display: flex; align-items: center; justify-content: center; padding: 20px; background: var(--scrim); }
.rs .ib-cover-dlg { width: auto; max-width: min(92vw, 720px); max-height: 92dvh; }
.rs .ib-cover-stage { display: flex; align-items: center; justify-content: center; min-width: 240px; min-height: 240px; padding: 16px; background: var(--field); }
.rs .ib-cover-stage img { display: block; max-width: min(85vw, 640px); max-height: calc(92dvh - 80px); width: auto; height: auto; object-fit: contain; }
@media (max-width: 719px) {
  .rs .scrim { padding: 8px; }
}
</style>
