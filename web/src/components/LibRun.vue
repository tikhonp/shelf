<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ended, fixName, lineText, meterCells } from '../lib/bulk'
import { useNow } from '../lib/clock'
import { clockSeconds, count, elapsed } from '../lib/format'
import { useBulk } from '../stores/bulk'
import Icon from './Icon.vue'
import Meter from './Meter.vue'

const bulk = useBulk()
const now = useNow()
const log = ref<HTMLElement>()

const run = computed(() => bulk.run!)
const over = computed(() => ended(run.value))
const handled = computed(() => run.value.done + run.value.skipped + run.value.failed)
const two = (n: number) => String(n).padStart(2, '0')
const took = computed(() => {
  const end = run.value.finished_at ? Date.parse(run.value.finished_at) : now.value
  return elapsed(Math.max(0, Math.round((end - Date.parse(run.value.created_at)) / 1000)))
})
const cells = computed(() => meterCells(run.value))
const left = computed(() => run.value.total - handled.value)
const meterWords = computed(() => `${count(run.value.done, 'album')} done, ${run.value.skipped + run.value.failed} skipped, ${left.value} left`)
const nowWords = computed(() => (run.value.now_state === 'analyzing' ? 'measuring' : 'writing'))
const outcome = computed(() => {
  const r = run.value
  if (r.reason === 'cancelled') return 'The run was cancelled. What it wrote is in History as one step.'
  if (r.state === 'failed') return r.error || 'The run stopped.'
  const skipped = r.skipped + r.failed
  return skipped > 0 ? `${count(skipped, 'album')} stayed as they were. The log says why.` : 'The run is in History as one step.'
})
const fixWords = computed(() => run.value.fixes.map(fixName).join(', '))

watch(
  () => run.value.log.length,
  () => nextTick(() => log.value?.scrollTo({ top: log.value.scrollHeight })),
)
</script>

<template>
  <section class="win" aria-labelledby="ld-r">
    <div class="tb">
      <h2 id="ld-r">Bulk run</h2>
      <span class="stripes"></span>
      <span v-if="!over" class="led rec blink" title="Running"></span>
      <button v-else type="button" class="wbtn" aria-label="Close the bulk run" @click="bulk.close()"><Icon name="close" /></button>
    </div>
    <div class="vscroll body">
      <div class="lcd tight" role="img" :aria-label="`${handled} of ${run.total} albums done, ${took} elapsed`">
        <div class="line"><span class="lcd-big num">{{ two(handled) }}/{{ two(run.total) }}</span><span class="lcd-l">ALBUMS DONE</span><span class="lcd-l num end">{{ took }} ELAPSED</span></div>
        <div class="lcd-l"><span class="ell">{{ run.now ? `NOW: ${run.now.toUpperCase()}` : fixWords.toUpperCase() }}</span></div>
      </div>
      <Meter :done="cells.done" :failed="cells.failed" :total="cells.total" :running="!over" :label="meterWords" />
      <p class="t2">{{ over ? outcome : 'A Save on another album waits for one album at most. The run shows in History as one step when it ends.' }}</p>
      <div ref="log" class="term" role="log" aria-label="Run log">
        <div v-for="l in run.log" :key="l.at + l.album"><span class="ts">{{ clockSeconds(l.at) }}</span>  <span :class="l.state === 'done' ? 'ok' : 'wn'">{{ lineText(l) }}</span></div>
        <div v-if="run.now">{{ run.now }}: {{ nowWords }}</div>
      </div>
    </div>
    <div v-if="!over" class="lib-save">
      <span class="grow"></span>
      <button type="button" class="key" :disabled="run.cancelling" @click="bulk.cancel()"><Icon name="stop" />{{ run.cancelling ? 'Cancelling' : 'Cancel' }}</button>
    </div>
  </section>
</template>

<style scoped>
.rs .body { gap: 14px; padding: 14px; }
.rs .tight { flex: none; gap: 6px; }
.rs .line { display: flex; align-items: baseline; gap: 10px; }
.rs .end { margin-left: auto; }
.rs .term { flex: 1; min-height: 120px; max-height: 340px; padding: 10px 12px; overflow-y: auto; scrollbar-width: thin; background: #07090C; border: 1px solid var(--ink); border-radius: 5px; box-shadow: inset 1px 2px 6px rgba(0, 0, 0, .9); color: #CFE9C9; font-family: var(--mono); font-size: 12.5px; line-height: 1.6; white-space: pre-wrap; }
.rs .term .ts { color: #8F93A8; }
.rs .term .ok { color: #7EE2A8; }
.rs .term .wn { color: #FFD15C; }
</style>
