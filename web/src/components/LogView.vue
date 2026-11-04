<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import type { LogLine } from '../api/types'
import { clockSeconds } from '../lib/format'

const props = defineProps<{ downloadId: number }>()

const maxLines = 2000
const lines = ref<LogLine[]>([])
const box = ref<HTMLElement>()
let stream: EventSource | null = null

function close() {
  stream?.close()
  stream = null
}

function follows() {
  const el = box.value
  return !el || el.scrollHeight - el.scrollTop - el.clientHeight < 40
}

function open(id: number) {
  close()
  lines.value = []
  stream = new EventSource(`/api/downloads/${id}/log`)
  stream.addEventListener('line', (m) => {
    const follow = follows()
    lines.value.push(JSON.parse((m as MessageEvent<string>).data) as LogLine)
    if (lines.value.length > maxLines) lines.value.splice(0, lines.value.length - maxLines)
    if (follow) void nextTick(() => box.value && (box.value.scrollTop = box.value.scrollHeight))
  })
  // Without this close EventSource would connect again and read the log from the start.
  stream.addEventListener('done', close)
}

watch(() => props.downloadId, open, { immediate: true })
onBeforeUnmount(close)

const tone = (level: string) => (level === 'error' ? 'er' : level === 'warn' ? 'wn' : '')
</script>

<template>
  <div ref="box" class="term" role="log" aria-label="Download log" tabindex="0">
    <div v-if="lines.length === 0" class="ts">No log lines yet.</div>
    <div v-for="(l, i) in lines" :key="i"><span class="ts">{{ clockSeconds(l.time) }}</span>  <span :class="tone(l.level)">{{ l.text }}</span></div>
  </div>
</template>

<style scoped>
.rs .term { margin: 0; padding: 10px 12px; overflow: hidden; background: #07090C; border: 1px solid var(--ink); border-radius: 5px; box-shadow: inset 1px 2px 6px rgba(0, 0, 0, .9); color: #CFE9C9; font-family: var(--mono); font-size: 12.5px; line-height: 1.6; white-space: pre-wrap; }
.rs .term .ts { color: #8F93A8; }
.rs .term .ok { color: #7EE2A8; }
.rs .term .wn { color: #FFD15C; }
.rs .term .er { color: #FF8F8F; }
.rs .term { max-height: 280px; overflow-y: auto; }
</style>
