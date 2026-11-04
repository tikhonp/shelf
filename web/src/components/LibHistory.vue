<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { LibraryChange } from '../api/types'
import { useNow } from '../lib/clock'
import { count, finishedAt } from '../lib/format'
import { historyLines } from '../lib/history'
import { useHistory } from '../stores/history'
import Icon from './Icon.vue'

defineEmits<{ close: [] }>()
const history = useHistory()
const now = useNow()
const openId = ref<number | null>(null)
const asking = ref(false)

onMounted(() => void history.load())

function toggle(c: LibraryChange) {
  openId.value = openId.value === c.id ? null : c.id
  if (openId.value !== null && !history.details[c.id]) void history.open(c.id)
}
const busy = (c: LibraryChange) => Object.values(history.running).includes(c.id)
const what = (c: LibraryChange) => (c.tags > 0 ? c.tags : c.cover ? 'Cover' : 0)
const lines = (c: LibraryChange) => historyLines(history.details[c.id]?.files ?? [])
const stateWord = (c: LibraryChange) => (c.undone ? 'Undone' : c.state === 'done' ? 'Saved' : c.state === 'partial' ? 'Partial' : c.state === 'failed' ? 'Failed' : 'Saving')
const stateClass = (c: LibraryChange) => (c.undone ? 'canceled' : c.state === 'done' ? 'done' : c.state === 'partial' ? 'partial' : c.state === 'failed' ? 'failed' : 'running')
const reversible = (c: LibraryChange) => c.state === 'done' || c.state === 'partial'

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
    <div v-if="asking" class="note ask" role="alertdialog" aria-label="Clear history">
      <div class="two grow"><span class="bold">Clear history?</span><span class="sub">Undo and redo go with it. No file changes.</span></div>
      <button type="button" class="key sm" @click="asking = false">Cancel</button>
      <button type="button" class="key sm sun" @click="clear">Clear</button>
    </div>
    <div v-if="!history.loading && history.changes.length === 0" class="pad"><div class="lcd lcd-state"><div class="lcd-big">NO SAVES</div></div></div>
    <div v-else class="rows list" role="table" aria-label="History">
      <div class="row thead g-lh" role="row">
        <span role="columnheader">When</span>
        <span role="columnheader">Album</span>
        <span role="columnheader" class="r">Tags</span>
        <span role="columnheader">State</span>
        <span role="columnheader"><span class="sr">Actions</span></span>
      </div>
      <template v-for="c in history.changes" :key="c.id">
        <div class="row g-lh" :class="{ cur: openId === c.id }" role="row">
          <span role="cell" class="t2 num sm">{{ finishedAt(c.created_at, now) }}</span>
          <button type="button" class="lib-open two" :aria-expanded="openId === c.id" @click="toggle(c)"><span class="ttl"><span class="ell">{{ c.title }}</span></span><span class="sub ell">{{ c.artist }}</span></button>
          <span role="cell" class="num sm r">{{ what(c) }}</span>
          <span role="cell"><span class="pill" :class="stateClass(c)">{{ stateWord(c) }}</span></span>
          <button v-if="reversible(c)" type="button" class="key sm r" :disabled="busy(c)" @click="history.reverse(c)">{{ c.undone ? 'Redo' : 'Undo' }}</button>
        </div>
        <p v-if="history.conflicts[c.id]" class="conflict t2" role="status">{{ count(history.conflicts[c.id]!.length, 'field') }} changed since then and stayed: {{ [...new Set(history.conflicts[c.id]!.map((x) => x.key))].join(', ') }}.</p>
        <div v-if="openId === c.id" class="lib-ba">
          <div class="row thead g-ba flat"><span>Track</span><span>Tag</span><span>Before</span><span>After</span></div>
          <div v-for="l in lines(c)" :key="l.track + l.key + l.before" class="row g-ba"><span class="t2 ell">{{ l.track }}</span><span class="mono ell tag">{{ l.key }}</span><span class="t2 ell">{{ l.before || 'Empty' }}</span><span class="ell bold">{{ l.after || 'Empty' }}</span></div>
        </div>
      </template>
      <div v-if="history.next !== null" class="pad"><button type="button" class="key block" @click="history.more()">Load more</button></div>
    </div>
  </section>
</template>

<style scoped>
.rs .list { flex: 1; min-height: 0; overflow-y: auto; scrollbar-width: thin; }
.rs .pad { padding: 14px; }
.rs .ask { margin: 12px; align-items: center; }
.rs .flat { position: static; }
.rs .tag { font-size: 12.5px; }
.rs .conflict { padding: 8px 14px; border-bottom: 1px solid var(--line); }
</style>
