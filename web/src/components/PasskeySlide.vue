<script setup lang="ts">
// The Passkey slide. Sliding it to ON, by a press or a 24px drag, starts a passkey sign-in, and sliding it back cancels.
// The parent owns the state: on is where the cap sits, and toggle asks to move it.
const props = defineProps<{ on: boolean; disabled?: boolean; row?: boolean }>()
const emit = defineEmits<{ toggle: [] }>()

let from: number | null = null
// dragged keeps the click that ends a drag from flipping the slide back.
let dragged = false
function down(e: PointerEvent) { from = e.clientX; dragged = false }
function move(e: PointerEvent) {
  if (from === null || props.disabled) return
  const d = e.clientX - from
  if ((!props.on && d > 24) || (props.on && d < -24)) { dragged = true; from = null; emit('toggle') }
}
function click() {
  if (dragged) { dragged = false; return }
  if (!props.disabled) emit('toggle')
}
</script>

<template>
  <button type="button" class="slide" :class="{ row }" role="switch" :aria-checked="on" :aria-disabled="disabled || undefined" aria-label="Sign in with a passkey"
    @pointerdown="down" @pointermove="move" @pointerup="from = null" @pointercancel="from = null" @click="click">
    <span class="sl-name">Passkey</span>
    <span class="sl-row"><span class="sl-t off">OFF</span><span class="sl-track"><span class="sl-cap"></span></span><span class="sl-t on">ON</span></span>
  </button>
</template>
