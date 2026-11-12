<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId } from 'vue'
import { vVars } from '../lib/vars'

// A note that opens under the pointer after 350ms. It sits on the page's root,
// because the panels clip what overflows them.
const props = defineProps<{ title?: string; note: string; open?: boolean }>()
const hover = ref(false)
const at = ref({ '--x': '0px', '--y': '0px' })
const root = ref<HTMLElement>()
const id = useId()
const shown = computed(() => !!props.note && (hover.value || props.open))
let timer: ReturnType<typeof setTimeout> | undefined

function place() {
  const r = root.value?.getBoundingClientRect()
  if (!r) return
  const width = 320
  at.value = { '--x': `${Math.max(8, Math.min(r.left, window.innerWidth - width - 8))}px`, '--y': `${r.bottom + 6}px` }
}
// A tap also sends pointerenter, with no leave to close the note, so only a mouse opens it.
function enter(e: PointerEvent) {
  if (e.pointerType !== 'mouse') return
  timer = setTimeout(() => {
    place()
    hover.value = true
  }, 350)
}
function leave() {
  clearTimeout(timer)
  hover.value = false
}
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <span ref="root" :class="{ 'has-tip': note }" :aria-describedby="shown ? id : undefined" @pointerenter="enter" @pointerleave="leave"><span class="tip-t"><slot /></span></span>
  <Teleport v-if="shown" to=".rs"><span :id="id" v-vars="at" class="tipbox at" role="tooltip"><span v-if="title" class="tip-h">{{ title }}</span><span>{{ note }}</span></span></Teleport>
</template>
