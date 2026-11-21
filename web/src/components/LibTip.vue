<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { vVars } from '../lib/vars'

// A note that opens under the pointer after 350ms or on keyboard focus. It sits on the page's root,
// because the panels clip what overflows them.
const props = defineProps<{ title?: string; note: string; open?: boolean }>()
const hover = ref(false)
const focused = ref(false)
const nested = ref(false)
const at = ref({ '--x': '0px', '--y': '0px' })
const root = ref<HTMLElement>()
const id = useId()
const shown = computed(() => !!props.note && (hover.value || focused.value || props.open))
let timer: ReturnType<typeof setTimeout> | undefined
let host: HTMLElement | null = null
let setHostDesc = false

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
function focusIn() {
  if (!props.note) return
  place()
  focused.value = true
}
function focusOut() {
  focused.value = false
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && (focused.value || hover.value)) {
    clearTimeout(timer)
    focused.value = false
    hover.value = false
  }
}

watch([shown, () => props.note], ([isShown]) => {
  if (!host) return
  if (isShown) {
    if (!host.getAttribute('aria-describedby')) {
      host.setAttribute('aria-describedby', id)
      setHostDesc = true
    }
  } else if (setHostDesc) {
    if (host.getAttribute('aria-describedby') === id) host.removeAttribute('aria-describedby')
    setHostDesc = false
  }
})

onMounted(() => {
  host = root.value?.parentElement?.closest<HTMLElement>('button, a[href], input, select, textarea, [tabindex]') ?? null
  nested.value = !!host
  if (host) {
    host.addEventListener('focus', focusIn)
    host.addEventListener('blur', focusOut)
    host.addEventListener('keydown', onKey)
  }
})
onBeforeUnmount(() => {
  clearTimeout(timer)
  if (host) {
    host.removeEventListener('focus', focusIn)
    host.removeEventListener('blur', focusOut)
    host.removeEventListener('keydown', onKey)
    if (setHostDesc && host.getAttribute('aria-describedby') === id) host.removeAttribute('aria-describedby')
  }
})
</script>

<template>
  <span
    ref="root"
    :class="{ 'has-tip': note }"
    :tabindex="note && !nested ? 0 : undefined"
    :aria-describedby="!nested && shown ? id : undefined"
    @pointerenter="enter"
    @pointerleave="leave"
    @focus="focusIn"
    @blur="focusOut"
    @keydown="onKey"
  ><span class="tip-t"><slot /></span></span>
  <Teleport v-if="shown" to=".rs"><span :id="id" v-vars="at" class="tipbox at" role="tooltip"><span v-if="title" class="tip-h">{{ title }}</span><span>{{ note }}</span></span></Teleport>
</template>
