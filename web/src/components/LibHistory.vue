<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import { errorText } from '../api/client'
import type { LibraryChange } from '../api/types'
import { useNow } from '../lib/clock'
import { finishedAt } from '../lib/format'
import { reversible, stateClass, stateWord } from '../lib/history'
import { runSub, runTitle } from '../lib/bulk'
import { useHistory } from '../stores/history'
import Icon from './Icon.vue'
import LibChangeLines from './LibChangeLines.vue'
import LibChangeNotes from './LibChangeNotes.vue'
import ScrollMore from './ScrollMore.vue'

const props = defineProps<{ changeId?: number | null }>()
defineEmits<{ close: [] }>()
const history = useHistory()
const now = useNow()
const openId = ref<number | null>(null)
const asking = ref(false)
const scroller = ref<HTMLElement>()
const missingChange = ref<number | null>(null)
let focusSeq = 0

async function focusChange(target: number | null | undefined) {
  const seq = ++focusSeq
  missingChange.value = null
  if (!target || history.error) return
  while (!history.changes.some((c) => c.id === target) && history.next !== null) {
    const prevNext = history.next
    try {
      await history.more()
    } catch (err) {
      if (seq === focusSeq) history.error = errorText(err)
      return
    }
    if (seq !== focusSeq) return
    if (history.next === prevNext) break
  }
  if (!history.changes.some((c) => c.id === target)) {
    if (seq === focusSeq && !history.error) missingChange.value = target
    return
  }
  openId.value = target
  if (!history.details[target]) void history.open(target)
  await nextTick()
  if (seq !== focusSeq) return
  scroller.value?.querySelector<HTMLElement>(`[data-change="${target}"]`)?.scrollIntoView?.({ block: 'center' })
}

onMounted(async () => {
  await history.load(null)
  await focusChange(props.changeId)
})

watch(() => props.changeId, (target) => {
  if (target) void focusChange(target)
})

function toggle(c: LibraryChange) {
  openId.value = openId.value === c.id ? null : c.id
  if (openId.value !== null && !history.details[c.id]) void history.open(c.id)
}
const what = (c: LibraryChange) => (c.kind === 'delete' ? '' : c.tags > 0 ? c.tags : c.cover ? 'Cover' : 0)

async function clear() {
  await history.clear()
  asking.value = false
}
</script>

<template>
  <section class="win" aria-labelledby="ld-h">
    <div class="tb">
      <h2 id="ld-h">History</h2>
      <span class="stripes"></span>
      <button v-if="history.changes.length > 0" type="button" class="wbtn txt" @click="asking = true">Clear history</button>
      <button type="button" class="wbtn" aria-label="Close history" @click="$emit('close')"><Icon name="close" /></button>
    </div>
    <p v-if="history.error" class="bad-text pad" role="alert">{{ history.error }}</p>
    <p v-else-if="missingChange" class="t2 sm pad" role="status">Change {{ missingChange }} is no longer in history.</p>
    <div v-if="asking" class="note ask" role="alertdialog" aria-label="Clear history">
      <div class="two grow"><span class="bold">Clear history?</span><span class="sub">Undo and redo go with it. No file changes.</span></div>
      <button type="button" class="key sm" @click="asking = false">Cancel</button>
      <button type="button" class="key sm sun" @click="clear">Clear</button>
    </div>
    <div v-if="!history.loading && history.changes.length === 0" class="pad"><div class="lcd lcd-state"><div class="lcd-big">NO SAVES</div></div></div>
    <div v-else ref="scroller" class="rows list" role="table" aria-label="History">
      <div class="row thead g-lh" role="row">
        <span role="columnheader">When</span>
        <span role="columnheader">Album or run</span>
        <span role="columnheader" class="r">Tags</span>
        <span role="columnheader">State</span>
        <span role="columnheader"><span class="sr">Actions</span></span>
      </div>
      <template v-for="c in history.changes" :key="c.id">
        <div class="row g-lh" :class="{ cur: openId === c.id }" :data-change="c.id" role="row">
          <span role="cell" class="t2 num sm">{{ finishedAt(c.created_at, now) }}</span>
          <button type="button" class="lib-open two" :aria-expanded="openId === c.id" @click="toggle(c)"><span class="ttl"><span class="ell">{{ c.run ? runTitle(c.run.fixes) : c.kind === 'delete' ? `Deleted: ${c.title}` : c.title }}</span></span><span class="sub ell">{{ [`Change ${c.id}`, c.run ? runSub(c.run) : c.artist, `By ${c.author || 'Unknown'}`].filter(Boolean).join(' · ') }}</span></button>
          <span role="cell" class="num sm r">{{ what(c) }}</span>
          <span role="cell"><span class="pill" :class="stateClass(c)">{{ stateWord(c) }}</span></span>
          <button v-if="reversible(c)" type="button" class="key sm r" :disabled="history.busy(c)" @click="history.reverse(c)">{{ c.undone ? 'Redo' : 'Undo' }}</button>
        </div>
        <LibChangeNotes :c="c" />
        <LibChangeLines v-if="openId === c.id" :c="c" :deep="!!c.run" />
      </template>
      <div v-if="history.next !== null" class="pad"><ScrollMore :load="history.more" :count="history.changes.length" /></div>
    </div>
  </section>
</template>

<style scoped>
.rs .list { flex: 1; min-height: 0; overflow-y: auto; scrollbar-width: thin; }
.rs .pad { padding: 14px; }
.rs .ask { margin: 12px; align-items: center; }
</style>
