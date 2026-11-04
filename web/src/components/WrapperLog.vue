<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import type { WrapperLogLine } from '../api/types'
import { clockSeconds } from '../lib/format'
import { useStatus } from '../stores/status'

const every = 3000
const kept = 200
const status = useStatus()
const lines = ref<WrapperLogLine[]>([])
const open = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

async function read() {
  try {
    const after = lines.value.at(-1)?.n ?? 0
    const page = await status.wrapperLog(after)
    lines.value = [...lines.value, ...page.lines].slice(-kept)
  } catch {
    // The wrapper's state on the card already says when it doesn't answer.
  }
}

function toggled(e: Event) {
  open.value = (e.target as HTMLDetailsElement).open
  clearInterval(timer)
  if (open.value) {
    void read()
    timer = setInterval(() => void read(), every)
  }
}
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <details class="tail" @toggle="toggled">
    <summary class="px t2">Wrapper log</summary>
    <div class="term" role="log" aria-label="Wrapper log, last lines">
      <div v-if="lines.length === 0" class="ts">No lines yet.</div>
      <div v-for="l in lines" :key="l.n"><span class="ts">{{ clockSeconds(l.time) }}</span>  {{ l.text }}</div>
    </div>
  </details>
</template>

<style scoped>
.rs .term { margin: 0; padding: 10px 12px; overflow: hidden; background: #07090C; border: 1px solid var(--ink); border-radius: 5px; box-shadow: inset 1px 2px 6px rgba(0, 0, 0, .9); color: #CFE9C9; font-family: var(--mono); font-size: 12.5px; line-height: 1.6; white-space: pre-wrap; }
.rs .term .ts { color: #8F93A8; }
.rs .term .ok { color: #7EE2A8; }
.rs .term .wn { color: #FFD15C; }
.rs .term .er { color: #FF8F8F; }
.rs .tail { flex: 1 1 100%; min-width: 0; }
.rs summary { min-height: 44px; display: flex; align-items: center; cursor: pointer; font-size: 13px; }
.rs .term { max-height: 220px; overflow-y: auto; }
</style>
