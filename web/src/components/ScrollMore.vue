<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

// Sits at the end of a list and reads the next page as the end scrolls into
// view. count is how many rows the list holds. A page that adds none stops the
// automatic reads, so a failing page doesn't get asked for in a loop, and the
// button takes over. busy says the list's store has a read running.
const props = defineProps<{ load: () => unknown; count: number; busy?: boolean }>()

const el = ref<HTMLElement | null>(null)
const reading = ref(false)
const stalled = ref(typeof IntersectionObserver === 'undefined')
let observer: IntersectionObserver | null = null
let seen = false

// The nearest scrolling box, so the margin below starts the read before the
// end shows. The page itself scrolls when there is none.
function scroller(node: HTMLElement): HTMLElement | null {
  for (let p = node.parentElement; p; p = p.parentElement) {
    if (/(auto|scroll|overlay)/.test(getComputedStyle(p).overflowY)) return p
  }
  return null
}

// A fresh observer reports at once whether the end shows. One already
// watching reports only when that changes, which a page too short to push the
// end away never does.
function watchEnd() {
  observer?.disconnect()
  observer = null
  if (stalled.value || !el.value) return
  observer = new IntersectionObserver(
    (entries) => {
      seen = entries.some((e) => e.isIntersecting)
      if (seen) void read()
    },
    { root: scroller(el.value), rootMargin: '0px 0px 600px 0px' },
  )
  observer.observe(el.value)
}

async function read() {
  if (reading.value || stalled.value || props.busy) return
  const before = props.count
  reading.value = true
  try {
    await props.load()
  } catch {
    // The store reports its own errors, and the button is there to try again.
  } finally {
    reading.value = false
  }
  if (props.count === before) {
    stalled.value = true
    watchEnd()
  }
}

function again() {
  stalled.value = false
  void read()
}

watch(
  () => props.count,
  () => {
    stalled.value = typeof IntersectionObserver === 'undefined'
    void nextTick(watchEnd)
  },
)
// A read the store started itself may end with the end still in view.
watch(
  () => props.busy,
  (now) => {
    if (!now && seen) void nextTick(watchEnd)
  },
)

onMounted(watchEnd)
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div ref="el" class="scroll-more">
    <button v-if="stalled" type="button" class="key block" :disabled="reading || busy" @click="again">Load more</button>
    <p v-else-if="reading || busy" class="t3 sm center" role="status">Loading more...</p>
  </div>
</template>

<style scoped>
.rs .scroll-more { min-height: 1px; }
</style>
