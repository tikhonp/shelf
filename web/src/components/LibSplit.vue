<script setup lang="ts">
defineProps<{ label: string }>()
const emit = defineEmits<{ move: [number] }>()

let from: number | null = null

function down(e: PointerEvent) {
  from = e.clientX
  ;(e.target as HTMLElement).setPointerCapture?.(e.pointerId)
}
function drag(e: PointerEvent) {
  if (from === null) return
  emit('move', e.clientX - from)
  from = e.clientX
}
const up = () => (from = null)
function key(e: KeyboardEvent) {
  if (e.key === 'ArrowLeft') emit('move', -20)
  if (e.key === 'ArrowRight') emit('move', 20)
}
</script>

<template>
  <button type="button" class="lib-split" :aria-label="`Resize the ${label}. Left and right arrows change the width.`" title="Drag to resize" @pointerdown="down" @pointermove="drag" @pointerup="up" @pointercancel="up" @keydown="key"></button>
</template>
